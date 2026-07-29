package hostid

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadOrCreatePersistsStableIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eta", "identity.json")
	first, err := LoadOrCreate(path, "alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.Glyph == "" || !validAccent(first.Accent) {
		t.Fatalf("invalid created identity: %#v", first)
	}

	second, err := LoadOrCreate(path, "bravo", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Accent != first.Accent || second.Glyph != first.Glyph {
		t.Fatalf("stable fields changed: first=%#v second=%#v", first, second)
	}
	if second.Hostname != "bravo" {
		t.Fatalf("hostname = %q, want bravo", second.Hostname)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("identity permissions = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestLoadOrCreateAccentOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	identity, err := LoadOrCreate(path, "alpha", "gold")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Accent != "gold" {
		t.Fatalf("accent = %q, want gold", identity.Accent)
	}
	if _, err := LoadOrCreate(path, "alpha", "not-a-color"); err == nil {
		t.Fatal("invalid accent did not fail")
	}
}

func TestLoadOrCreateRejectsMalformedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path, "alpha", ""); err == nil {
		t.Fatal("malformed identity did not fail")
	}
}

func TestForIsDeterministic(t *testing.T) {
	first := For("host-1", "alpha")
	second := For("host-1", "bravo")
	if first.Accent != second.Accent || first.Glyph != second.Glyph {
		t.Fatalf("identity visuals changed: first=%#v second=%#v", first, second)
	}
	if first.Hostname != "alpha" || second.Hostname != "bravo" {
		t.Fatal("hostname was not retained")
	}
}
