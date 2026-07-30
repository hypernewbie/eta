// Package terminal manages short-lived local PTY sessions.
package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

const maxBufferedOutput = 1 << 20

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}
type Session struct {
	mu      sync.Mutex
	file    *os.File
	command *exec.Cmd
	output  []byte
	closed  bool
}

func NewManager() *Manager { return &Manager{sessions: map[string]*Session{}} }
func (m *Manager) Start(directory string, columns, rows uint16) (string, error) {
	if columns == 0 {
		columns = 120
	}
	if rows == 0 {
		rows = 36
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: columns, Rows: rows})
	if err != nil {
		return "", err
	}
	s := &Session{file: file, command: cmd}
	id, err := newID()
	if err != nil {
		file.Close()
		return "", err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	go s.collect()
	go func() { _ = cmd.Wait(); s.close() }()
	return id, nil
}
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}
func (m *Manager) Close(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s != nil {
		s.close()
	}
}
func (s *Session) collect() {
	buffer := make([]byte, 32<<10)
	for {
		n, err := s.file.Read(buffer)
		if n > 0 {
			s.mu.Lock()
			s.output = append(s.output, buffer[:n]...)
			if len(s.output) > maxBufferedOutput {
				s.output = append([]byte(nil), s.output[len(s.output)-maxBufferedOutput:]...)
			}
			s.mu.Unlock()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.close()
			}
			return
		}
	}
}
func (s *Session) Output(offset int) ([]byte, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset < 0 {
		offset = 0
	}
	if offset > len(s.output) {
		offset = len(s.output)
	}
	return append([]byte(nil), s.output[offset:]...), len(s.output), s.closed
}
func (s *Session) Input(body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("terminal is closed")
	}
	_, err := s.file.Write(body)
	return err
}
func (s *Session) Resize(columns, rows uint16) error {
	if columns == 0 || rows == 0 {
		return errors.New("invalid terminal size")
	}
	return pty.Setsize(s.file, &pty.Winsize{Cols: columns, Rows: rows})
}
func (s *Session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	file := s.file
	s.mu.Unlock()
	_ = file.Close()
}
func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
