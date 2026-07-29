package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
