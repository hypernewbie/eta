package roots

import (
	"path/filepath"
	"testing"
)

func TestAddAppendsAndPersistsAcrossLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roots.json")
	store := New(path)

	list, err := store.Add("home", "/srv/home")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Path != "/srv/home" || list[0].Removed {
		t.Fatalf("unexpected list after add: %#v", list)
	}

	reloaded, err := New(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded) != 1 || reloaded[0].Path != "/srv/home" {
		t.Fatalf("did not persist: %#v", reloaded)
	}
}

func TestAddRejectsADuplicateActivePath(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "roots.json"))
	if _, err := store.Add("home", "/srv/home"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("home again", "/srv/home"); err == nil {
		t.Fatal("expected an error adding the same active path twice")
	}
}

// The core invariant: removing an earlier root must not renumber a
// later one. A splice-based removal would make index 1 become "docs" —
// exactly the bug a persisted reference (a shortcut, a window, a
// transfer job) naming index 1 as "photos" would silently inherit.
func TestRemoveTombstonesInPlaceWithoutRenumbering(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "roots.json"))
	if _, err := store.Add("home", "/srv/home"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("photos", "/srv/photos"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("docs", "/srv/docs"); err != nil {
		t.Fatal(err)
	}

	list, err := store.Remove(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("remove changed the list length: %#v", list)
	}
	if !list[0].Removed || list[0].Path != "/srv/home" {
		t.Fatalf("index 0 should be a tombstoned /srv/home, got %#v", list[0])
	}
	if list[1].Removed || list[1].Path != "/srv/photos" {
		t.Fatalf("index 1 must still be the active /srv/photos, got %#v", list[1])
	}
	if list[2].Removed || list[2].Path != "/srv/docs" {
		t.Fatalf("index 2 must still be the active /srv/docs, got %#v", list[2])
	}
}

func TestRemoveRejectsOutOfRangeOrAlreadyRemoved(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "roots.json"))
	if _, err := store.Add("home", "/srv/home"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Remove(5); err == nil {
		t.Fatal("expected an error removing an out-of-range id")
	}
	if _, err := store.Remove(-1); err == nil {
		t.Fatal("expected an error removing a negative id")
	}
	if _, err := store.Remove(0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Remove(0); err == nil {
		t.Fatal("expected an error removing an already-removed id")
	}
}

// Re-adding a path that was removed reactivates its original slot
// rather than appending a second entry for the same directory — the
// list would otherwise accumulate a tombstone every time someone
// toggled the same root off and back on.
func TestAddReactivatesATombstonedSlotAtItsOriginalIndex(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "roots.json"))
	if _, err := store.Add("home", "/srv/home"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("photos", "/srv/photos"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Remove(0); err != nil {
		t.Fatal(err)
	}
	list, err := store.Add("home again", "/srv/home")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("re-adding a removed path should reuse its slot, not append: %#v", list)
	}
	if list[0].Removed || list[0].Path != "/srv/home" || list[0].Name != "home again" {
		t.Fatalf("index 0 should be reactivated with the new name: %#v", list[0])
	}
}

func TestLoadOfMissingFileIsEmptyNotAnError(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "does-not-exist.json"))
	list, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected an empty list, got %#v", list)
	}
}
