package peers

import (
	"path/filepath"
	"testing"
)

func TestExplicitPeerInventory(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "peers.json"))
	if e := s.Add(Peer{URL: "http://eta-b:7080", Name: "B"}); e != nil {
		t.Fatal(e)
	}
	if e := s.Add(Peer{URL: "http://eta-b:7080"}); e == nil {
		t.Fatal("duplicate accepted")
	}
	p, e := s.List()
	if e != nil || len(p) != 1 || p[0].Name != "B" {
		t.Fatal(e)
	}
	if e := s.Remove("http://eta-b:7080"); e != nil {
		t.Fatal(e)
	}
	p, e = s.List()
	if e != nil || len(p) != 0 {
		t.Fatal(e)
	}
}
