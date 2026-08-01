package hostid

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
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

func TestSetAccentRejectsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if _, err := SetAccent(path, "not-a-color"); err == nil {
		t.Fatal("unknown accent did not fail")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("SetAccent created identity.json on validation failure")
	}
}

func TestSetAccentRequiresExistingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	if _, err := SetAccent(missing, "red"); err == nil {
		t.Fatal("SetAccent on missing file did not fail")
	}
}

func TestSetAccentPersistsAndPreservesFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eta", "identity.json")
	original, err := LoadOrCreate(path, "alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := SetAccent(path, "gold")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Accent != "gold" {
		t.Fatalf("accent = %q, want gold", updated.Accent)
	}
	if updated.ID != original.ID || updated.Hostname != original.Hostname || updated.Glyph != original.Glyph {
		t.Fatalf("stable fields changed: original=%#v updated=%#v", original, updated)
	}
	// Round-trip: reload and confirm the new accent persists.
	reloaded, err := LoadOrCreate(path, "alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Accent != "gold" {
		t.Fatalf("reloaded accent = %q, want gold", reloaded.Accent)
	}
}

func TestSetAccentIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	if _, err := LoadOrCreate(path, "alpha", ""); err != nil {
		t.Fatal(err)
	}
	// Concurrent SetAccent calls must converge without leaving a
	// partial file on disk. Final on-disk accent must be one of the
	// values callers passed.
	const callers = 8
	accents := []string{"red", "blue", "green", "amber", "pink", "teal", "indigo", "orange"}
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func(accent string) {
			defer wg.Done()
			_, _ = SetAccent(path, accent)
		}(accents[i])
	}
	wg.Wait()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got Identity
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, candidate := range accents {
		if got.Accent == candidate {
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("final accent %q is not one of %v", got.Accent, accents)
	}
}
