// Package remotepc runs eta on another machine over SSH and forwards it
// to a local port, so that machine can be browsed as an ordinary peer.
//
// Almost nothing here is implemented. The system `ssh` binary connects
// (as VS Code Remote-SSH and Ansible both do, rather than a Go SSH
// library — so this works exactly when the user's own `ssh <dest>`
// works). `go install` fetches, builds, pins the version, and verifies
// integrity against the checksum database. `ssh -L` reaches it, so ssh
// auth is the only auth and nothing is exposed remotely.
// --exit-on-stdin-close stops it, so there is no daemon to manage.
// GOPATH and GOCACHE live in ~/.eta so cleanup is one directory.
//
// What's left is glue: an argv, two ports, two one-line commands, a
// marker scanner, a health poll.
//
// Full rationale and citations: temp/remote-bootstrap-design.md.
package remotepc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sshBinary is the client this package spawns. Overridden in tests.
var sshBinary = "ssh"

// Phase is coarse progress, not a build log. Deliberately few.
type Phase string

const (
	PhaseConnecting Phase = "connecting"
	PhaseChecking   Phase = "checking"
	PhaseInstalling Phase = "installing"
	PhaseStarting   Phase = "starting"
	PhaseReady      Phase = "ready"
	PhaseFailed     Phase = "failed"
)

const (
	// Bounded so a silent remote can't hang forever. Generous: the first
	// connect compiles from source, later ones hit Go's build cache.
	establishTimeout = 10 * time.Minute
	detectTimeout    = 30 * time.Second
	recentLines      = 40
)

// shell is which command language the remote's ssh session speaks. Only
// the family — not GOOS/GOARCH, since `go install` compiles natively.
type shell int

const (
	shellPOSIX shell = iota
	shellPowerShell
)

type Options struct {
	// Passed to ssh verbatim: hostname, user@host, or a ~/.ssh/config alias.
	Destination string
	// Default to this binary's own, so a remote never runs a different
	// version than the machine that started it.
	Module  string
	Version string
}

// Session is one running remote eta, reachable at URL while alive.
type Session struct {
	destination string
	url         string
	cmd         *exec.Cmd
	stdin       io.Closer

	mu     sync.Mutex
	phase  Phase
	err    error
	recent []string

	exited chan struct{}
}

// URL is a forwarded loopback address, valid only while this session
// lives — so a saved peer must be keyed by Destination, never by this.
func (s *Session) URL() string { return s.url }

// Destination is the stable identity: unchanged between sessions.
func (s *Session) Destination() string { return s.destination }

func (s *Session) Phase() Phase {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phase
}

func (s *Session) Recent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.recent...)
}

func (s *Session) setPhase(p Phase) {
	s.mu.Lock()
	s.phase = p
	s.mu.Unlock()
}

func (s *Session) record(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recent = append(s.recent, line)
	if len(s.recent) > recentLines {
		s.recent = s.recent[len(s.recent)-recentLines:]
	}
}

