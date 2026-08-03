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
	// Short: this only asks whether something is already answering, and a
	// PC that isn't running eta must not be made to wait for the real
	// work to start.
	adoptTimeout = 12 * time.Second
	recentLines  = 40
	// eta's own default port. An eta already running on a PC is almost
	// always on this, because that is what starting it with no flags
	// gives you.
	defaultRemotePort = 7080
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
	// Port to look for an eta already running on that PC. Zero means
	// eta's own default. Set in tests so they never probe -- or adopt --
	// a real instance the developer is running on this machine.
	RemotePort int
}

func (o Options) remotePort() int {
	if o.RemotePort > 0 {
		return o.RemotePort
	}
	return defaultRemotePort
}

// Session is one running remote eta, reachable at URL while alive.
type Session struct {
	destination string
	url         string
	localPort   int
	adopted     bool
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

// Adopted reports that this session attached to an eta that was already
// running on that PC, rather than installing and starting one. An
// adopted eta is not ours: nothing was installed for it, and ending the
// session leaves it running.
func (s *Session) Adopted() bool { return s.adopted }

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
	// An adopted session runs no remote command, so there is nothing on
	// the far side to shut down gracefully and nothing to wait for. Only
	// the tunnel goes; the eta it reached keeps running, because this
	// computer never started it.
	if s.adopted {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		<-s.exited
		return
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

// Err is why the session failed, or nil. Unlike Wait it does not block,
// so a status request can report a failure without waiting for one.
func (s *Session) Err() error {
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
	return sshArgsMode(destination, forward, false)
}

// sshArgsMode adds -N for a tunnel that runs no remote command at all.
// That is what adopting an already-running eta needs, and it is why
// adopting works on a PC whose ssh shell this package could not otherwise
// use: with no command to run, the shell never matters.
func sshArgsMode(destination string, forward string, tunnelOnly bool) ([]string, error) {
	if err := validateDestination(destination); err != nil {
		return nil, err
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-T",
	}
	if tunnelOnly {
		args = append(args, "-N")
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

// Start is Begin plus WaitReady: convenient when the caller has nothing
// to do until the session works.
func Start(ctx context.Context, opts Options) (*Session, error) {
	session, err := Begin(ctx, opts)
	if err != nil {
		return nil, err
	}
	if err := session.WaitReady(ctx); err != nil {
		session.Stop()
		return nil, err
	}
	return session, nil
}

// Begin connects to a PC, returning as soon as the ssh process is
// spawned — before it is ready. Callers reporting progress need the
// session while it converges: a first install compiles from source and
// can take minutes, and a session that only exists once it works cannot
// be observed getting there.
//
// An eta already running on that PC is adopted rather than replaced. That
// is checked first, and if it answers, nothing is installed and nothing
// is started — see adopt.
func Begin(ctx context.Context, opts Options) (*Session, error) {
	if err := validateDestination(opts.Destination); err != nil {
		return nil, err
	}
	if session, ok := adopt(ctx, opts.Destination, opts.remotePort()); ok {
		return session, nil
	}
	return install(ctx, opts)
}

// adopt attaches to an eta already listening on that PC, by forwarding to
// its port and asking it directly. It returns false if nothing answers,
// leaving the caller to install one.
//
// This exists so that a PC someone already started eta on is connected
// to, not competed with. Without it, setting such a PC up would install
// over the running one's binary and start a second instance beside it,
// leaving two etas on one machine and the user's own no longer the one
// being talked to.
//
// The tunnel runs no remote command (-N), which has three consequences,
// all wanted:
//
//   - Nothing is installed and nothing is started, so an eta installed by
//     any other means -- a package, a service, a manual build -- is
//     adopted just as happily as one this package installed.
//   - Ending the session closes only the tunnel. The adopted eta keeps
//     running, because it was never ours to stop.
//   - It needs no shell on the far side, so this works even on a PC whose
//     ssh shell this package would otherwise refuse.
//
// Whether it is really eta answering is decided by asking it, not by
// finding something bound to the port: anything at all can be listening
// on 7080, and only eta answers its own health endpoint.
func adopt(ctx context.Context, destination string, remotePort int) (*Session, bool) {
	localPort, err := freeLocalPort()
	if err != nil {
		return nil, false
	}
	args, err := sshArgsMode(destination,
		fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", localPort, remotePort), true)
	if err != nil {
		return nil, false
	}
	if _, err := exec.LookPath(sshBinary); err != nil {
		return nil, false
	}

	cmd := exec.Command(sshBinary, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, false
	}
	if err := cmd.Start(); err != nil {
		return nil, false
	}

	session := &Session{
		destination: destination,
		url:         "http://127.0.0.1:" + strconv.Itoa(localPort),
		localPort:   localPort,
		adopted:     true,
		phase:       PhaseChecking,
		cmd:         cmd,
		stdin:       stdin,
		exited:      make(chan struct{}),
	}
	adoptCtx, cancel := context.WithTimeout(ctx, adoptTimeout)
	defer cancel()

	var drained sync.WaitGroup
	drained.Add(2)
	go func() { defer drained.Done(); session.drain(stdout) }()
	go func() {
		defer drained.Done()
		// Watching for ssh reporting that the forward could not reach
		// anything, so a PC with no eta running gives up at once instead
		// of waiting out the timeout. Without this every fresh install
		// pays the full probe first -- measured at twelve dead seconds
		// before any work starts.
		//
		// The timeout above remains the real bound; this only makes the
		// common case fast, so a change in ssh's wording costs speed
		// rather than correctness.
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			session.record(line)
			if strings.Contains(line, "open failed") ||
				strings.Contains(line, "connect failed") ||
				strings.Contains(line, "Connection refused") {
				cancel()
			}
		}
	}()
	go func() {
		waitErr := cmd.Wait()
		drained.Wait()
		session.mu.Lock()
		if session.phase != PhaseReady {
			// The phase flip always happens, even when readMarkers has
			// already set err on an ETA:fail marker. The marker's err
			// is the user-visible reason; the wait goroutine's role here
			// is the phase. Splitting the guard is the difference between
			// the browser seeing "failed" and seeing the session sit at
			// "checking" until the manager rips it out.
			if session.err == nil {
				session.err = session.failure(waitErr)
			}
			session.phase = PhaseFailed
		}
		session.mu.Unlock()
		close(session.exited)
	}()

	if err := session.WaitReady(adoptCtx); err != nil {
		session.Stop()
		return nil, false
	}
	return session, true
}

// install is the path for a PC with no eta running on it: put one there
// and start it, leashed to this connection.
func install(ctx context.Context, opts Options) (*Session, error) {
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
		if session.phase != PhaseReady {
			// The phase flip always happens, even when readMarkers has
			// already set err on an ETA:fail marker. The marker's err
			// is the user-visible reason; the wait goroutine's role here
			// is the phase. Splitting the guard is the difference between
			// the browser seeing "failed" and seeing the session sit at
			// "checking" until Manager.Connect rips it out.
			if session.err == nil {
				session.err = session.failure(waitErr)
			}
			session.phase = PhaseFailed
		}
		session.mu.Unlock()
		close(session.exited)
	}()

	session.localPort = localPort
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
			// The phase flip is part of the marker's job, not the wait
			// goroutine's: a single line on stdout is the failure, and
			// the browser's status poll looks at the phase first. Leaving
			// the phase at "checking" or "installing" until ssh exits
			// races Manager.Connect's Disconnect, and the user sees
			// "disconnected" instead of the reason they came for.
			s.phase = PhaseFailed
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

// WaitReady polls until the forwarded port serves eta. Decided locally,
// not by a remote marker or a remote curl: it checks the thing that
// matters (reachable from here) and the remote needs no HTTP client.
// Gives up the moment ssh dies rather than waiting out its own timeout.
//
// Safe to call more than once, and returns immediately once ready, so
// several callers can wait on the same converging session.
func (s *Session) WaitReady(ctx context.Context) error {
	if s.Phase() == PhaseReady {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, establishTimeout)
	defer cancel()

	url := "http://127.0.0.1:" + strconv.Itoa(s.localPort) + "/api/healthz"
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

// ErrAdopted reports an action that only makes sense for an eta this
// computer installed, attempted on one that was already running.
var ErrAdopted = errors.New("eta on that PC was already running and was not installed from here, so there is nothing here to remove")

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
	// failed memoizes the last Begin-time error for a destination, so a
	// status poll that lands before any session exists — the case for
	// pre-session failures (dev-build refusal, detectShell, missing ssh
	// on this machine) and for the brief starting window while a
	// adopt-probe/detectShell is still running — can answer with the
	// real reason instead of "disconnected". Cleared at the start of
	// every new attempt.
	failed map[string]error
}

func NewManager() *Manager {
	return &Manager{
		sessions: map[string]*Session{},
		starting: map[string]chan struct{}{},
		failed:   map[string]error{},
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
		// A new attempt invalidates any memo from a previous one. The
		// user clicked Set up again; the last error is not what they
		// are asking about now.
		delete(m.failed, opts.Destination)
		m.mu.Unlock()

		// Registered as soon as the process exists, not once it works, so
		// its phase is observable while it converges.
		session, err := Begin(ctx, opts)
		m.mu.Lock()
		delete(m.starting, opts.Destination)
		if err == nil {
			m.sessions[opts.Destination] = session
			delete(m.failed, opts.Destination)
		} else {
			// Begin failed before a session existed: dev-build refusal,
			// detectShell, freeLocalPort, missing ssh, pipe/Start. A
			// status poll during or after the failure has nothing else
			// to report against, so the reason is memoized until the
			// next attempt or an explicit reset.
			m.failed[opts.Destination] = err
		}
		m.mu.Unlock()
		close(done)
		if err != nil {
			return nil, err
		}
		if err := session.WaitReady(ctx); err != nil {
			// Stop the process — on a timeout it is still running, and
			// Stop is the only thing that closes its stdin and lets ssh
			// exit — but keep the session registered in m.sessions. It
			// is the only place that carries phase, err, and recent, and
			// a status poll ~50 ms after the POST is exactly what the
			// browser is doing. Disconnect here would race that poll:
			// the next poll would find the map empty and report
			// "disconnected" instead of the actual reason.
			session.Stop()
			return nil, err
		}
		return session, nil
	}
}

// ConnectAsync starts a connection and returns straight away. The caller
// polls Get for progress. Used by the HTTP layer, where a first connect
// can take minutes and holding a request open for it is not an option.
func (m *Manager) ConnectAsync(opts Options) error {
	if err := validateDestination(opts.Destination); err != nil {
		return err
	}
	if _, live := m.Get(opts.Destination); live {
		return nil
	}
	go func() {
		// Deliberately not tied to the request's context: the connection
		// outlives the request that asked for it.
		_, _ = m.Connect(context.Background(), opts)
	}()
	return nil
}

// Pending reports a session that exists but is not ready yet, so status
// can distinguish "still working" from "never started".
func (m *Manager) Pending(destination string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[destination]
	return session, ok
}

// Starting reports whether a connect attempt is in progress for a
// destination but has not yet produced a session. The status handler
// answers "connecting" for this case, since a poll that lands a few
// hundred ms after the POST — before Begin's adopt-probe/detectShell
// completes — would otherwise see no session and report "disconnected"
// even though a connection is on its way.
func (m *Manager) Starting(destination string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.starting[destination]
	return ok
}

// LastFailure returns the memoized Begin-time error for a destination,
// if one exists. The status handler renders it as a failed setup
// rather than as a disconnected one, so a user who hit "Go not
// installed" or "Windows cmd.exe" actually sees that — the wording is
// the only thing the install path can say, and it used to be lost.
func (m *Manager) LastFailure(destination string) (error, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	err, ok := m.failed[destination]
	return err, ok
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
