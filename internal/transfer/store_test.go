package transfer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

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
