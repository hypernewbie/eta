package transfer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreFinalizesEmptyFile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(bytes.NewReader(nil), 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Open("empty", manifest); err != nil {
		t.Fatal(err)
	}
	if missing, err := store.Missing("empty"); err != nil || len(missing) != 0 {
		t.Fatalf("missing=%v err=%v", missing, err)
	}
	out := filepath.Join(t.TempDir(), "empty")
	if err := store.Finalize("empty", out); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil || info.Size() != 0 {
		t.Fatalf("info=%v err=%v", info, err)
	}
}

func TestStoreResumesAndAtomicallyFinalizes(t *testing.T) {
	m, e := BuildManifest(bytes.NewBufferString("eta transfer"), 4)
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Open("x", m); e != nil {
		t.Fatal(e)
	}
	if e = s.Write("x", 1, []byte("tran")); e != nil {
		t.Fatal(e)
	}
	missing, e := s.Missing("x")
	if e != nil || len(missing) != 2 {
		t.Fatal(missing, e)
	}
	if e = s.Finalize("x", filepath.Join(t.TempDir(), "out")); e == nil {
		t.Fatal("finalized incomplete")
	}
	for i, b := range [][]byte{[]byte("eta "), []byte("tran"), []byte("sfer")} {
		if e = s.Write("x", i, b); e != nil {
			t.Fatal(e)
		}
	}
	out := filepath.Join(t.TempDir(), "out")
	if e = s.Finalize("x", out); e != nil {
		t.Fatal(e)
	}
	body, e := os.ReadFile(out)
	if e != nil || string(body) != "eta transfer" {
		t.Fatalf("%q %v", body, e)
	}
}