// Stop closes stdin (the leash, which stops the remote eta) and kills
// ssh as a backstop. The kill is explicit because OS parent-death is
// Linux-only: docker/cli's own pdeathsig_nolinux.go is an empty func.
func (s *Session) Stop() {
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	select {
	case <-s.exited:
		return
	case <-time.After(3 * time.Second):
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.exited
}

func (s *Session) Wait() error {
	<-s.exited
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// validateDestination refuses values ssh reads as options: a
// "-oProxyCommand=..." destination would run a command on *this*
// machine. sshArgs' "--" is a second layer (as in docker/cli).
func validateDestination(destination string) error {
	if strings.TrimSpace(destination) == "" {
		return errors.New("no SSH destination given")
	}
	if strings.HasPrefix(destination, "-") {
		return fmt.Errorf("invalid SSH destination %q: values beginning with \"-\" are refused, because ssh would read one as an option instead of a destination", destination)
	}
	return nil
}

// sshArgs builds one ssh argv. Each option earns its place:
//   - BatchMode: no terminal here to answer a prompt; a hang waiting on
//     one is indistinguishable from an unreachable machine.
//   - ConnectTimeout: else an unroutable host hangs for the kernel's own
//     TCP timeout.
//   - ServerAliveInterval: notice a connection that died without closing.
//   - -T: no pty. Keeps stdout/stderr as separate clean pipes; a pty
//     merges them, adds \r\n, and on Windows means ConPTY
//     screen-repaint output that would have to be parsed, not read.
//   - ExitOnForwardFailure: defaults to "no", so without it a failed
//     local bind leaves ssh running and the health check succeeds
//     against whatever else owns that port.
func sshArgs(destination string, forward string) ([]string, error) {
	if err := validateDestination(destination); err != nil {
		return nil, err
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-T",
	}
	if forward != "" {
		args = append(args, "-o", "ExitOnForwardFailure=yes", "-L", forward)
	}
	return append(args, "--", destination), nil
}

// freeLocalPort asks the OS for a port and releases it. ssh's -L doesn't
// document port-0 allocation, and scraping its "Allocated port" output
// is the parsing this package avoids. ExitOnForwardFailure turns the
// release-to-bind race into a clean error.
func freeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// remoteCommand is the whole remote side: three env vars, install, run.
//
// Nothing user-supplied is interpolated — module and version come from
// this binary's build info, ports are ints, root is the remote's own
// $HOME — so there is no quoting regime and no shell-quoting helper.
//
// GOPATH and GOCACHE sit in ~/.eta so one removal covers binary, module
// cache and build cache. -modcacherw is required for that removal to
// work at all: go marks module cache files read-only otherwise.
//
// Exposing $HOME needs no confirmation: it binds loopback only and is
// reachable solely through this session's forward.
func remoteCommand(sh shell, module, version string, remotePort int) string {
	port := strconv.Itoa(remotePort)
	switch sh {
	case shellPowerShell:
		return strings.Join([]string{
			`$ErrorActionPreference = "Stop"`,
			`if (-not (Get-Command go -ErrorAction SilentlyContinue)) { Write-Output "ETA:fail:no Go toolchain found on this PC (install Go, or make sure it is on the PATH for non-interactive SSH sessions)"; exit 1 }`,
			`$env:GOPATH = Join-Path $HOME ".eta"`,
			`$env:GOCACHE = Join-Path $env:GOPATH "build-cache"`,
			`$env:GOFLAGS = "-modcacherw"`,
			`Write-Output "ETA:installing"`,
			`go install ` + module + `@` + version,
			`if ($LASTEXITCODE -ne 0) { Write-Output "ETA:fail:go install failed"; exit 1 }`,
			`Write-Output "ETA:starting"`,
			`& (Join-Path $env:GOPATH "bin\eta.exe") --exit-on-stdin-close --ip 127.0.0.1 --port ` + port + ` --root $HOME`,
		}, "; ")
	default:
		return strings.Join([]string{
			`command -v go >/dev/null 2>&1 || { echo "ETA:fail:no Go toolchain found on this PC (install Go, or make sure it is on the PATH for non-interactive SSH sessions)"; exit 1; }`,
			`GOPATH="$HOME/.eta"; export GOPATH`,
			`GOCACHE="$GOPATH/build-cache"; export GOCACHE`,
			`GOFLAGS=-modcacherw; export GOFLAGS`,
			`echo "ETA:installing"`,
			`go install ` + module + `@` + version + ` || { echo "ETA:fail:go install failed"; exit 1; }`,
			`echo "ETA:starting"`,
			`exec "$GOPATH/bin/eta" --exit-on-stdin-close --ip 127.0.0.1 --port ` + port + ` --root "$HOME"`,
		}, "\n")
	}
}

// cleanupCommand removes everything this package put on a remote: it all
// lives in ~/.eta, and no config or service is installed.
//
// `go clean -modcache` first is not optional — go stores module cache
// files read-only, so a plain remove fails partway through. -modcacherw
// prevents new ones but can't fix a cache an earlier install left (a
// real-sshd test caught exactly that). Guarded on `go` existing, failure
// ignored, so an uninstalled Go still gets the removal attempted.
//
// Not quite everything: Go ≥1.23 writes telemetry outside ~/.eta. That's
// Go's state, not eta's; left alone.
func cleanupCommand(sh shell) string {
	if sh == shellPowerShell {
		return strings.Join([]string{
			`$env:GOPATH = Join-Path $HOME ".eta"`,
			`if (Get-Command go -ErrorAction SilentlyContinue) { go clean -modcache 2>$null }`,
			`Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path $HOME ".eta")`,
		}, "; ")
	}
	return strings.Join([]string{
		`GOPATH="$HOME/.eta"; export GOPATH`,
		`command -v go >/dev/null 2>&1 && go clean -modcache 2>/dev/null`,
		`rm -rf "$HOME/.eta"`,
	}, "\n")
}

// detectShell tries `uname -sm` (every POSIX target), then a variable
// only PowerShell defines. Nothing else is supported: a Windows PC still
// on the cmd.exe default gets a clear error naming what to change,
// rather than a third command language and quoting regime.
func detectShell(ctx context.Context, destination string) (shell, error) {
	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()

	if out, err := runSSH(ctx, destination, "uname -sm"); err == nil && strings.TrimSpace(out) != "" {
		return shellPOSIX, nil
	}
	if out, err := runSSH(ctx, destination, `Write-Output $PSVersionTable.PSVersion.Major`); err == nil && strings.TrimSpace(out) != "" {
		return shellPowerShell, nil
	}
	return 0, fmt.Errorf("could not run a command on %s: it answered neither `uname` (Linux/macOS) nor PowerShell. If this is a Windows PC, set its SSH shell to PowerShell — the registry value DefaultShell under HKEY_LOCAL_MACHINE\\SOFTWARE\\OpenSSH — since the Windows default, cmd.exe, is not supported", destination)
}

// runSSH runs one short command and collects its output. Detection only;
// a session streams instead.
func runSSH(ctx context.Context, destination, command string) (string, error) {
	args, err := sshArgs(destination, "")
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath(sshBinary); err != nil {
		return "", fmt.Errorf("no ssh client found on this computer (%w). On Windows, turn on the optional OpenSSH Client feature; on Linux or macOS, install an openssh-client package", err)
	}
	out, err := exec.CommandContext(ctx, sshBinary, append(args, command)...).Output()
	return string(out), err
}

// selfModuleVersion pins the remote to this binary's own version. A
// development build has none, which must be an explicit error rather
// than falling back to @latest and installing a different eta.
func selfModuleVersion() (module, version string, err error) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", errors.New("this build has no module information, so there is no version a remote PC could install")
	}
	module = info.Main.Path
	version = info.Main.Version
	if module == "" {
		return "", "", errors.New("this build has no module path, so there is no version a remote PC could install")
	}
	if version == "" || version == "(devel)" {
		return "", "", fmt.Errorf("this is a development build of eta (%s), which has no published version a remote PC could install — use a released build to set up a PC over SSH", module)
	}
	return module, version, nil
}

