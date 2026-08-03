package main

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// buildEtaBinary builds the real program: the stdin leash only exists at
// the process boundary, so it can't be tested in-process.
func buildEtaBinary(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "eta")
	cmd := exec.Command("go", "build", "-o", out, ".")
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build eta: %v\n%s", err, combined)
	}
	return out
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForHealthz(t *testing.T, port int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/api/healthz"
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("eta did not answer %s within %s", url, within)
}

// TestExitOnStdinCloseStopsWhenStdinCloses guards the leash the remote-PC
// feature depends on: an eta started over SSH must exit when that
// connection ends, leaving nothing running on the remote.
//
// Reproduces the real condition — real process, real pipe, confirmed
// serving, then close the pipe and require it to exit unaided. This is
// the one mechanism the design can't fall back from.
func TestExitOnStdinCloseStopsWhenStdinCloses(t *testing.T) {
	binary := buildEtaBinary(t)
	port := freePort(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := t.TempDir()

	cmd := exec.Command(binary,
		"--exit-on-stdin-close",
		"--root", root,
		"--port", strconv.Itoa(port),
		"--ip", "127.0.0.1",
	)
	// Every persistent-state path redirected into a temp dir: a test
	// must never touch the real user config directory.
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+config,
		"XDG_CACHE_HOME="+t.TempDir(),
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		// Only reached on failure; a passing run already reaped it.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	waitForHealthz(t, port, 15*time.Second)

	// The assertion: closing stdin, and nothing else, must stop it.
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-exited:
		// Exit status not asserted: same graceful path as SIGTERM. What
		// matters is it ended without being killed.
	case <-time.After(15 * time.Second):
		t.Fatal("eta did not exit after its stdin closed — the SSH leash the remote-bootstrap design relies on does not hold")
	}
}

// TestWithoutExitOnStdinCloseAClosedStdinIsIgnored is the other half: the
// flag must be opt-in, so an eta started from a shell, launcher or
// service manager — any of which may hand it a stdin that closes at once
// — doesn't exit unexpectedly.
func TestWithoutExitOnStdinCloseAClosedStdinIsIgnored(t *testing.T) {
	binary := buildEtaBinary(t)
	port := freePort(t)
	root := t.TempDir()
	config := t.TempDir()

	cmd := exec.Command(binary,
		"--root", root,
		"--port", strconv.Itoa(port),
		"--ip", "127.0.0.1",
	)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+config,
		"XDG_CACHE_HOME="+t.TempDir(),
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	waitForHealthz(t, port, 15*time.Second)
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}

	// Still serving a moment later: closed stdin was ignored.
	time.Sleep(500 * time.Millisecond)
	waitForHealthz(t, port, 2*time.Second)
}
