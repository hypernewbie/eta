package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	dir string
	mu  sync.Mutex
}
type session struct {
	Manifest Manifest `json:"manifest"`
	Complete []bool   `json:"complete"`
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}
func (s *Store) Open(id string, m Manifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if filepath.Base(id) != id || id == "" {
		return errors.New("invalid transfer ID")
	}
	_, e := s.load(id)
	if e == nil {
		return nil
	}
	if !os.IsNotExist(e) {
		return e
	}
	f, e := os.OpenFile(s.part(id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if e = f.Truncate(m.Size); e == nil {
		e = f.Close()
	}
	if e != nil {
		return e
	}
	return s.save(id, session{Manifest: m, Complete: make([]bool, len(m.Chunks))})
}
func (s *Store) Missing(id string) ([]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, e := s.load(id)
	if e != nil {
		return nil, e
	}
	var missing []int
	for i, done := range x.Complete {
		if !done {
			missing = append(missing, i)
		}
	}
	return missing, nil
}
func (s *Store) Write(id string, index int, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, e := s.load(id)
	if e != nil {
		return e
	}
	if e = x.Manifest.Verify(index, body); e != nil {
		return e
	}
	f, e := os.OpenFile(s.part(id), os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.WriteAt(body, int64(index)*x.Manifest.ChunkSize)
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	x.Complete[index] = true
	return s.save(id, x)
}
func (s *Store) Finalize(id, destination string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, e := s.load(id)
	if e != nil {
		return e
	}
	for _, done := range x.Complete {
		if !done {
			return errors.New("transfer incomplete")
		}
	}
	if _, e = os.Stat(destination); e == nil {
		return errors.New("destination exists")
	} else if !os.IsNotExist(e) {
		return e
	}
	if e = os.Rename(s.part(id), destination); e != nil {
		return e
	}
	return os.Remove(s.meta(id))
}
func (s *Store) part(id string) string { return filepath.Join(s.dir, id+".part") }
func (s *Store) meta(id string) string { return filepath.Join(s.dir, id+".json") }
func (s *Store) load(id string) (session, error) {
	b, e := os.ReadFile(s.meta(id))
	if e != nil {
		return session{}, e
	}
	var x session
	if e = json.Unmarshal(b, &x); e != nil {
		return session{}, e
	}
	return x, nil
}
func (s *Store) save(id string, x session) error {
	b, e := json.Marshal(x)
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(s.dir, ".transfer-")
	if e != nil {
		return e
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, e = tmp.Write(b); e == nil {
		e = tmp.Close()
	}
	if e != nil {
		return e
	}
	return os.Rename(name, s.meta(id))
}
func (s *Store) String() string { return fmt.Sprintf("transfer store %s", s.dir) }
