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
func (s *Store) Find(raw string) (Peer, bool, error) {
	items, err := s.List()
	if err != nil {
		return Peer{}, false, err
	}
	for _, peer := range items {
		if peer.URL == strings.TrimSuffix(raw, "/") {
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
