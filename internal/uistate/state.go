// Package uistate persists Eta's small, versioned desktop intent state.
package uistate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const Version = 1
const maxWindows = 128

type Window struct {
	Kind      string `json:"kind"`
	Root      int    `json:"root,omitempty"`
	Path      string `json:"path,omitempty"`
	Peer      string `json:"peer,omitempty"`
	X         int    `json:"x,omitempty"`
	Y         int    `json:"y,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Minimized bool   `json:"minimized,omitempty"`
	Maximized bool   `json:"maximized,omitempty"`
}

type State struct {
	Version int      `json:"version"`
	Windows []Window `json:"windows,omitempty"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "eta", "state.json"), nil
}

func New(path string) *Store { return &Store{path: path} }

func (s *Store) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return State{Version: Version}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read UI state: %w", err)
	}
	var state State
	if err := json.Unmarshal(body, &state); err != nil {
		return State{}, fmt.Errorf("decode UI state: %w", err)
	}
	if state.Version == 0 {
		state.Version = Version
	}
	if state.Version != Version {
		return State{Version: Version}, nil
	}
	if err := validate(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *Store) Save(state State) error {
	state.Version = Version
	if err := validate(state); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create UI state directory: %w", err)
	}
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode UI state: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".state-*")
	if err != nil {
		return fmt.Errorf("create temporary UI state: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set UI state permissions: %w", err)
	}
	if _, err := temporary.Write(append(body, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write UI state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close UI state: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		return fmt.Errorf("replace UI state: %w", err)
	}
	return nil
}

func validate(state State) error {
	if len(state.Windows) > maxWindows {
		return fmt.Errorf("UI state has too many windows")
	}
	for _, window := range state.Windows {
		if window.Kind != "explorer" && window.Kind != "file" {
			return fmt.Errorf("unknown window kind %q", window.Kind)
		}
		if window.Root < 0 || strings.HasPrefix(window.Path, "/") || strings.Contains(window.Path, "\\") || (window.Peer != "" && (len(window.Peer) > 2048 || !strings.HasPrefix(window.Peer, "http"))) {
			return fmt.Errorf("invalid window path")
		}
		if window.Kind == "file" && window.Path == "" {
			return fmt.Errorf("file window has no path")
		}
		if window.Width < 0 || window.Height < 0 || window.X < 0 || window.Y < 0 {
			return fmt.Errorf("invalid window geometry")
		}
	}
	return nil
}
