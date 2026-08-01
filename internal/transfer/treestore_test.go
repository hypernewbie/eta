package transfer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// treeFixture sets up a fresh root with a populated source tree, then
// returns a TreeStore bound to that root plus convenience fields for
// the test. Each test gets its own t.TempDir().
func treeFixture(t *testing.T) (store *TreeStore, root string, sourceDir string, tree Tree) {
	t.Helper()
	dir := t.TempDir()
	root = filepath.Join(dir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	sourceDir = filepath.Join(dir, "source")
	if err := os.Mkdir(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	mustMkdir(t, filepath.Join(sourceDir, "nested"))
	mustMkdir(t, filepath.Join(sourceDir, "empty"))
	mustWrite(t, filepath.Join(sourceDir, "top.md"), "# top\n")
	mustWrite(t, filepath.Join(sourceDir, "nested/leaf.md"), "# leaf\n")
	mustWrite(t, filepath.Join(sourceDir, "nested/big.md"), string(make([]byte, 4096)))
	built, err := BuildTree(sourceDir)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	return NewTreeStore(root), root, sourceDir, built
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stageAllFiles writes every file in the tree to its corresponding
// staging path under the given session ID, simulating what the
// per-file /api/transfers PUT path produces after a successful rename.
func stageAllFiles(t *testing.T, store *TreeStore, id string, source string, tree Tree) {
	t.Helper()
	for _, file := range tree.Files {
		staging, err := store.ResolveStagingPath(id, file.Path)
		if err != nil {
			t.Fatalf("ResolveStagingPath(%q): %v", file.Path, err)
		}
		body, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatal(err)
		}
		mustMkdir(t, filepath.Dir(staging))
		mustWrite(t, staging, string(body))
	}
}

func TestTreeStoreCreateHappyPath(t *testing.T) {
	store, _, _, tree := treeFixture(t)
	id, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("Create returned empty id")
	}
	// Staging exists; directories pre-created; intent recorded.
	staging := store.StagingRoot(id)
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("staging missing: %v", err)
	}
	for _, dir := range tree.Directories {
		if _, err := os.Stat(filepath.Join(staging, filepath.FromSlash(dir))); err != nil {
			t.Fatalf("staged dir %q missing: %v", dir, err)
		}
	}
	intent, complete, err := store.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Destination != "destination" {
		t.Fatalf("unexpected destination: %q", intent.Destination)
	}
	if len(intent.Files) != len(tree.Files) {
		t.Fatalf("intent files %d, want %d", len(intent.Files), len(tree.Files))
	}
	for _, file := range tree.Files {
		if complete[file.Path] {
			t.Fatalf("file %q should be incomplete before staging", file.Path)
		}
	}
}

