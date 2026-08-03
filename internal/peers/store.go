// Package peers owns an explicit coordinator's peer inventory.
package peers

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Peer struct {
	URL    string `json:"url"`
	Name   string `json:"name,omitempty"`
	ID     string `json:"id,omitempty"`
	Accent string `json:"accent,omitempty"`
	Glyph  string `json:"glyph,omitempty"`
	// Verifier is this peer's own PBKDF2 access-password verifier,
	// captured once when it was enrolled with a password (see
	// handlePeerAdd), never the plaintext. It authenticates this server
	// to that peer on every proxied request going forward — a peer with
	// a password is just another client who knows it, and this is how
	// this server proves it does too, without asking again each time.
	Verifier string `json:"verifier,omitempty"`
	// SSHDestination, when set, means this PC is reached by running eta on
	// it over SSH rather than by talking to one already listening. It is
	// what the user typed and what ssh resolves, and it is this peer's
	// durable identity: URL holds an ephemeral forwarded loopback port
	// that changes on every reconnect, so it cannot be the key. Empty for
	// an ordinary peer, and absent from older inventories.
	SSHDestination string `json:"ssh_destination,omitempty"`
}
type Store struct {
	path string
	mu   sync.Mutex
}

func DefaultPath() (string, error) {
	d, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(d, "eta", "peers.json"), nil
}
func New(path string) *Store { return &Store{path: path} }
func (s *Store) List() ([]Peer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, e := os.ReadFile(s.path)
	if os.IsNotExist(e) {
		return []Peer{}, nil
	}
	if e != nil {
		return nil, e
	}
	var p []Peer
	if e = json.Unmarshal(b, &p); e != nil {
		return nil, e
	}
	sort.Slice(p, func(i, j int) bool { return p[i].URL < p[j].URL })
	return p, nil
}
func (s *Store) Add(peer Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer.URL = strings.TrimSuffix(peer.URL, "/")
	u, e := url.Parse(peer.URL)
	if e != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("invalid peer URL")
	}
	p, e := s.list()
	if e != nil {
		return e
	}
	for _, x := range p {
		if x.URL == peer.URL {
			return fmt.Errorf("peer already exists")
		}
	}
	p = append(p, peer)
	return s.save(p)
}

// Update replaces an existing peer's details, keyed by URL. Identity is
// not immutable: a PC can be renamed or have its colour changed, and the
// inventory holds a copy taken at enrolment that would otherwise stay
// wrong forever.
func (s *Store) Update(peer Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer.URL = strings.TrimSuffix(peer.URL, "/")
	list, err := s.list()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].URL == peer.URL || (peer.SSHDestination != "" && list[i].SSHDestination == peer.SSHDestination) {
			if peer.Verifier == "" {
				peer.Verifier = list[i].Verifier
			}
			list[i] = peer
			return s.save(list)
		}
	}
	return fmt.Errorf("unknown peer")
}

// FindBySSHDestination looks a peer up by the identity that survives
// reconnects, rather than by its current URL.
func (s *Store) FindBySSHDestination(destination string) (Peer, bool, error) {
	if destination == "" {
		return Peer{}, false, nil
	}
	items, err := s.List()
	if err != nil {
		return Peer{}, false, err
	}
	for _, peer := range items {
		if peer.SSHDestination == destination {
			return peer, true, nil
		}
	}
	return Peer{}, false, nil
}

// UpsertBySSHDestination records an SSH-backed peer, matching on its
// destination so a reconnect updates the existing entry's URL in place
// instead of adding a second one. Keying on URL cannot do this: the
// forwarded port differs every session, so each reconnect would append a
// new entry and strand the old one pointing at a closed port — or worse,
// at a port now serving a different tunnel.
func (s *Store) UpsertBySSHDestination(peer Peer) error {
	if peer.SSHDestination == "" {
		return fmt.Errorf("peer has no SSH destination")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	peer.URL = strings.TrimSuffix(peer.URL, "/")
	list, err := s.list()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].SSHDestination == peer.SSHDestination {
			// Preserve anything the caller didn't supply: a reconnect
			// knows the new URL but not necessarily the identity fields
			// filled in by a later identity probe.
			if peer.Name == "" {
				peer.Name = list[i].Name
			}
			if peer.ID == "" {
				peer.ID = list[i].ID
			}
			if peer.Accent == "" {
				peer.Accent = list[i].Accent
			}
			if peer.Glyph == "" {
				peer.Glyph = list[i].Glyph
			}
			if peer.Verifier == "" {
				peer.Verifier = list[i].Verifier
			}
			list[i] = peer
			return s.save(list)
		}
	}
	return s.save(append(list, peer))
}

func (s *Store) Find(raw string) (Peer, bool, error) {
	items, err := s.List()
	if err != nil {
		return Peer{}, false, err
	}
	cleanRaw := strings.TrimSuffix(raw, "/")
	for _, peer := range items {
		if peer.URL == cleanRaw || (peer.SSHDestination != "" && peer.SSHDestination == cleanRaw) || (peer.Name != "" && strings.EqualFold(peer.Name, cleanRaw)) {
			return peer, true, nil
		}
	}
	return Peer{}, false, nil
}
func (s *Store) Remove(raw string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.list()
	if e != nil {
		return e
	}
	out := p[:0]
	for _, x := range p {
		if x.URL != raw {
			out = append(out, x)
		}
	}
	return s.save(out)
}
func (s *Store) list() ([]Peer, error) {
	b, e := os.ReadFile(s.path)
	if os.IsNotExist(e) {
		return []Peer{}, nil
	}
	if e != nil {
		return nil, e
	}
	var p []Peer
	return p, json.Unmarshal(b, &p)
}
func (s *Store) save(p []Peer) error {
	if e := os.MkdirAll(filepath.Dir(s.path), 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(p, "", "  ")
	if e != nil {
		return e
	}
	// Atomic write (tempfile + rename) so a kill mid-write can't
	// truncate the live peers.json. Matches the pattern in
	// internal/hostid and internal/uistate; the journal claim
	// 'all atomic on disk' depends on it.
	temporary, e := os.CreateTemp(filepath.Dir(s.path), ".peers-*")
	if e != nil {
		return e
	}
	name := temporary.Name()
	defer os.Remove(name)
	if e := temporary.Chmod(0600); e == nil {
		_, e = temporary.Write(append(b, '\n'))
	}
	if closeErr := temporary.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(name, s.path)
}
