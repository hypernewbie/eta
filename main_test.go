package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hypernewbie/eta/internal/diskcache"
	"github.com/hypernewbie/eta/internal/mdns"
	"github.com/hypernewbie/eta/internal/peers"
	"github.com/hypernewbie/eta/internal/roots"
	"github.com/hypernewbie/eta/internal/terminal"
	"github.com/hypernewbie/eta/internal/tmux"
	"github.com/hypernewbie/eta/internal/transfer"
	"github.com/hypernewbie/eta/internal/uistate"
	dnsmsg "github.com/miekg/dns"
)

func TestMediaTypes(t *testing.T) {
	for name, want := range map[string]string{
		"song.mp3": "audio/mpeg", "song.ogg": "audio/ogg", "song.wav": "audio/wav",
		"movie.mp4": "video/mp4", "movie.webm": "video/webm",
	} {
		if got := mediaType(name); got != want {
			t.Errorf("mediaType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCoordinatorDeletesRemoteSourceAfterMove(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.bin"), []byte("eta"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := newServer([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	sourceHTTP := httptest.NewServer(source.routes())
	defer sourceHTTP.Close()
	coordinator, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	if err := coordinator.peers.Add(peers.Peer{URL: sourceHTTP.URL}); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/remote/delete?peer="+url.QueryEscape(sourceHTTP.URL), strings.NewReader(`{"root":0,"path":"source.bin"}`))
	coordinator.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("delete=%d %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "source.bin")); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
}

func TestCoordinatorTransfersDirectoryToPeer(t *testing.T) {
	sourceRoot, destinationRoot := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(sourceRoot, "folder", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sourceRoot, "folder", "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "folder", "nested", "source.txt"), []byte("eta"), 0o600); err != nil {
		t.Fatal(err)
	}
	coordinator, err := newServer([]string{sourceRoot})
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := newServer([]string{destinationRoot})
	if err != nil {
		t.Fatal(err)
	}
	receiver.transfers, err = transfer.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	peer := httptest.NewServer(receiver.routes())
	defer peer.Close()
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	if err := coordinator.peers.Add(peers.Peer{URL: peer.URL}); err != nil {
		t.Fatal(err)
	}
	payload := bytes.NewBufferString(`{"peer":"` + peer.URL + `","sourceRoot":0,"sourcePath":"folder","destinationRoot":0,"destinationPath":"copied"}`)
	response := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/transfers/send", payload))
	if response.Code != http.StatusAccepted {
		t.Fatalf("send=%d %s", response.Code, response.Body.String())
	}
	var job transfer.Job
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		current, found := coordinator.transferJobs.Get(job.ID)
		if !found {
			t.Fatal("job missing")
		}
		if current.Done {
			if current.Error != "" {
				t.Fatal(current.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transfer did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	body, err := os.ReadFile(filepath.Join(destinationRoot, "copied", "nested", "source.txt"))
	if err != nil || string(body) != "eta" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if info, err := os.Stat(filepath.Join(destinationRoot, "copied", "empty")); err != nil || !info.IsDir() {
		t.Fatalf("empty directory missing: %v", err)
	}
}

func TestCoordinatorAsksPeerToTransferDirectly(t *testing.T) {
	sourceRoot, destinationRoot := t.TempDir(), t.TempDir()
	source, err := newServer([]string{sourceRoot})
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := newServer([]string{destinationRoot})
	if err != nil {
		t.Fatal(err)
	}
	receiver.transfers, err = transfer.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	receiverHTTP := httptest.NewServer(receiver.routes())
	defer receiverHTTP.Close()
	sourceHTTP := httptest.NewServer(source.routes())
	defer sourceHTTP.Close()
	coordinator, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.advertiseURL = receiverHTTP.URL
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	if err := coordinator.peers.Add(peers.Peer{URL: sourceHTTP.URL}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "source.bin"), []byte("eta"), 0600); err != nil {
		t.Fatal(err)
	}
	payload := bytes.NewBufferString(`{"sourcePeer":"` + sourceHTTP.URL + `","sourceRoot":0,"sourcePath":"source.bin","destinationRoot":0,"destinationPath":"received.bin"}`)
	response := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/remote/transfers/send", payload))
	if response.Code != http.StatusAccepted {
		t.Fatalf("send=%d %s", response.Code, response.Body.String())
	}
	var job struct{ Peer, ID string }
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		current, found := source.transferJobs.Get(job.ID)
		if !found {
			t.Fatal("job missing")
		}
		if current.Done {
			if current.Error != "" {
				t.Fatal(current.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transfer did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	body, err := os.ReadFile(filepath.Join(destinationRoot, "received.bin"))
	if err != nil || string(body) != "eta" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}

func TestCoordinatorStartsDirectPeerTransfer(t *testing.T) {
	sourceRoot, destinationRoot := t.TempDir(), t.TempDir()
	coordinator, err := newServer([]string{sourceRoot})
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := newServer([]string{destinationRoot})
	if err != nil {
		t.Fatal(err)
	}
	receiver.transfers, err = transfer.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	peer := httptest.NewServer(receiver.routes())
	defer peer.Close()
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	if err := coordinator.peers.Add(peers.Peer{URL: peer.URL}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "source.bin"), []byte("eta"), 0600); err != nil {
		t.Fatal(err)
	}
	payload := bytes.NewBufferString(`{"peer":"` + peer.URL + `","sourceRoot":0,"sourcePath":"source.bin","destinationRoot":0,"destinationPath":"received.bin"}`)
	response := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/transfers/send", payload))
	if response.Code != http.StatusAccepted {
		t.Fatalf("send=%d %s", response.Code, response.Body.String())
	}
	var job transfer.Job
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		current, found := coordinator.transferJobs.Get(job.ID)
		if !found {
			t.Fatal("job missing")
		}
		if current.Done {
			if current.Error != "" {
				t.Fatal(current.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transfer did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err := os.ReadFile(filepath.Join(destinationRoot, "received.bin"))
	if err != nil || string(got) != "eta" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestDirectTransferBetweenEtaInstances(t *testing.T) {
	sourceRoot, destinationRoot := t.TempDir(), t.TempDir()
	sender, err := newServer([]string{sourceRoot})
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := newServer([]string{destinationRoot})
	if err != nil {
		t.Fatal(err)
	}
	receiver.transfers, err = transfer.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	peer := httptest.NewServer(receiver.routes())
	defer peer.Close()
	sourcePath := filepath.Join(sender.roots[0].Path, "source.bin")
	body := []byte("eta direct transfer")
	if err := os.WriteFile(sourcePath, body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := transfer.SendFile(context.Background(), peer.Client(), peer.URL, 0, "received.bin", sourcePath); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destinationRoot, "received.bin"))
	if err != nil || string(got) != string(body) {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestTransferAPIResumesVerifiedChunks(t *testing.T) {
	root := t.TempDir()
	s, err := newServer([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	s.transfers, err = transfer.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("eta transfer")
	manifest, err := transfer.BuildManifest(bytes.NewReader(body), 4)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"root": 0, "path": "received.bin", "manifest": manifest})
	create := httptest.NewRecorder()
	s.routes().ServeHTTP(create, httptest.NewRequest("POST", "/api/transfers", bytes.NewReader(payload)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", create.Code, create.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(create.Body).Decode(&created)
	for index, chunk := range [][]byte{[]byte("eta "), []byte("tran"), []byte("sfer")} {
		response := httptest.NewRecorder()
		s.routes().ServeHTTP(response, httptest.NewRequest("PUT", fmt.Sprintf("/api/transfers/%s/chunks/%d", created.ID, index), bytes.NewReader(chunk)))
		if response.Code != http.StatusNoContent {
			t.Fatalf("chunk %d = %d %s", index, response.Code, response.Body.String())
		}
	}
	finishBody := bytes.NewBufferString(`{"root":0,"path":"received.bin"}`)
	finish := httptest.NewRecorder()
	s.routes().ServeHTTP(finish, httptest.NewRequest("POST", "/api/transfers/"+created.ID+"/finalize", finishBody))
	if finish.Code != http.StatusNoContent {
		t.Fatalf("finalize=%d %s", finish.Code, finish.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(root, "received.bin"))
	if err != nil || string(got) != string(body) {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestRemoteFileRangeProxyCachesPeerRead(t *testing.T) {
	var fileReads int
	peerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/list":
			_, _ = w.Write([]byte(`{"entry":{"kind":"file","size":6,"modified":"2025-01-01T00:00:00Z"}}`))
		case "/api/file":
			fileReads++
			w.Header().Set("Content-Range", "bytes 2-4/6")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("cde"))
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer peerServer.Close()
	s, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	if err := s.peers.Add(peers.Peer{URL: peerServer.URL}); err != nil {
		t.Fatal(err)
	}
	s.remoteCache, err = diskcache.New(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		request := httptest.NewRequest("GET", "/api/remote/file?peer="+url.QueryEscape(peerServer.URL)+"&root=0&path=x", nil)
		request.Header.Set("Range", "bytes=2-4")
		response := httptest.NewRecorder()
		s.routes().ServeHTTP(response, request)
		if response.Code != http.StatusPartialContent || response.Body.String() != "cde" {
			t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
		}
	}
	if fileReads != 1 {
		t.Fatalf("peer file reads = %d", fileReads)
	}
}

func TestRemoteListProxyUsesEnrolledPeer(t *testing.T) {
	peerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/list" || r.URL.Query().Get("path") != "docs" {
			t.Fatalf("request = %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"entries":[]}`))
	}))
	defer peerServer.Close()
	s, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	if err := s.peers.Add(peers.Peer{URL: peerServer.URL}); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.routes().ServeHTTP(response, httptest.NewRequest("GET", "/api/remote/list?peer="+url.QueryEscape(peerServer.URL)+"&root=0&path=docs", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestPeerEnrollmentProbesIdentity(t *testing.T) {
	peerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/identity":
			_, _ = w.Write([]byte(`{"id":"peer-id","hostname":"peer","accent":"blue","glyph":"𓀀"}`))
		case "/api/auth/status":
			// Enrolling a peer also asks whether it needs a password (see
			// handlePeerAdd); an unprotected peer answers like this.
			_, _ = w.Write([]byte(`{"enabled":false}`))
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer peerServer.Close()
	s, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	req := httptest.NewRequest("POST", "/api/peers", strings.NewReader(`{"url":"`+peerServer.URL+`"}`))
	response := httptest.NewRecorder()
	s.routes().ServeHTTP(response, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d", response.Code)
	}
	items, err := s.peers.List()
	if err != nil || len(items) != 1 || items[0].Name != "peer" {
		t.Fatalf("peers = %#v err=%v", items, err)
	}
}

func TestStateEndpoint(t *testing.T) {
	s, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s.state = uistate.New(filepath.Join(t.TempDir(), "state.json"))
	put := httptest.NewRequest("PUT", "/api/state", strings.NewReader(`{"version":1,"windows":[{"kind":"explorer","root":0,"path":"docs"}]}`))
	put.Header.Set("Content-Type", "application/json")
	putResponse := httptest.NewRecorder()
	s.routes().ServeHTTP(putResponse, put)
	if putResponse.Code != 200 {
		t.Fatalf("state PUT status = %d, want 200", putResponse.Code)
	}
	getResponse := httptest.NewRecorder()
	s.routes().ServeHTTP(getResponse, httptest.NewRequest("GET", "/api/state", nil))
	if getResponse.Code != 200 {
		t.Fatalf("state GET status = %d, want 200", getResponse.Code)
	}
	var state uistate.State
	if err := json.NewDecoder(getResponse.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if len(state.Windows) != 1 || state.Windows[0].Path != "docs" {
		t.Fatalf("state = %#v", state)
	}
}

// Control-plane JSON describes state that changes underneath the client:
// desktop windows, directory listings, peer inventory, transfer jobs. None
// of it carries a validator, so without an explicit directive a client or
// intermediary may apply heuristic freshness and serve a body that no
// longer matches the server.
func TestJSONEndpointsAreNotCacheable(t *testing.T) {
	root := t.TempDir()
	s, err := newServer([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	s.state = uistate.New(filepath.Join(t.TempDir(), "state.json"))
	for _, path := range []string{
		"/api/state",
		"/api/roots",
		"/api/identity",
		"/api/list?root=0&path=",
		// /api/peers is omitted: a bare test server has no peer store
		// configured and answers 500. It writes through the same helper.
	} {
		response := httptest.NewRecorder()
		s.routes().ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 200 {
			t.Fatalf("%s status = %d, want 200", path, response.Code)
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want %q", path, got, "no-store")
		}
	}
}

func TestIdentityEndpoint(t *testing.T) {
	s, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/identity", nil)
	response := httptest.NewRecorder()
	s.routes().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("identity status = %d, want 200", response.Code)
	}
	var identity struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
		Accent   string `json:"accent"`
		Glyph    string `json:"glyph"`
	}
	if err := json.NewDecoder(response.Body).Decode(&identity); err != nil {
		t.Fatal(err)
	}
	if identity.ID == "" || identity.Hostname == "" || identity.Accent == "" || identity.Glyph == "" {
		t.Fatalf("incomplete identity response: %#v", identity)
	}
}

func TestTargetStaysWithinConfiguredRoot(t *testing.T) {
	rootPath := t.TempDir()
	outsidePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "inside.txt"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsidePath, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outsidePath, "secret.txt"), filepath.Join(rootPath, "escape")); err != nil {
		t.Fatal(err)
	}

	s, err := newServer([]string{rootPath})
	if err != nil {
		t.Fatal(err)
	}
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/list?root=0&path="+path, nil)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		return w
	}

	if got := request("inside.txt").Code; got != 200 {
		t.Fatalf("inside file status = %d, want 200", got)
	}
	if got := request("../secret.txt").Code; got != 400 {
		t.Fatalf("parent traversal status = %d, want 400", got)
	}
	if got := request("escape").Code; got != 400 {
		t.Fatalf("symlink escape status = %d, want 400", got)
	}
}

// Pins the sweep wiring in main.go. Without this test, a future
// refactor that drops the startup-time sweep leaves abandoned
// .eta/staging dirs accumulating on the receiver until the disk
// fills.
//
// Simulates an orphan by writing a staging directory under
// .eta/staging/ without a matching intent record — the same state
// a crashed mid-transfer sender leaves behind. The sweep should
// remove it on its startup pass.
func TestSweepStaleTreeSessionsRemovesOrphans(t *testing.T) {
	root := t.TempDir()
	server, err := newServer([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(server.treeStores) != 1 {
		t.Fatalf("treeStores = %d, want 1", len(server.treeStores))
	}

	orphanID := "orphan-" + time.Now().Format("150405.000000")
	orphanPath := filepath.Join(root, ".eta", "staging", orphanID)
	if err := os.MkdirAll(orphanPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphanPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.sweepStaleTreeSessions(ctx)

	// Wait up to 2 seconds for the startup sweep to remove the
	// orphan. The startup sweep runs synchronously inside the
	// goroutine, so the only delay is scheduling.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(orphanPath); os.IsNotExist(err) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("orphan staging %q not swept within 2s", orphanPath)
}

// A peer's terminal session lives on the peer, so the browser must be
// able to stream its output through the coordinator. Without a remote
// stream route the client asked the local instance for an id it had
// never created, and a remote terminal accepted keystrokes while
// showing nothing.
func TestRemoteTerminalStreamsPeerOutput(t *testing.T) {
	coordinator, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	remoteRoot := t.TempDir()
	receiver, err := newServer([]string{remoteRoot})
	if err != nil {
		t.Fatal(err)
	}
	receiver.terminals = terminal.NewManager()
	peer := httptest.NewServer(receiver.routes())
	defer peer.Close()
	coordinator.peers = peers.New(filepath.Join(t.TempDir(), "peers.json"))
	if err := coordinator.peers.Add(peers.Peer{URL: peer.URL}); err != nil {
		t.Fatal(err)
	}

	start := httptest.NewRecorder()
	coordinator.routes().ServeHTTP(start, httptest.NewRequest(
		"POST", "/api/remote/terminals?peer="+url.QueryEscape(peer.URL),
		bytes.NewBufferString(`{"root":0,"path":"","columns":80,"rows":24}`)))
	if start.Code != http.StatusCreated {
		t.Fatalf("start=%d %s", start.Code, start.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(start.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("remote terminal returned no id")
	}

	session, found := receiver.terminals.Get(created.ID)
	if !found {
		t.Fatal("session was not created on the peer")
	}
	if err := session.Input([]byte("echo eta-remote-marker\n")); err != nil {
		t.Fatal(err)
	}

	// The stream is long-lived, so read it with a deadline and stop at
	// the marker rather than waiting for the body to end.
	streamed := make(chan string, 1)
	go func() {
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET",
			"/api/remote/terminals/"+created.ID+"/stream?offset=0&peer="+url.QueryEscape(peer.URL), nil)
		ctx, cancel := context.WithTimeout(request.Context(), 4*time.Second)
		defer cancel()
		coordinator.routes().ServeHTTP(response, request.WithContext(ctx))
		streamed <- response.Body.String()
	}()

	select {
	case body := <-streamed:
		if !strings.Contains(body, "eta-remote-marker") {
			t.Fatalf("peer output never reached the coordinator: %q", body)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("remote terminal stream produced nothing")
	}
}

// Dotfiles are hidden on every platform; the Windows attribute path is
// covered by the build-tagged isHidden and cannot be exercised here.
func TestListingMarksDotfilesHidden(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".hidden-file", "visible.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, ".hidden-dir"), 0700); err != nil {
		t.Fatal(err)
	}
	server, err := newServer([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, httptest.NewRequest("GET", "/api/list?root=0&path=", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list=%d %s", response.Code, response.Body.String())
	}
	var listing struct {
		Entries []entry `json:"entries"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 3 {
		t.Fatalf("entries = %d, want 3: %+v", len(listing.Entries), listing.Entries)
	}
	for _, item := range listing.Entries {
		want := strings.HasPrefix(item.Name, ".")
		if item.Hidden != want {
			t.Errorf("%q hidden = %v, want %v", item.Name, item.Hidden, want)
		}
	}
}

// A machine without tmux must answer "not available" rather than an
// error or an empty session list, so the UI can tell "nothing running"
// apart from "cannot ask".
func TestTmuxListReportsAvailability(t *testing.T) {
	server, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, httptest.NewRequest("GET", "/api/tmux", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("tmux list=%d %s", response.Code, response.Body.String())
	}
	var body struct {
		Available bool           `json:"available"`
		Sessions  []tmux.Session `json:"sessions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Sessions == nil {
		t.Error("sessions must be an empty list, never null")
	}
	if _, err := exec.LookPath("tmux"); err != nil && body.Available {
		t.Error("reported tmux as available on a machine without it")
	}
}

// Session names reach a command line; the endpoint must refuse the
// unsafe ones rather than relying on the caller.
func TestTmuxCreateRejectsUnsafeName(t *testing.T) {
	server, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/tmux",
		bytes.NewBufferString(`{"name":"evil; rm -rf /"}`)))
	if response.Code == http.StatusCreated {
		t.Fatalf("unsafe session name accepted: %s", response.Body.String())
	}
}

// A file's own directory, not a shell that has to be told where to go —
// this is Eta's whole "editor": open a terminal that already dropped you
// into vim on the file. Skips rather than fails on a machine without
// vim, the same way the tmux tests handle tmux's absence.
func TestTerminalStartWithEditOpensVimOnTheFile(t *testing.T) {
	if _, err := exec.LookPath("vim"); err != nil {
		t.Skip("vim not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := newServer([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	start := httptest.NewRequest(http.MethodPost, "/api/terminals",
		strings.NewReader(`{"root":0,"path":"notes.txt","columns":80,"rows":24,"edit":true}`))
	startW := httptest.NewRecorder()
	server.routes().ServeHTTP(startW, start)
	if startW.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", startW.Code, startW.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(startW.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// vim's initial screen names the file it opened, somewhere in its
	// status/ruler area, so its presence in the raw PTY output is a
	// reasonable signal that vim actually started on this file rather
	// than a shell landing in its directory.
	deadline := time.Now().Add(3 * time.Second)
	var output string
	for time.Now().Before(deadline) {
		outW := httptest.NewRecorder()
		server.routes().ServeHTTP(outW, httptest.NewRequest(http.MethodGet, "/api/terminals/"+created.ID, nil))
		var body struct {
			Output string `json:"output"`
		}
		if err := json.NewDecoder(outW.Body).Decode(&body); err == nil {
			output = body.Output
			if strings.Contains(output, "notes.txt") {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(output, "notes.txt") {
		t.Fatalf("vim's screen never mentioned the file name; got %q", output)
	}
}

func TestVersionAndChangelogEndpoints(t *testing.T) {
	server, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	versionW := httptest.NewRecorder()
	server.routes().ServeHTTP(versionW, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if versionW.Code != http.StatusOK {
		t.Fatalf("version: %d %s", versionW.Code, versionW.Body.String())
	}
	var version struct {
		Version     string `json:"version"`
		Commit      string `json:"commit"`
		Date        string `json:"date"`
		BuildSource string `json:"build_source"`
	}
	if err := json.NewDecoder(versionW.Body).Decode(&version); err != nil {
		t.Fatal(err)
	}
	// An untagged local build (what every dev loop in this repo uses)
	// must read as unset, not as a plausible-looking fake version.
	if version.Version != "dev" || version.Commit != "none" || version.BuildSource != "source" {
		t.Fatalf("unexpected default version info: %#v", version)
	}

	changelogW := httptest.NewRecorder()
	server.routes().ServeHTTP(changelogW, httptest.NewRequest(http.MethodGet, "/api/changelog", nil))
	if changelogW.Code != http.StatusOK {
		t.Fatalf("changelog: %d", changelogW.Code)
	}
	if !strings.Contains(changelogW.Body.String(), "# Changelog") {
		t.Fatalf("changelog body did not look like CHANGELOG.md: %q", changelogW.Body.String()[:min(80, changelogW.Body.Len())])
	}

	// Both stay reachable even with a password set: version and
	// changelog carry no secrets, and are useful on a login screen.
	setPassword(t, server, "some-password")
	for _, path := range []string{"/api/version", "/api/changelog"} {
		w := httptest.NewRecorder()
		server.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s with a password set: got %d, want public", path, w.Code)
		}
	}
}

// enableRootManagement wires a test server's rootsStore, mirroring what
// main() does at startup — newServer alone never sets this up, so a
// test server's roots are startup-only unless this is called.
func enableRootManagement(t *testing.T, s *server) {
	t.Helper()
	s.rootsPath = filepath.Join(t.TempDir(), "roots.json")
	s.rootsStore = roots.New(s.rootsPath)
	seed := make([]roots.Root, len(s.roots))
	for i, r := range s.roots {
		seed[i] = roots.Root{Name: r.Name, Path: r.Path}
	}
	if err := s.rootsStore.SaveAll(seed); err != nil {
		t.Fatal(err)
	}
	s.applyPersistedRoots(seed)
}

func TestRootAddAndListRoundTrip(t *testing.T) {
	server, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	enableRootManagement(t, server)
	newDir := t.TempDir()

	addW := httptest.NewRecorder()
	server.routes().ServeHTTP(addW, httptest.NewRequest(http.MethodPost, "/api/roots", strings.NewReader(`{"path":"`+newDir+`"}`)))
	if addW.Code != http.StatusCreated {
		t.Fatalf("add: %d %s", addW.Code, addW.Body.String())
	}
	var added publicRoot
	if err := json.NewDecoder(addW.Body).Decode(&added); err != nil {
		t.Fatal(err)
	}
	if added.ID != 1 {
		t.Fatalf("expected the new root at id 1, got %d", added.ID)
	}

	listW := httptest.NewRecorder()
	server.routes().ServeHTTP(listW, httptest.NewRequest(http.MethodGet, "/api/roots", nil))
	var list []publicRoot
	if err := json.NewDecoder(listW.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 roots, got %#v", list)
	}
}

func TestRootAddRejectsANonDirectoryOrDuplicate(t *testing.T) {
	dir := t.TempDir()
	server, err := newServer([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	enableRootManagement(t, server)

	notADir := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	server.routes().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/roots", strings.NewReader(`{"path":"`+notADir+`"}`)))
	if w.Code == http.StatusCreated {
		t.Fatal("a file was accepted as a root")
	}

	w = httptest.NewRecorder()
	server.routes().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/roots", strings.NewReader(`{"path":"`+dir+`"}`)))
	if w.Code == http.StatusCreated {
		t.Fatal("an already-added root was accepted a second time")
	}
}

// The whole point of this feature: removing an earlier root must not
// let a later one, or anything that already named it by index, resolve
// to a different directory than the one it always meant.
func TestRemovedRootIndexIsNeverReassignedToADifferentDirectory(t *testing.T) {
	dirA, dirB, dirC := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dirB, "b-marker.txt"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := newServer([]string{dirA, dirB, dirC})
	if err != nil {
		t.Fatal(err)
	}
	enableRootManagement(t, server)

	// Remove root 0 (dirA).
	removeW := httptest.NewRecorder()
	server.routes().ServeHTTP(removeW, httptest.NewRequest(http.MethodDelete, "/api/roots?id=0", nil))
	if removeW.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", removeW.Code, removeW.Body.String())
	}

	// Root 1 (dirB) must still be dirB — listing it must still find the
	// marker file that only exists in dirB, not whatever a naive
	// splice-based removal would have shifted into slot 1.
	listReq := httptest.NewRequest(http.MethodGet, "/api/list?root=1&path=", nil)
	listW := httptest.NewRecorder()
	server.routes().ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list root 1: %d %s", listW.Code, listW.Body.String())
	}
	if !strings.Contains(listW.Body.String(), "b-marker.txt") {
		t.Fatalf("root 1 no longer resolves to dirB: %s", listW.Body.String())
	}

	// A request still naming the removed root 0 must be refused, not
	// silently served from nothing (root 0's slot has no replacement —
	// this asserts that explicitly, in case a future change ever gives
	// it one).
	staleReq := httptest.NewRequest(http.MethodGet, "/api/list?root=0&path=", nil)
	staleW := httptest.NewRecorder()
	server.routes().ServeHTTP(staleW, staleReq)
	if staleW.Code == http.StatusOK {
		t.Fatalf("a removed root's index still served a request: %s", staleW.Body.String())
	}

	// The public listing must also have dropped it.
	rootsW := httptest.NewRecorder()
	server.routes().ServeHTTP(rootsW, httptest.NewRequest(http.MethodGet, "/api/roots", nil))
	var publicList []publicRoot
	if err := json.NewDecoder(rootsW.Body).Decode(&publicList); err != nil {
		t.Fatal(err)
	}
	for _, r := range publicList {
		if r.ID == 0 {
			t.Fatalf("removed root 0 still appears in the public list: %#v", publicList)
		}
	}
	if len(publicList) != 2 {
		t.Fatalf("expected 2 active roots (1 and 2), got %#v", publicList)
	}
}

func TestRootRemoveRejectsUnknownID(t *testing.T) {
	server, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	enableRootManagement(t, server)
	w := httptest.NewRecorder()
	server.routes().ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/roots?id=99", nil))
	if w.Code == http.StatusOK {
		t.Fatal("an out-of-range root id was accepted for removal")
	}
}

func TestRootManagementUnavailableWithoutAStore(t *testing.T) {
	// A plain newServer() (every other test in this file) never wires
	// rootsStore -- these endpoints must fail clearly, not panic on a
	// nil store.
	server, err := newServer([]string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	addW := httptest.NewRecorder()
	server.routes().ServeHTTP(addW, httptest.NewRequest(http.MethodPost, "/api/roots", strings.NewReader(`{"path":"`+t.TempDir()+`"}`)))
	if addW.Code == http.StatusCreated {
		t.Fatal("root add succeeded without a rootsStore")
	}
	removeW := httptest.NewRecorder()
	server.routes().ServeHTTP(removeW, httptest.NewRequest(http.MethodDelete, "/api/roots?id=0", nil))
	if removeW.Code == http.StatusOK {
		t.Fatal("root remove succeeded without a rootsStore")
	}
}

// The error a user actually saw:
//
//	probe peer identity: Get "http://jupiter.local:7080/api/identity":
//	dial tcp: lookup jupiter.local on 127.0.0.53:53: server misbehaving
//
// Every fact in that sentence is either internal (the probe endpoint),
// not theirs (the stub resolver's address), or wrong as a diagnosis (the
// server is not misbehaving). .local is reserved for mDNS by RFC 6762,
// so an ordinary DNS server is expected to refuse it -- which is exactly
// what happened, and is fixable once said.
func TestExplainPeerProbeDiagnosesAnMDNSName(t *testing.T) {
	// The real wrapped chain: http.Client wraps *url.Error around the
	// dialer's *net.OpError around the resolver's *net.DNSError.
	err := &url.Error{
		Op:  "Get",
		URL: "http://jupiter.local:7080/api/identity",
		Err: &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &net.DNSError{
				Err:    "server misbehaving",
				Name:   "jupiter.local",
				Server: "127.0.0.53:53",
			},
		},
	}
	got := explainPeerProbe("http://jupiter.local:7080", err).Error()

	if !strings.Contains(got, "mDNS") {
		t.Errorf("the diagnosis should name mDNS, since that is the actual cause; got: %s", got)
	}
	if !strings.Contains(got, "IP address") {
		t.Errorf("should say what to try instead; got: %s", got)
	}
	if !strings.Contains(got, "jupiter.local") {
		t.Errorf("should name the host the user typed; got: %s", got)
	}
	// None of Go's plumbing should survive into what the user reads.
	for _, leak := range []string{"127.0.0.53", "server misbehaving", "/api/identity", "dial tcp", "probe peer identity"} {
		if strings.Contains(got, leak) {
			t.Errorf("internal detail %q leaked into the message: %s", leak, got)
		}
	}
}

// A name that genuinely does not exist is a different problem from a
// .local one, and gets a different instruction.
func TestExplainPeerProbeReportsAnUnknownName(t *testing.T) {
	err := &url.Error{Op: "Get", Err: &net.OpError{Op: "dial", Err: &net.DNSError{
		Err: "no such host", Name: "nosuchpc", IsNotFound: true,
	}}}
	got := explainPeerProbe("http://nosuchpc:7080", err).Error()
	if !strings.Contains(got, "nosuchpc") || !strings.Contains(got, "spelling") {
		t.Errorf("expected a name-not-found message naming the host; got: %s", got)
	}
	if strings.Contains(got, "mDNS") {
		t.Errorf("mDNS is irrelevant to an ordinary hostname; got: %s", got)
	}
}

// A real refusal from a real closed port, rather than a hand-built error,
// so this stays true to whatever the platform actually returns.
func TestExplainPeerProbeReportsARefusedConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedURL := "http://" + listener.Addr().String()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()

	_, probeErr := probePeer(context.Background(), closedURL)
	if probeErr == nil {
		t.Fatal("expected the probe to fail against a closed port")
	}
	got := explainPeerProbe(closedURL, probeErr).Error()
	if !strings.Contains(got, "Eta doesn't appear to be running") {
		t.Errorf("a refusal means nothing is listening, and should say so; got: %s", got)
	}
	if !strings.Contains(got, port) {
		t.Errorf("should name the port that refused; got: %s", got)
	}
	if strings.Contains(got, "connect: connection refused") {
		t.Errorf("raw syscall wording leaked: %s", got)
	}
}

// Reachable but not Eta is worth separating from unreachable: the address
// is fine and the fix is a different port, not a different machine.
func TestExplainPeerProbeSeparatesReachableButNotEta(t *testing.T) {
	notEta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notEta.Close()

	_, probeErr := probePeer(context.Background(), notEta.URL)
	if probeErr == nil {
		t.Fatal("expected a probe against a non-Eta server to fail")
	}
	got := explainPeerProbe(notEta.URL, probeErr).Error()
	if !strings.Contains(got, "isn't Eta") || !strings.Contains(got, "Check the port") {
		t.Errorf("expected a reachable-but-wrong-service message; got: %s", got)
	}

	// Eta's own endpoint answering with something else is the same class
	// of problem, and must not read as a network failure.
	wrongShape := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"hostname":"x"}`)
	}))
	defer wrongShape.Close()
	_, probeErr = probePeer(context.Background(), wrongShape.URL)
	if probeErr == nil {
		t.Fatal("expected an incomplete identity to fail")
	}
	if got := explainPeerProbe(wrongShape.URL, probeErr).Error(); !strings.Contains(got, "isn't Eta") {
		t.Errorf("an incomplete identity is still 'not Eta'; got: %s", got)
	}
}

// Unreachable addresses must not read as a server fault of ours: the
// address the user typed is the thing at issue.
func TestExplainPeerProbeIsABadRequestNotAServerError(t *testing.T) {
	err := explainPeerProbe("http://jupiter.local:7080", &net.DNSError{Err: "server misbehaving", Name: "jupiter.local"})
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an apiError carrying a status, got %T", err)
	}
	if apiErr.status != http.StatusBadRequest {
		t.Errorf("expected 400 for an address that doesn't work, got %d", apiErr.status)
	}
}

// The reported failure, end to end: a user types a .local hostname and
// adding the PC fails, because Go's resolver will not resolve a name
// that RFC 6762 defines as multicast-only.
//
// Here a real responder advertises the name (as the peer's own Avahi or
// Bonjour would) pointing at a real Eta identity endpoint, and the probe
// must succeed through the ordinary peer transport.
func TestProbePeerResolvesADotLocalHostname(t *testing.T) {
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/identity" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id": "abc", "hostname": "JUPITER", "accent": "#8b5cf6", "glyph": "J",
		})
	}))
	defer identity.Close()

	port := identity.URL[strings.LastIndex(identity.URL, ":")+1:]
	stop := startTestMDNSResponder(t, "jupiter.local.", "127.0.0.1")
	defer stop()
	mdns.Forget("jupiter.local")

	got, err := probePeer(context.Background(), "http://jupiter.local:"+port)
	if err != nil {
		t.Fatalf("a .local peer must be reachable by name: %v", err)
	}
	if got.Hostname != "JUPITER" {
		t.Errorf("expected the peer's identity, got %+v", got)
	}
}

// startTestMDNSResponder answers A queries for one .local name, standing
// in for the Avahi or Bonjour responder on a real peer.
func startTestMDNSResponder(t *testing.T, fqdn, ip string) func() {
	t.Helper()
	conn, err := net.ListenMulticastUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("224.0.0.251"), Port: 5353})
	if err != nil {
		t.Skipf("cannot listen on the mDNS group here: %v", err)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 9000)
		for {
			select {
			case <-done:
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				continue
			}
			msg := new(dnsmsg.Msg)
			if msg.Unpack(buf[:n]) != nil || len(msg.Question) == 0 {
				continue
			}
			if !strings.EqualFold(msg.Question[0].Name, fqdn) || msg.Question[0].Qtype != dnsmsg.TypeA {
				continue
			}
			reply := new(dnsmsg.Msg)
			reply.SetReply(msg)
			reply.Answer = []dnsmsg.RR{&dnsmsg.A{
				Hdr: dnsmsg.RR_Header{Name: fqdn, Rrtype: dnsmsg.TypeA, Class: dnsmsg.ClassINET, Ttl: 120},
				A:   net.ParseIP(ip),
			}}
			if wire, err := reply.Pack(); err == nil {
				_, _ = conn.WriteToUDP(wire, src)
			}
		}
	}()
	return func() { close(done); conn.Close() }
}
