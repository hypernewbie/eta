// Package roots persists Eta's exposed root directories as a stable,
// append-only list. A root's position in this list is its ID —
// handleRoots in main.go has returned {id: index, name} to the browser
// since before this package existed, and every persisted reference to a
// root (a desktop shortcut, a window's state, a transfer job) is that
// same index. Removing a root by splicing it out of a slice would
// silently repoint every one of those references at whatever root
// happened to shift into its old position; the danger is not merely a
// renumbering, it is a Copy or Delete quietly landing in the wrong
// directory.
//
// So removal here never deletes or renumbers a slot. It sets Removed on
// the entry in place. New roots are only ever appended, keeping every
// existing index meaningful for the life of this config file — a
// request naming a removed root's old index fails cleanly ("unknown
// root"), never silently resolves to a directory that took its place,
// because nothing ever takes its place.
package roots

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Root is Name/Path, matching the shape main.go already built in memory
// from -root flags, plus Removed for a tombstoned slot.
type Root struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Removed bool   `json:"removed,omitempty"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store { return &Store{path: path} }

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "eta", "roots.json"), nil
}

func (s *Store) Load() ([]Root, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() ([]Root, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []Root
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	return list, nil
}

func (s *Store) save(list []Root) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// SaveAll overwrites the whole list, position for position. Used once,
// at startup, to seed the file from -root flags when it does not exist
// yet — every later mutation goes through Add/Remove instead, so a
// caller cannot accidentally renumber an established list by saving a
// stale copy of it.
func (s *Store) SaveAll(list []Root) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(list)
}

// Add appends a new root, or reactivates a removed one at its original
// index if the same path was removed before — re-adding a root you just
// removed should not create a second entry for it, and reusing the old
// slot rather than appending keeps the list from growing tombstones for
// what is really the same root toggled off and back on.
func (s *Store) Add(name, path string) ([]Root, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return nil, err
	}
	for i, r := range list {
		if r.Path != path {
			continue
		}
		if !r.Removed {
			return nil, errors.New("root already added")
		}
		list[i].Removed = false
		list[i].Name = name
		if err := s.save(list); err != nil {
			return nil, err
		}
		return list, nil
	}
	list = append(list, Root{Name: name, Path: path})
	if err := s.save(list); err != nil {
		return nil, err
	}
	return list, nil
}

// Remove tombstones the root at id (its index) in place. id must
// address an existing, currently-active entry.
func (s *Store) Remove(id int) ([]Root, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return nil, err
	}
	if id < 0 || id >= len(list) || list[id].Removed {
		return nil, errors.New("unknown root")
	}
	list[id].Removed = true
	if err := s.save(list); err != nil {
		return nil, err
	}
	return list, nil
}