func TestTreeStoreCreateRefusesExistingDestination(t *testing.T) {
	store, root, _, tree := treeFixture(t)
	mustMkdir(t, filepath.Join(root, "destination"))
	if _, err := store.Create("destination", tree); err == nil {
		t.Fatal("expected destination-exists error")
	} else if err.Error() != "destination exists" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTreeStoreCreateRefusesReservedRoot(t *testing.T) {
	store, _, _, tree := treeFixture(t)
	cases := []string{".eta", ".eta/staging", ".eta/intents/foo"}
	for _, dest := range cases {
		if _, err := store.Create(dest, tree); err == nil {
			t.Fatalf("expected reserved-path error for %q", dest)
		}
	}
}

func TestTreeStoreCreateRejectsBadPaths(t *testing.T) {
	store, _, _, _ := treeFixture(t)
	cases := []string{"", ".", "..", "../escape", "/abs", "a/../../b"}
	for _, p := range cases {
		if _, err := store.Create(p, Tree{}); err == nil {
			t.Fatalf("expected error for destination %q", p)
		}
	}
}

func TestTreeStoreCreateRejectsEscapeInTreeFiles(t *testing.T) {
	store, _, _, _ := treeFixture(t)
	tree := Tree{Files: []TreeFile{{Path: "../escape.txt", Manifest: mustManifest(t, 12)}}}
	if _, err := store.Create("dest", tree); err == nil {
		t.Fatal("expected escape error")
	}
}

func mustManifest(t *testing.T, size int) Manifest {
	t.Helper()
	body := make([]byte, size)
	f, err := os.CreateTemp(t.TempDir(), "m-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err = os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := BuildManifest(f, DefaultChunkSize)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTreeStoreTouchUpdatesLastProgress(t *testing.T) {
	store, _, _, tree := treeFixture(t)
	id, err := store.Create("dest", tree)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := store.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := store.Touch(id); err != nil {
		t.Fatal(err)
	}
	second, _, err := store.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	if !second.LastProgress.After(first.LastProgress) {
		t.Fatalf("LastProgress did not advance: %v vs %v", first.LastProgress, second.LastProgress)
	}
}

func TestTreeStoreCommitAtomicPromotion(t *testing.T) {
	store, root, source, tree := treeFixture(t)
	id, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	stageAllFiles(t, store, id, source, tree)

	if err := store.Commit(id); err != nil {
		t.Fatalf("commit: %v", err)
	}
	// Destination tree exists with the right files.
	for _, file := range tree.Files {
		dst := filepath.Join(root, "destination", filepath.FromSlash(file.Path))
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatalf("destination file %q missing: %v", file.Path, err)
		}
		if info.Size() != file.Manifest.Size {
			t.Fatalf("destination file %q has size %d, want %d", file.Path, info.Size(), file.Manifest.Size)
		}
	}
	// Staging is gone.
	staging := store.StagingRoot(id)
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging should be gone, got err=%v", err)
	}
	// Intent record is gone.
	if _, err := os.Stat(store.intentPath(id)); !os.IsNotExist(err) {
		t.Fatalf("intent should be gone, got err=%v", err)
	}
}

