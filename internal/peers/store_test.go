package peers

import (
	"os"
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

func TestPeerVerifierPersistedAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	s1 := New(path)
	if err := s1.UpsertBySSHDestination(Peer{SSHDestination: "hammond", URL: "http://127.0.0.1:40001", Verifier: "secret-hash-123"}); err != nil {
		t.Fatal(err)
	}

	// Re-instantiate store (simulating server restart)
	s2 := New(path)
	peer, found, err := s2.FindBySSHDestination("hammond")
	if err != nil || !found {
		t.Fatalf("failed to find peer after restart: %v", err)
	}
	if peer.Verifier != "secret-hash-123" {
		t.Fatalf("expected verifier 'secret-hash-123', got %q", peer.Verifier)
	}

	// Reconnect on new port, without providing verifier (must preserve existing)
	if err := s2.UpsertBySSHDestination(Peer{SSHDestination: "hammond", URL: "http://127.0.0.1:40002"}); err != nil {
		t.Fatal(err)
	}
	peer2, found2, _ := s2.FindBySSHDestination("hammond")
	if !found2 || peer2.Verifier != "secret-hash-123" {
		t.Fatalf("verifier wiped on reconnect: %+v", peer2)
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

// TestUpsertBySSHDestinationRewritesTheURLInPlace is the reason
// SSHDestination exists: an SSH-backed peer's URL is a forwarded
// loopback port that differs every session, so keying on URL would append
// a new entry per reconnect and leave the old one pointing at a closed
// port -- or at a port since taken by a different tunnel.
func TestUpsertBySSHDestinationRewritesTheURLInPlace(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "peers.json"))
	if err := s.UpsertBySSHDestination(Peer{
		SSHDestination: "pi@minerva",
		URL:            "http://127.0.0.1:41001",
		Name:           "MINERVA",
	}); err != nil {
		t.Fatal(err)
	}
	// A reconnect: same PC, new tunnel port, and the caller does not
	// resupply identity.
	if err := s.UpsertBySSHDestination(Peer{
		SSHDestination: "pi@minerva",
		URL:            "http://127.0.0.1:52002",
	}); err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one entry after a reconnect, got %d: %+v", len(list), list)
	}
	if list[0].URL != "http://127.0.0.1:52002" {
		t.Errorf("URL was not rewritten to the new tunnel port: %q", list[0].URL)
	}
	if list[0].Name != "MINERVA" {
		t.Errorf("identity the reconnect did not resupply was lost: %+v", list[0])
	}
}

func TestUpsertBySSHDestinationKeepsDistinctPCsSeparate(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "peers.json"))
	for _, dest := range []string{"minerva", "pi@nas", "workshop"} {
		if err := s.UpsertBySSHDestination(Peer{SSHDestination: dest, URL: "http://127.0.0.1:41001"}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected three distinct PCs, got %d: %+v", len(list), list)
	}
}

// An SSH-backed peer must not collide with an ordinary one that happens to
// share a URL, since the tunnel port is arbitrary and could match anything.
func TestUpsertBySSHDestinationLeavesOrdinaryPeersAlone(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "peers.json"))
	if err := s.Add(Peer{URL: "http://127.0.0.1:41001", Name: "ORDINARY"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertBySSHDestination(Peer{
		SSHDestination: "minerva",
		URL:            "http://127.0.0.1:41001",
		Name:           "SSH-BACKED",
	}); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected the ordinary peer to survive alongside the SSH-backed one, got %d: %+v", len(list), list)
	}
}

func TestFindBySSHDestination(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "peers.json"))
	if err := s.UpsertBySSHDestination(Peer{SSHDestination: "minerva", URL: "http://127.0.0.1:41001"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.FindBySSHDestination("minerva"); err != nil || !ok {
		t.Fatalf("expected to find the peer, ok=%v err=%v", ok, err)
	}
	if _, ok, _ := s.FindBySSHDestination("someone-else"); ok {
		t.Error("found a destination that was never recorded")
	}
	// An ordinary peer has no destination, so an empty lookup must not
	// match it.
	if err := s.Add(Peer{URL: "http://eta-b:7080"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.FindBySSHDestination(""); ok {
		t.Error("an empty destination matched a peer that has none")
	}
}

func TestUpsertBySSHDestinationRefusesAnEmptyDestination(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "peers.json"))
	if err := s.UpsertBySSHDestination(Peer{URL: "http://127.0.0.1:41001"}); err == nil {
		t.Fatal("expected an error: without a destination there is no identity to key on")
	}
}

// Older inventories have no ssh_destination field at all, and must load
// unchanged rather than being treated as SSH-backed.
func TestPeersWithoutAnSSHDestinationLoadAsOrdinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	if err := os.WriteFile(path, []byte(`[{"url":"http://eta-b:7080","name":"B"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := New(path).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].SSHDestination != "" {
		t.Fatalf("expected one ordinary peer, got %+v", list)
	}
}
