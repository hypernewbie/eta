package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hypernewbie/eta/internal/diskcache"
	"github.com/hypernewbie/eta/internal/peers"
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
