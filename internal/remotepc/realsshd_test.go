//go:build realsshd

// Behind a build tag: needs a real sshd, takes seconds not milliseconds.
//
//	go test -tags realsshd ./internal/remotepc/
//
// session_test.go replaces ssh with a script, which can't prove the
// things this feature rests on: that a real ssh and sshd set up the
// forward, that eta is reachable through it, and that the stdin leash
// really stops the remote process. Those are what would silently be wrong.
package remotepc

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type sshdFixture struct {
	port     int
	fakeHome string
	shim     string
}

// startRealSSHD runs an unprivileged sshd on loopback with throwaway keys,
// forcing the session's HOME and PATH so the test never touches the real
// home directory and can supply a stub `go`.
func startRealSSHD(t *testing.T) *sshdFixture {
	t.Helper()
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		for _, candidate := range []string{"/usr/sbin/sshd", "/usr/local/sbin/sshd"} {
			if _, statErr := os.Stat(candidate); statErr == nil {
				sshd = candidate
				err = nil
				break
			}
		}
	}
	if err != nil || sshd == "" {
		t.Skip("no sshd available")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen available")
	}

	dir := t.TempDir()
	hostKey := filepath.Join(dir, "host_ed25519")
	clientKey := filepath.Join(dir, "id_ed25519")
	for _, key := range []string{hostKey, clientKey} {
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
			t.Fatalf("ssh-keygen: %v\n%s", err, out)
		}
	}

	// A HOME the test owns, and a PATH with a stub `go` first.
	fakeHome := filepath.Join(dir, "home")
	stubBin := filepath.Join(dir, "stubbin")
	for _, d := range []string{fakeHome, stubBin, filepath.Join(fakeHome, ".eta", "bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// `go install` is stubbed — whether it works is Go's business, and
	// stubbing keeps this hermetic. `go clean` is passed through to the
	// real toolchain, because cleanup depends on it actually clearing a
	// read-only module cache; stubbing it would prove nothing.
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain to delegate `go clean` to")
	}
	stubGo := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"clean\" ]; then exec %s \"$@\"; fi\nexit 0\n", realGo)
	if err := os.WriteFile(filepath.Join(stubBin, "go"), []byte(stubGo), 0o755); err != nil {
		t.Fatal(err)
	}
	// The real eta, where `go install` would have put it.
	etaBinary := filepath.Join(fakeHome, ".eta", "bin", "eta")
	build := exec.Command("go", "build", "-o", etaBinary, ".")
	build.Dir = ".."
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build eta: %v\n%s", err, out)
	}

	publicKey, keyErr := os.ReadFile(clientKey + ".pub")
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	authorized := filepath.Join(dir, "authorized_keys")
	// environment= needs PermitUserEnvironment: how HOME and PATH get
	// overridden without touching the real account.
	entry := fmt.Sprintf("environment=\"HOME=%s\",environment=\"PATH=%s:/usr/bin:/bin\" %s",
		fakeHome, stubBin, strings.TrimSpace(string(publicKey)))
	if err := os.WriteFile(authorized, []byte(entry+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	configPath := filepath.Join(dir, "sshd_config")
	config := fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %s
AuthorizedKeysFile %s
PermitUserEnvironment yes
AllowTcpForwarding yes
UsePAM no
StrictModes no
PasswordAuthentication no
KbdInteractiveAuthentication no
PidFile %s
`, port, hostKey, authorized, filepath.Join(dir, "sshd.pid"))
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	server := exec.Command(sshd, "-D", "-e", "-f", configPath)
	var serverLog strings.Builder
	server.Stdout = &serverLog
	server.Stderr = &serverLog
	if err := server.Start(); err != nil {
		t.Fatalf("start sshd: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_ = server.Wait()
	})

	knownHosts := filepath.Join(dir, "known_hosts")
	// A wrapper adding only what a user's own ~/.ssh/config would supply:
	// key, port, known_hosts. Production passes none of these, because
	// resolving them is ssh's job.
	shim := filepath.Join(dir, "ssh-shim")
	shimBody := fmt.Sprintf(`#!/bin/sh
exec ssh -i %s -o UserKnownHostsFile=%s -o StrictHostKeyChecking=accept-new -p %d "$@"
`, clientKey, knownHosts, port)
	if err := os.WriteFile(shim, []byte(shimBody), 0o755); err != nil {
		t.Fatal(err)
	}

	fixture := &sshdFixture{port: port, fakeHome: fakeHome, shim: shim}

	// Wait for sshd, and accept its host key on first contact.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("sshd did not accept connections on 127.0.0.1:%d\n%s", port, serverLog.String())
		}
		out, err := exec.Command(shim, "-o", "BatchMode=yes", "127.0.0.1", "echo ready").CombinedOutput()
		if err == nil && strings.Contains(string(out), "ready") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fixture
}

func (f *sshdFixture) use(t *testing.T) {
	t.Helper()
	old := sshBinary
	sshBinary = f.shim
	t.Cleanup(func() { sshBinary = old })
}

func answers(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

// TestRealSSHDSessionAndLeash is the end-to-end proof: a real session over
// a real ssh to a real sshd, eta reachable through the forward, and then
// the part nothing else can check — that stopping actually stops the
// remote eta rather than just dropping the tunnel.
//
// It probes the remote's own port, not the forwarded one: losing the
// forward makes the local URL fail either way, so the local URL can't
// tell "leash worked" from "tunnel closed".
func TestRealSSHDSessionAndLeash(t *testing.T) {
	fixture := startRealSSHD(t)
	fixture.use(t)

	session, err := Start(context.Background(), Options{
		Destination: "127.0.0.1",
		Module:      "github.com/hypernewbie/eta",
		Version:     "v0.0.0-test",
		// A port nothing owns, so this exercises the install path. Left
		// at the default it would adopt whatever is on 7080 -- including
		// a developer's own running Eta -- and then rightly refuse to
		// kill it, failing this leash assertion for the wrong reason.
		RemotePort: freePort(t),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if session.Phase() != PhaseReady {
		t.Fatalf("expected PhaseReady, got %q (recent: %v)", session.Phase(), session.Recent())
	}
	if !answers(session.URL() + "/api/healthz") {
		t.Fatalf("eta is not reachable at %s despite Start reporting success", session.URL())
	}

	// The remote's own port, from the forward this session set up, so the
	// leash is observable independently of the tunnel.
	remoteURL := ""
	for _, arg := range session.cmd.Args {
		if strings.HasPrefix(arg, "127.0.0.1:") && strings.Count(arg, ":") == 3 {
			parts := strings.Split(arg, ":")
			remoteURL = "http://127.0.0.1:" + parts[3]
		}
	}
	if remoteURL == "" {
		t.Fatalf("could not find the forward in ssh args: %v", session.cmd.Args)
	}
	if !answers(remoteURL + "/api/healthz") {
		t.Fatalf("expected the remote eta to be listening on %s", remoteURL)
	}

	session.Stop()

	// It must have exited on its own from stdin EOF — nothing killed it
	// remotely.
	deadline := time.Now().Add(20 * time.Second)
	for {
		if !answers(remoteURL + "/api/healthz") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the remote eta is still serving %s after the session stopped — the stdin leash did not hold, and a remote process has been left running", remoteURL)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Connect idempotency against a real connection: asking twice must not
// start a second eta, which would fail to bind and confuse the user.
func TestRealSSHDManagerReusesOneRemoteProcess(t *testing.T) {
	fixture := startRealSSHD(t)
	fixture.use(t)

	manager := NewManager()
	options := Options{
		Destination: "127.0.0.1",
		Module:      "github.com/hypernewbie/eta",
		Version:     "v0.0.0-test",
		RemotePort:  freePort(t), // never probe a real instance on 7080
	}
	t.Cleanup(manager.StopAll)

	first, err := manager.Connect(context.Background(), options)
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}
	second, err := manager.Connect(context.Background(), options)
	if err != nil {
		t.Fatalf("second connect: %v", err)
	}
	if first != second {
		t.Fatal("connecting twice started a second session instead of reusing the live one")
	}
	if !answers(first.URL() + "/api/healthz") {
		t.Fatalf("session not reachable at %s", first.URL())
	}
}

// Cleanup must leave nothing under ~/.eta. This is the test that caught
// the read-only module cache defeating the removal.
func TestRealSSHDCleanupRemovesEverything(t *testing.T) {
	fixture := startRealSSHD(t)
	fixture.use(t)

	etaDir := filepath.Join(fixture.fakeHome, ".eta")
	if _, err := os.Stat(etaDir); err != nil {
		t.Fatalf("expected %s to exist before cleanup: %v", etaDir, err)
	}
	// What Go's module cache leaves without -modcacherw: a read-only file
	// in a read-only dir. Made read-only leaf-first, since creating it
	// read-only would leave nothing able to create its children.
	readOnlyDir := filepath.Join(etaDir, "pkg", "mod", "example.com", "m@v1")
	if err := os.MkdirAll(readOnlyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	readOnlyFile := filepath.Join(readOnlyDir, "go.mod")
	if err := os.WriteFile(readOnlyFile, []byte("module example.com/m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readOnlyFile, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readOnlyDir, 0o555); err != nil {
		t.Fatal(err)
	}
	// Restore write permission so t.TempDir's removal works even if the
	// assertion below fails.
	t.Cleanup(func() { _ = os.Chmod(readOnlyDir, 0o755) })

	if err := Cleanup(context.Background(), "127.0.0.1"); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(etaDir); !os.IsNotExist(err) {
		remaining := []string{}
		_ = filepath.Walk(etaDir, func(path string, info os.FileInfo, _ error) error {
			remaining = append(remaining, path)
			return nil
		})
		t.Fatalf("%s still exists after cleanup (%v); remaining: %v", etaDir, err, remaining)
	}
	_ = strconv.Itoa(fixture.port)
}

// TestRealSSHDAdoptsAnAlreadyRunningEta is the case where someone already
// started eta on that PC themselves. It must be connected to, not
// competed with: installing over it and starting a second instance beside
// it would leave two etas on one machine, with the user's own no longer
// the one being talked to.
//
// Proven against a real eta on the real default port, reached over a real
// ssh forward -- and then, after the session ends, still running, because
// this computer never started it.
func TestRealSSHDAdoptsAnAlreadyRunningEta(t *testing.T) {
	fixture := startRealSSHD(t)
	fixture.use(t)

	// Someone's own eta, already running on the default port. Started
	// here with no leash at all, exactly as a person running it would.
	// Deliberately NOT defaultRemotePort: this machine is the "remote"
	// for these tests, and a developer running Eta normally has 7080
	// bound. Probing it would adopt their live instance instead of the
	// one this test started, and the assertions would be about their
	// process rather than anything this test controls.
	adoptPort := freePort(t)
	etaBinary := filepath.Join(fixture.fakeHome, ".eta", "bin", "eta")
	theirs := exec.Command(etaBinary,
		"--ip", "127.0.0.1",
		"--port", strconv.Itoa(adoptPort),
		"--root", t.TempDir(),
	)
	theirs.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+t.TempDir(),
		"XDG_CACHE_HOME="+t.TempDir(),
	)
	if err := theirs.Start(); err != nil {
		t.Fatalf("start the already-running eta: %v", err)
	}
	defer func() {
		_ = theirs.Process.Kill()
		_ = theirs.Wait()
	}()
	theirURL := "http://127.0.0.1:" + strconv.Itoa(adoptPort)
	deadline := time.Now().Add(20 * time.Second)
	for !answers(theirURL + "/api/healthz") {
		if time.Now().After(deadline) {
			t.Skipf("could not start an eta on port %d to adopt", adoptPort)
		}
		time.Sleep(100 * time.Millisecond)
	}

	session, err := Start(context.Background(), Options{
		Destination: "127.0.0.1",
		Module:      "github.com/hypernewbie/eta",
		Version:     "v0.0.0-test",
		RemotePort:  adoptPort,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !session.Adopted() {
		t.Fatal("expected the already-running eta to be adopted, not replaced")
	}
	if !answers(session.URL() + "/api/healthz") {
		t.Fatalf("adopted session is not reachable at %s", session.URL())
	}
	// Nothing was installed and nothing was started: the only eta on that
	// PC is still the one that was already there.
	if !answers(theirURL + "/api/healthz") {
		t.Fatal("the already-running eta stopped answering after being adopted")
	}

	session.Stop()

	// The decisive assertion: ending our session must leave their eta
	// alone. It was never ours to stop.
	time.Sleep(500 * time.Millisecond)
	if !answers(theirURL + "/api/healthz") {
		t.Fatal("ending the session killed an eta this computer did not start")
	}
}

// freePort returns a port with nothing on it, so a test never probes a
// fixed one that a real instance on this machine might own.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