// Start detects the remote's shell, installs and runs eta there, and
// waits until it actually answers locally.
func Start(ctx context.Context, opts Options) (*Session, error) {
	module, version := opts.Module, opts.Version
	if module == "" || version == "" {
		selfModule, selfVersion, err := selfModuleVersion()
		if err != nil {
			return nil, err
		}
		if module == "" {
			module = selfModule
		}
		if version == "" {
			version = selfVersion
		}
	}
	if err := validateDestination(opts.Destination); err != nil {
		return nil, err
	}

	sh, err := detectShell(ctx, opts.Destination)
	if err != nil {
		return nil, err
	}

	localPort, err := freeLocalPort()
	if err != nil {
		return nil, err
	}
	// Per session, not fixed: a manually run eta, a second machine
	// bootstrapping at once, or one left by a dropped connection the
	// remote sshd hasn't noticed yet, must not collide.
	remotePort, err := freeLocalPort()
	if err != nil {
		return nil, err
	}

	forward := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", localPort, remotePort)
	args, err := sshArgs(opts.Destination, forward)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath(sshBinary); err != nil {
		return nil, fmt.Errorf("no ssh client found on this computer (%w). On Windows, turn on the optional OpenSSH Client feature; on Linux or macOS, install an openssh-client package", err)
	}

	session := &Session{
		destination: opts.Destination,
		url:         "http://127.0.0.1:" + strconv.Itoa(localPort),
		phase:       PhaseConnecting,
		exited:      make(chan struct{}),
	}

	cmd := exec.Command(sshBinary, append(args, remoteCommand(sh, module, version, remotePort))...)
	// Stdin stays open for the session's life and is the leash: closing it
	// closes the ssh channel, the remote's stdin hits EOF, and
	// --exit-on-stdin-close shuts it down. Same on POSIX and Windows,
	// unlike a pty and SIGHUP.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start ssh: %w", err)
	}
	session.cmd = cmd
	session.stdin = stdin
	session.setPhase(PhaseChecking)

	// Both pipes, concurrently, for the session's whole life. Draining
	// only stdout deadlocks: `go install` writes progress to stderr, and
	// once that buffer fills the remote go blocks on write and never
	// reaches the line this side waits for.
	var drained sync.WaitGroup
	drained.Add(2)
	go func() { defer drained.Done(); session.readMarkers(stdout) }()
	go func() { defer drained.Done(); session.drain(stderr) }()

	go func() {
		waitErr := cmd.Wait()
		drained.Wait()
		session.mu.Lock()
		if session.phase != PhaseReady && session.err == nil {
			session.err = session.failure(waitErr)
			session.phase = PhaseFailed
		}
		session.mu.Unlock()
		close(session.exited)
	}()

	if err := session.awaitReady(ctx, localPort); err != nil {
		session.Stop()
		return nil, err
	}
	return session, nil
}

