package uistate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eta", "state.json")
	store := New(path)
	want := State{Windows: []Window{{Kind: "explorer", Root: 1, Path: "docs", Peer: "http://peer.example:7080", X: 10, Y: 20, Width: 800, Height: 600}, {Kind: "file", Root: 1, Path: "docs/a.md", Maximized: true}}}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != Version || len(got.Windows) != 2 || got.Windows[0].Peer != "http://peer.example:7080" || got.Windows[1].Path != "docs/a.md" {
		t.Fatalf("unexpected state: %#v", got)
	}
}

func TestStoreMissingAndFutureStateAreSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := New(path)
	state, err := store.Load()
	if err != nil || state.Version != Version || len(state.Windows) != 0 {
		t.Fatalf("missing state = %#v, %v", state, err)
	}
	if err := os.WriteFile(path, []byte(`{"version":99,"windows":[{"kind":"explorer"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err = store.Load()
	if err != nil || state.Version != Version || len(state.Windows) != 0 {
		t.Fatalf("future state = %#v, %v", state, err)
	}
}

func TestStoreRejectsUnsafeWindows(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "state.json"))
	for _, state := range []State{
		{Windows: []Window{{Kind: "terminal"}}},
		{Windows: []Window{{Kind: "file", Path: ""}}},
		{Windows: []Window{{Kind: "file", Path: "/secret"}}},
		{Windows: []Window{{Kind: "explorer", X: -1}}},
	} {
		if err := store.Save(state); err == nil {
			t.Fatalf("unsafe state accepted: %#v", state)
		}
	}
}