func TestTreeStoreCommitRefusesIfDestinationAppeared(t *testing.T) {
	store, root, source, tree := treeFixture(t)
	id, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	stageAllFiles(t, store, id, source, tree)
	// Race: someone creates the destination between staging and commit.
	if err := os.MkdirAll(filepath.Join(root, "destination"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(id); err == nil {
		t.Fatal("commit must refuse when destination exists")
	}
}

func TestTreeStoreCommitRefusesIncomplete(t *testing.T) {
	store, _, source, tree := treeFixture(t)
	id, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	// Stage only half the files.
	for i, file := range tree.Files {
		if i >= len(tree.Files)/2 {
			break
		}
		staging, err := store.ResolveStagingPath(id, file.Path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := os.ReadFile(filepath.Join(source, filepath.FromSlash(file.Path)))
		mustMkdir(t, filepath.Dir(staging))
		mustWrite(t, staging, string(body))
	}
	if err := store.Commit(id); err == nil {
		t.Fatal("commit must refuse when files are missing")
	}
}

func TestTreeStoreCommitRefusesWrongSize(t *testing.T) {
	store, _, source, tree := treeFixture(t)
	id, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Files) == 0 {
		t.Skip("tree has no files")
	}
	stageAllFiles(t, store, id, source, tree)
	first := tree.Files[0]
	staging, _ := store.ResolveStagingPath(id, first.Path)
	body, _ := os.ReadFile(staging)
	if err := os.WriteFile(staging, append(body, 'X'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(id); err == nil {
		t.Fatal("commit must refuse wrong-size file")
	}
}

// TreeBuilderCompatibility covers BuildTree + TreeStore together: every
// file path in the tree must be stageable, and the staging dir must
// contain every source file at commit time.
func TestTreeStoreRoundtripAcrossFullTree(t *testing.T) {
	store, root, source, tree := treeFixture(t)
	id, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	stageAllFiles(t, store, id, source, tree)
	if err := store.Commit(id); err != nil {
		t.Fatal(err)
	}
	// Every destination file byte-equal to source.
	for _, file := range tree.Files {
		want, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(root, "destination", filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("file %q content mismatch", file.Path)
		}
	}
}

func TestTreeStoreAbortClearsStagingAndIntent(t *testing.T) {
	store, _, _, tree := treeFixture(t)
	id, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Abort(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.StagingRoot(id)); !os.IsNotExist(err) {
		t.Fatalf("staging not removed: %v", err)
	}
	if _, err := os.Stat(store.intentPath(id)); !os.IsNotExist(err) {
		t.Fatalf("intent not removed: %v", err)
	}
}

// CrashSimulationInterruptedCommit: staging survives a process that
// died after file PUTs but before commit. Destination must be absent
// and re-issuing Create must succeed (different ID, but file-identical
// staging ready for retry).
func TestTreeStoreCrashInterruptedCommit(t *testing.T) {
	store, root, source, tree := treeFixture(t)
	id, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	stageAllFiles(t, store, id, source, tree)
	// "Crash": we just don't call Commit.
	if _, err := os.Stat(filepath.Join(root, "destination")); !os.IsNotExist(err) {
		t.Fatalf("destination should be absent on interrupted commit, got err=%v", err)
	}
	// Staging is still there for retry.
	staging := store.StagingRoot(id)
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("staging should survive interrupted commit: %v", err)
	}
	// Status correctly reports all files as complete (resume-friendly).
	_, complete, err := store.Status(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range tree.Files {
		if !complete[file.Path] {
			t.Fatalf("file %q should be complete after staging", file.Path)
		}
	}
}

func TestTreeStoreSweepByLastProgress(t *testing.T) {
	store, _, _, tree := treeFixture(t)
	id, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	// Backdate the intent so it looks stale.
	intent, _, _ := store.Status(id)
	intent.LastProgress = time.Now().Add(-48 * time.Hour)
	store.mu.Lock()
	if err := store.saveIntentLocked(intent); err != nil {
		t.Fatal(err)
	}
	store.mu.Unlock()
	n, err := store.Sweep(24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("swept = %d, want 1", n)
	}
	if _, err := os.Stat(store.StagingRoot(id)); !os.IsNotExist(err) {
		t.Fatal("staging not swept")
	}
}

func TestTreeStoreSweepRemovesOrphans(t *testing.T) {
	store, _, _, _ := treeFixture(t)
	orphan := store.StagingRoot("orphan-id")
	if err := os.MkdirAll(filepath.Join(orphan, "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if n, err := store.Sweep(1*time.Hour, time.Now()); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		// No active session to count; orphan removed without contributing
		// to the swept-sessions count.
		t.Fatalf("swept = %d, want 0", n)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan not removed: %v", err)
	}
}

func TestTreeStoreIdIsUniquePerCreate(t *testing.T) {
	store, _, _, tree := treeFixture(t)
	first, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	// Manually remove the destination so a second Create can succeed.
	if err := os.RemoveAll(filepath.Join(store.rootPath, "destination")); err != nil {
		t.Fatal(err)
	}
	second, err := store.Create("destination", tree)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("ids not unique: %q vs %q", first, second)
	}
}

func TestTreeStoreSweepKeepActiveSession(t *testing.T) {
	store, _, _, tree := treeFixture(t)
	if _, err := store.Create("destination", tree); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	n, err := store.Sweep(24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("fresh session swept: %d", n)
	}
}

func TestTreeStoreValidateRelativeCleanliness(t *testing.T) {
	bad := []string{"", ".", "..", "../x", "/abs", "a/../b", "a//b"}
	for _, p := range bad {
		if err := ValidateRelative(p); err == nil {
			t.Fatalf("expected ValidateRelative to reject %q", p)
		}
	}
	good := []string{"a", "a/b", "deep/nested/file.txt", "a-b_c.d"}
	for _, p := range good {
		if err := ValidateRelative(p); err != nil {
			t.Fatalf("ValidateRelative(%q) unexpectedly rejected: %v", p, err)
		}
	}
}

// Compile-time guard that TreeStore satisfies an interface we expect
// Compile-time guard that TreeStore satisfies the HTTP handler contract.
var _ TreeAPI = (*TreeStore)(nil)

// TreeAPI is the contract the HTTP handlers use against a per-root
// store. Asserted via the compile-time guard above.
type TreeAPI interface {
	Create(destination string, tree Tree) (string, error)
	Status(id string) (TreeIntent, map[string]bool, error)
	Touch(id string) error
	Commit(id string) error
	Abort(id string) error
	ResolveStagingPath(id, relative string) (string, error)
	StagingRoot(id string) string
}