// readMarkers watches stdout for phase markers, keeping every line for
// the failure report.
func (s *Session) readMarkers(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		s.record(line)
		switch {
		case line == "ETA:installing":
			s.setPhase(PhaseInstalling)
		case line == "ETA:starting":
			s.setPhase(PhaseStarting)
		case strings.HasPrefix(line, "ETA:fail:"):
			s.mu.Lock()
			s.err = errors.New(strings.TrimPrefix(line, "ETA:fail:"))
			s.mu.Unlock()
		}
	}
}

func (s *Session) drain(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			s.record(line)
		}
	}
}

// failure prefers the remote's own reported reason, falling back to the
// last thing it said.
func (s *Session) failure(waitErr error) error {
	if s.err != nil {
		return s.err
	}
	tail := s.recent
	if len(tail) > 5 {
		tail = tail[len(tail)-5:]
	}
	detail := strings.Join(tail, "; ")
	if detail == "" {
		if waitErr != nil {
			return fmt.Errorf("the SSH connection to %s ended without eta starting: %w", s.destination, waitErr)
		}
		return fmt.Errorf("the SSH connection to %s ended without eta starting", s.destination)
	}
	return fmt.Errorf("eta did not start on %s: %s", s.destination, detail)
}

// awaitReady polls until the forwarded port serves eta. Decided locally,
// not by a remote marker or a remote curl: it checks the thing that
// matters (reachable from here) and the remote needs no HTTP client.
// Gives up the moment ssh dies rather than waiting out its own timeout.
func (s *Session) awaitReady(ctx context.Context, localPort int) error {
	ctx, cancel := context.WithTimeout(ctx, establishTimeout)
	defer cancel()

	url := "http://127.0.0.1:" + strconv.Itoa(localPort) + "/api/healthz"
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		select {
		case <-s.exited:
			return s.Wait()
		case <-ctx.Done():
			return fmt.Errorf("eta did not answer on %s within %s: %w", s.destination, establishTimeout, ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			continue
		}
		response.Body.Close()
		if response.StatusCode == http.StatusOK {
			s.setPhase(PhaseReady)
			return nil
		}
	}
}

// Cleanup removes eta's files from a remote. Separate from Stop: stopping
// is routine, removing isn't.
func Cleanup(ctx context.Context, destination string) error {
	sh, err := detectShell(ctx, destination)
	if err != nil {
		return err
	}
	if _, err := runSSH(ctx, destination, cleanupCommand(sh)); err != nil {
		return fmt.Errorf("could not remove eta's files from %s: %w", destination, err)
	}
	return nil
}

// Manager keeps one session per destination. Without it, double-clicking
// twice starts a second remote eta that fails to bind, for no reason the
// user could act on.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	starting map[string]chan struct{}
}

func NewManager() *Manager {
	return &Manager{
		sessions: map[string]*Session{},
		starting: map[string]chan struct{}{},
	}
}

// Connect returns a live session, establishing one only if needed.
func (m *Manager) Connect(ctx context.Context, opts Options) (*Session, error) {
	if err := validateDestination(opts.Destination); err != nil {
		return nil, err
	}
	for {
		m.mu.Lock()
		if existing, ok := m.sessions[opts.Destination]; ok {
			select {
			case <-existing.exited:
				delete(m.sessions, opts.Destination)
			default:
				m.mu.Unlock()
				return existing, nil
			}
		}
		if wait, ok := m.starting[opts.Destination]; ok {
			// Already being established; wait rather than race, then recheck.
			m.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		done := make(chan struct{})
		m.starting[opts.Destination] = done
		m.mu.Unlock()

		session, err := Start(ctx, opts)

		m.mu.Lock()
		delete(m.starting, opts.Destination)
		if err == nil {
			m.sessions[opts.Destination] = session
		}
		m.mu.Unlock()
		close(done)
		return session, err
	}
}

// Get returns the live session for a destination, if any.
func (m *Manager) Get(destination string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[destination]
	if !ok {
		return nil, false
	}
	select {
	case <-session.exited:
		delete(m.sessions, destination)
		return nil, false
	default:
		return session, true
	}
}

// Disconnect stops one session.
func (m *Manager) Disconnect(destination string) {
	m.mu.Lock()
	session, ok := m.sessions[destination]
	delete(m.sessions, destination)
	m.mu.Unlock()
	if ok {
		session.Stop()
	}
}

// StopAll ends every session, and so every remote eta this computer
// started. For shutdown: the remote would exit on its own once stdin
// closed, but not promptly enough to leave to a timeout.
func (m *Manager) StopAll() {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for destination, session := range m.sessions {
		all = append(all, session)
		delete(m.sessions, destination)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, session := range all {
		wg.Add(1)
		go func(s *Session) { defer wg.Done(); s.Stop() }(session)
	}
	wg.Wait()
}
