package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

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
