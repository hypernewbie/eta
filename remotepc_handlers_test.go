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
