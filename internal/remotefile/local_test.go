package remotefile

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalSourceRangesAndContains(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "a.txt"), []byte("abcdef"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(out, filepath.Join(root, "escape")); e != nil {
		t.Fatal(e)
	}
	s, e := NewLocalSource(root)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.OpenRange(context.Background(), "a.txt", 2, 3)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(r)
	r.Close()
	if e != nil || string(b) != "cde" {
		t.Fatalf("range=%q err=%v", b, e)
	}
	if _, e := s.Stat(context.Background(), "escape"); e == nil {
		t.Fatal("escape accepted")
	}
}
