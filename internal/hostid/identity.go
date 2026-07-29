// Package hostid provides Eta's persistent, display-safe machine identity.
package hostid

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Identity is safe to expose to trusted Eta peers and the browser UI. It does
// not contain paths, network addresses, or credentials.
type Identity struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Accent   string `json:"accent"`
	Glyph    string `json:"glyph"`
}

var accents = []string{
	"purple", "blue", "green", "amber", "red", "pink", "teal", "indigo",
	"orange", "cyan", "rose", "lime", "white", "gold", "violet", "emerald",
	"neon", "coral", "fuchsia", "canary", "copper", "mint",
}

// glyphs are Phi's 96-glyph, width-filtered Egyptian hieroglyph pool. Keeping
// the same deterministic FNV-1a mapping lets users build a durable visual
// association between an Eta host identity and its window marker.
var glyphs = []string{
	"𓀀", "𓀊", "𓀔", "𓀞", "𓀨", "𓀲", "𓀼", "𓁇", "𓁑", "𓁛", "𓁥", "𓁯", "𓁹", "𓂃", "𓂎", "𓂘",
	"𓂢", "𓂬", "𓂹", "𓃃", "𓃏", "𓃙", "𓃣", "𓃭", "𓃷", "𓄁", "𓄋", "𓄕", "𓄡", "𓄫", "𓄵", "𓅀",
	"𓅊", "𓅖", "𓅠", "𓅫", "𓅵", "𓆀", "𓆋", "𓆕", "𓆟", "𓆩", "𓆵", "𓇃", "𓇏", "𓇚", "𓇫", "𓇵",
	"𓈀", "𓈊", "𓈕", "𓈟", "𓈩", "𓈳", "𓈽", "𓉇", "𓉑", "𓉛", "𓉦", "𓉰", "𓉿", "𓊉", "𓊓", "𓊝",
	"𓊪", "𓊴", "𓊾", "𓋉", "𓋓", "𓋝", "𓋧", "𓋱", "𓋼", "𓌍", "𓌛", "𓌪", "𓌶", "𓍀", "𓍎", "𓍞",
	"𓍬", "𓍶", "𓎀", "𓎏", "𓎞", "𓎨", "𓎳", "𓎽", "𓏈", "𓏔", "𓏟", "𓏱", "𓏾", "𓐌", "𓐖", "𓐠",
}

// AccentNames returns a copy of the supported Phi accent registry names.
func AccentNames() []string { return append([]string(nil), accents...) }

// DefaultPath returns the per-user identity path on macOS, Windows, and Linux.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(dir, "eta", "identity.json"), nil
}

// LoadOrCreate loads a stable machine ID, creating it atomically when absent.
// Hostname is refreshed at every load so ordinary computer renames are shown
// without changing the host's stable ID, accent, or glyph.
func LoadOrCreate(path, hostname, accentOverride string) (Identity, error) {
	if hostname == "" {
		return Identity{}, errors.New("hostname is required")
	}
	if accentOverride != "" && !validAccent(accentOverride) {
		return Identity{}, fmt.Errorf("unknown accent %q", accentOverride)
	}

	identity, err := load(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Identity{}, err
	}
	created := errors.Is(err, os.ErrNotExist)
	if created {
		id, err := randomID()
		if err != nil {
			return Identity{}, err
		}
		identity = Identity{ID: id}
	}
	if identity.ID == "" {
		return Identity{}, fmt.Errorf("read identity %q: missing id", path)
	}

	changed := created
	if identity.Hostname != hostname {
		identity.Hostname = hostname
		changed = true
	}
	if accentOverride != "" && identity.Accent != accentOverride {
		identity.Accent = accentOverride
		changed = true
	}
	if !validAccent(identity.Accent) {
		identity.Accent = accentFor(identity.ID)
		changed = true
	}
	glyph := glyphFor(identity.ID)
	if identity.Glyph != glyph {
		identity.Glyph = glyph
		changed = true
	}
	if changed {
		if err := store(path, identity); err != nil {
			return Identity{}, err
		}
	}
	return identity, nil
}

// For returns a deterministic identity for tests and in-memory callers.
func For(id, hostname string) Identity {
	return Identity{ID: id, Hostname: hostname, Accent: accentFor(id), Glyph: glyphFor(id)}
}

func load(path string) (Identity, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Identity{}, err
	}
	var identity Identity
	if err := json.Unmarshal(body, &identity); err != nil {
		return Identity{}, fmt.Errorf("decode identity %q: %w", path, err)
	}
	return identity, nil
}

func store(path string, identity Identity) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create identity directory: %w", err)
	}
	body, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return fmt.Errorf("encode identity: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return fmt.Errorf("create temporary identity: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set identity permissions: %w", err)
	}
	if _, err := temporary.Write(append(body, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write identity: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close identity: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("replace identity: %w", err)
	}
	return nil
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate identity: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func validAccent(accent string) bool {
	for _, candidate := range accents {
		if accent == candidate {
			return true
		}
	}
	return false
}

func accentFor(id string) string { return accents[fnv1a(id)%uint32(len(accents))] }
func glyphFor(id string) string  { return glyphs[fnv1a(id)%uint32(len(glyphs))] }

func fnv1a(value string) uint32 {
	hash := uint32(2166136261)
	for _, byte := range []byte(value) {
		hash ^= uint32(byte)
		hash *= 16777619
	}
	return hash
}
