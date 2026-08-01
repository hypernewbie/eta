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

func TestUpdateReplacesIdentityInPlace(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "peers.json"))
	if err := store.Add(Peer{URL: "http://pc-b:7080", Name: "OLD", Accent: "purple", Glyph: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(Peer{URL: "http://pc-b:7080", Name: "NEW", Accent: "teal", Glyph: "B"}); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("update duplicated the peer: %+v", list)
	}
	if list[0].Accent != "teal" || list[0].Name != "NEW" || list[0].Glyph != "B" {
		t.Fatalf("identity not replaced: %+v", list[0])
	}
	if err := store.Update(Peer{URL: "http://absent:7080"}); err == nil {
		t.Error("updating an unknown peer should fail")
	}
}
