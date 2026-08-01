package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hypernewbie/eta/internal/diskcache"
	"github.com/hypernewbie/eta/internal/peers"
	"github.com/hypernewbie/eta/internal/terminal"
	"github.com/hypernewbie/eta/internal/transfer"
	"github.com/hypernewbie/eta/internal/uistate"
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
		if r.URL.Path != "/api/identity" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"peer-id","hostname":"peer","accent":"blue","glyph":"𓀀"}`))
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
