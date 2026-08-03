package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hypernewbie/eta/internal/peers"
)

func remotePCTestServer(t *testing.T) *server {
	t.Helper()
	s, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// newServer leaves the inventory unset; main() supplies it. A temp one
	// here, never the real user config directory.
	s.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	t.Cleanup(s.remotePCs.StopAll)
	return s
}

// An instance can run with no peer inventory at all, as every other
// peer-touching handler allows. Status must answer rather than panic.
func TestRemotePCStatusWithoutAPeerInventory(t *testing.T) {
	s, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.remotePCs.StopAll)
	s.peers = nil
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/remote-pc?destination=never-seen", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body)
	}
}

func decodeStatus(t *testing.T, body []byte) remotePCStatus {
	t.Helper()
	var status remotePCStatus
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return status
}

// A destination ssh would read as an option must be refused at the edge,
// before anything spawns. Otherwise "-oProxyCommand=..." runs a command
// on the machine serving this API, reachable from the browser.
func TestRemotePCConnectRefusesAnOptionLikeDestination(t *testing.T) {
	s := remotePCTestServer(t)
	for _, bad := range []string{"-oProxyCommand=touch /tmp/x", "--version", "-"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/remote-pc",
			strings.NewReader(`{"destination":`+jsonQuote(bad)+`}`))
		s.routes().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("destination %q: expected 400, got %d (%s)", bad, recorder.Code, recorder.Body)
		}
	}
}

func TestRemotePCConnectRequiresADestination(t *testing.T) {
	s := remotePCTestServer(t)
	for _, body := range []string{`{}`, `{"destination":""}`, `{"destination":"   "}`} {
		recorder := httptest.NewRecorder()
		s.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/remote-pc", strings.NewReader(body)))
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("body %s: expected 400, got %d", body, recorder.Code)
		}
	}
}

// Status for a PC that was never connected is a normal answer, not an
// error: the UI asks about PCs it knows of, including ones that are off.
func TestRemotePCStatusForAnUnknownDestinationIsDisconnected(t *testing.T) {
	s := remotePCTestServer(t)
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/remote-pc?destination=never-seen", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body)
	}
	if status := decodeStatus(t, recorder.Body.Bytes()); status.Phase != "disconnected" {
		t.Fatalf("expected phase disconnected, got %q", status.Phase)
	}
}

func TestRemotePCStatusRequiresADestination(t *testing.T) {
	s := remotePCTestServer(t)
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/remote-pc", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
}

// Disconnecting a PC that isn't connected must not error: the UI can send
// it on a session that already ended, and that is not a failure.
func TestRemotePCDisconnectIsSafeWhenNothingIsConnected(t *testing.T) {
	s := remotePCTestServer(t)
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/remote-pc?destination=never-seen", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body)
	}
}

// Connecting returns immediately rather than holding the request open for
// what may be a multi-minute first install, and reports a phase the UI
// can poll on.
func TestRemotePCConnectIsAsynchronous(t *testing.T) {
	s := remotePCTestServer(t)
	recorder := httptest.NewRecorder()
	// A destination that will fail to resolve: the point is that the
	// handler answers at once regardless of what the connection does.
	s.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/remote-pc",
		strings.NewReader(`{"destination":"eta-nonexistent-host.invalid"}`)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d (%s)", recorder.Code, recorder.Body)
	}
	if status := decodeStatus(t, recorder.Body.Bytes()); status.Destination != "eta-nonexistent-host.invalid" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

// The peer entry is written only once the session actually works. A PC
// that never came up must not be left in the inventory as though it had.
func TestRemotePCDoesNotRecordAPeerUntilItIsReady(t *testing.T) {
	s := remotePCTestServer(t)
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/remote-pc?destination=never-seen", nil))
	list, err := s.peers.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no peer recorded for a PC that never connected, got %+v", list)
	}
}

// jsonQuote is a tiny helper so the injection cases above can be embedded
// in a request body safely.
func jsonQuote(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

// hostOnly drops the port from an "host:port" address so it can be
// used as the -L bind without producing a 5-field spec ssh rejects.
// Regression: a request Host of "charon:7080" was being passed
// through unchanged, which made the install command
//   ssh ... -L charon:7080:<forwardPort>:127.0.0.1:<remotePort> ...
// and ssh reported "Bad local forwarding specification".
func TestHostOnlyStripsPort(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"charon", "charon"},
		{"charon:7080", "charon"},
		{"192.168.1.10:7080", "192.168.1.10"},
		{"[::1]:7080", "::1"},
		{"100.92.136.40", "100.92.136.40"},
	}
	for _, c := range cases {
		if got := hostOnly(c.in); got != c.want {
			t.Errorf("hostOnly(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A successful SSH setup must hand the browser a peer with name, id,
// accent, and glyph populated. The browser renders every peer with
// peer.name.toUpperCase() and the destination-stamped accent and glyph
// in refreshEtaMenu and desktopIconModel, so a record without those
// fields is a guaranteed "peer.name is undefined" crash on the very
// first paint of the new PC.
//
// handlePeerAdd has always probed /api/identity for the same reason;
// the SSH setup path was the seam. A reconnect on the same destination
// must reuse the identity from the existing record rather than
// re-probing, since the URL alone changes every session and a re-probe
// would race the user's click and could substitute different identity
// fields if the remote was renamed between sessions.
func TestRemotePCSetupPopulatesPeerIdentityOnReady(t *testing.T) {
	// A real Eta server stood up for the duration of the test: it
	// responds to /api/identity the way a freshly installed remote
	// would, which is what handleRemotePCStatus is going to probe
	// against the session's forwarded URL.
	remote, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	remoteHTTP := httptest.NewServer(remote.routes())
	defer remoteHTTP.Close()

	s := remotePCTestServer(t)

	// A session in PhaseReady at the remote's URL — exactly the state
	// the status handler sees when a real setup finishes.
	s.remotePCs.SetSessionForTest("minerva", remoteHTTP.URL)

	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/remote-pc?destination=minerva", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body)
	}
	status := decodeStatus(t, recorder.Body.Bytes())
	if status.Phase != "ready" {
		t.Fatalf("expected phase ready, got %q", status.Phase)
	}

	after, found, err := s.peers.FindBySSHDestination("minerva")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("the peer record was not created on PhaseReady")
	}
	if after.Name == "" {
		t.Fatal("peer.name must be populated; the browser's peer.name.toUpperCase() crashes otherwise")
	}
	if after.ID == "" || after.Accent == "" || after.Glyph == "" {
		t.Errorf("peer identity fields must all be populated, got %+v", after)
	}

	// A reconnect on the same destination must preserve the identity
	// from the existing record rather than re-probing, since the URL
	// alone changes every session and a re-probe would race the
	// user's click.
	s.remotePCs.SetSessionForTest("minerva", "http://127.0.0.1:1") // a different forwarded port
	recorder = httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/remote-pc?destination=minerva", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("reconnect: expected 200, got %d (%s)", recorder.Code, recorder.Body)
	}
	preserved, _, _ := s.peers.FindBySSHDestination("minerva")
	if preserved.Name != after.Name || preserved.Accent != after.Accent || preserved.Glyph != after.Glyph {
		t.Errorf("a reconnect must preserve identity; got %+v, want %+v", preserved, after)
	}
}
