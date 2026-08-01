package transfer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TreeStore manages a per-root tree-transfer session lifecycle. A session
// reserves a destination, stages every file under that root's hidden
// staging directory, and atomically promotes the staging tree to the
// destination via a single os.Rename. The promotion is POSIX-atomic on
// any single filesystem; no half-copied destination tree can ever be
// observed.
//
// The contract this store enforces:
//   - destination must NOT exist before Create (single-rename semantics)
//   - files & directories inside the staging tree must be valid relative paths
//   - the staging root (".eta") is reserved and never user-creatable
//   - commit either succeeds completely or leaves the destination absent
//
// Staging lives inside the destination root on purpose: it removes the
// EXDEV cross-filesystem rename hazard that per-file staging (under the
// user cache directory) currently has against NAS-mounted roots.
type TreeStore struct {
	rootPath string
	mu       sync.Mutex
}

// TreeIntent is the durable per-session record. Persisted atomically
// alongside the staging directory under {root}/.eta/intents/{id}.json.
type TreeIntent struct {
	Version      int                `json:"version"`
	ID           string             `json:"id"`
	Destination  string             `json:"destination"`
	Directories  []string           `json:"directories"`
	Files        []TreeIntentFile   `json:"files"`
	Created      time.Time          `json:"created"`
	LastProgress time.Time          `json:"lastProgress"`
}

// TreeIntentFile is the per-file record kept in the intent. Size is the
// pre-transfer byte length so commit can verify presence without keeping
// full chunk manifests in the intent record (which would balloon for
// big trees).
type TreeIntentFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

const (
	treeIntentVersion = 1
	stagingDirName    = ".eta"
	stagingSubdir     = "staging"
	intentsSubdir     = "intents"
)

// NewTreeStore binds a tree session manager to a single filesystem root.
func NewTreeStore(rootPath string) *TreeStore {
	return &TreeStore{rootPath: rootPath}
}

// StagingRoot returns the per-session staging directory for the given ID.
// Useful for tests; the HTTP API resolves this internally.
func (s *TreeStore) StagingRoot(id string) string {
	return filepath.Join(s.stagingRoot(), id)
}

// ResolveStagingPath returns the absolute staging path a sender must
// target for a given file (relative to the session's destination tree).
func (s *TreeStore) ResolveStagingPath(id, relative string) (string, error) {
	if err := ValidateRelative(relative); err != nil {
		return "", err
	}
	return filepath.Join(s.StagingRoot(id), filepath.FromSlash(relative)), nil
}

// Create reserves a destination and creates the staging tree. The
// supplied Tree describes the directories and files that will be sent.
// On success returns the new intent ID and persists the intent record.
// On any error the partially-built staging directory is removed.
func (s *TreeStore) Create(destination string, tree Tree) (string, error) {
	if err := ValidateRelative(destination); err != nil {
		return "", err
	}
	if isReservedPath(destination) {
		return "", errors.New("destination is reserved")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fullDest := filepath.Join(s.rootPath, filepath.FromSlash(destination))
	if _, err := os.Stat(fullDest); err == nil {
		return "", errors.New("destination exists")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	id, err := newTreeID()
	if err != nil {
		return "", err
	}
	staging := s.StagingRoot(id)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return "", err
	}
	for _, dir := range tree.Directories {
		if err := ValidateRelative(dir); err != nil {
			os.RemoveAll(staging)
			return "", fmt.Errorf("directory %q: %w", dir, err)
		}
		if err := os.MkdirAll(filepath.Join(staging, filepath.FromSlash(dir)), 0o755); err != nil {
			os.RemoveAll(staging)
			return "", err
		}
	}
	files := make([]TreeIntentFile, 0, len(tree.Files))
	for _, item := range tree.Files {
		if err := ValidateRelative(item.Path); err != nil {
			os.RemoveAll(staging)
			return "", fmt.Errorf("file %q: %w", item.Path, err)
		}
		files = append(files, TreeIntentFile{Path: item.Path, Size: item.Manifest.Size})
	}
	intent := TreeIntent{
		Version:      treeIntentVersion,
		ID:           id,
		Destination:  destination,
		Directories:  append([]string(nil), tree.Directories...),
		Files:        files,
		Created:      time.Now().UTC(),
		LastProgress: time.Now().UTC(),
	}
	if err := s.saveIntentLocked(intent); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	if err := s.ensureIntentsDirLocked(); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	return id, nil
}

// ListIntents returns every persisted tree-intent record on this root,
// keyed by id. Used by the receiver to discover in-flight sessions
// during per-file finalize (so it can refresh LastProgress on the
// matching tree). Returns an empty map when no intents exist.
func (s *TreeStore) ListIntents() (map[string]TreeIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listIntentsLocked()
}

// Status returns the current intent plus a per-file "complete" map derived
// from the filesystem: each file is complete iff a regular file exists at
// its staging path with the expected size. Cheap (no chunk manifest in
// intent) and survives crashes.
func (s *TreeStore) Status(id string) (TreeIntent, map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	intent, err := s.loadIntentLocked(id)
	if err != nil {
		return TreeIntent{}, nil, err
	}
	complete := make(map[string]bool, len(intent.Files))
	for _, file := range intent.Files {
		path := filepath.Join(s.StagingRoot(id), filepath.FromSlash(file.Path))
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Size() == file.Size {
			complete[file.Path] = true
		}
	}
	return intent, complete, nil
}

// Touch refreshes the intent's LastProgress timestamp. Callers should
// invoke this after each chunk PUT or per-file finalize so crash-recovery
// sweeps can distinguish in-flight transfers from abandoned ones.
func (s *TreeStore) Touch(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	intent, err := s.loadIntentLocked(id)
	if err != nil {
		return err
	}
	intent.LastProgress = time.Now().UTC()
	return s.saveIntentLocked(intent)
}

// Commit verifies that every file in the intent is fully written under
// the staging tree, then performs a single os.Rename of the staging root
// to the destination. Either the entire destination tree appears or the
// rename fails and the staging tree remains for retry/abort.
func (s *TreeStore) Commit(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	intent, err := s.loadIntentLocked(id)
	if err != nil {
		return err
	}
	staging := s.StagingRoot(id)
	for _, file := range intent.Files {
		path := filepath.Join(staging, filepath.FromSlash(file.Path))
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("file %q not staged: %w", file.Path, err)
		}
		if !info.Mode().IsRegular() || info.Size() != file.Size {
			return fmt.Errorf("file %q incomplete", file.Path)
		}
	}
	destination := filepath.Join(s.rootPath, filepath.FromSlash(intent.Destination))
	if _, err := os.Stat(destination); err == nil {
		return errors.New("destination exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staging, destination); err != nil {
		return err
	}
	if err := os.Remove(s.intentPathLocked(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Abort removes the staging tree and intent record without committing.
func (s *TreeStore) Abort(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.RemoveAll(s.StagingRoot(id)); err != nil {
		return err
	}
	if err := os.Remove(s.intentPathLocked(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Sweep removes intent records and their staging trees whose
// LastProgress is older than ttl relative to now. Orphan staging dirs
// (no matching intent record) are also removed. Returns the number of
// sessions swept.
func (s *TreeStore) Sweep(ttl time.Duration, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	intents, err := s.listIntentsLocked()
	if err != nil {
		return 0, err
	}
	swept := 0
	for _, intent := range intents {
		if now.Sub(intent.LastProgress) <= ttl {
			continue
		}
		if err := os.RemoveAll(s.StagingRoot(intent.ID)); err != nil {
			return swept, err
		}
		if err := os.Remove(s.intentPathLocked(intent.ID)); err != nil && !os.IsNotExist(err) {
			return swept, err
		}
		swept++
	}
	// Sweep orphan staging dirs.
	stagingBase := s.stagingRoot()
	entries, err := os.ReadDir(stagingBase)
	if err != nil && !os.IsNotExist(err) {
		return swept, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, ok := intents[entry.Name()]; ok {
			continue
		}
		_ = os.RemoveAll(filepath.Join(stagingBase, entry.Name()))
	}
	return swept, nil
}

// stagingRoot returns the staging directory of this store's root.
func (s *TreeStore) stagingRoot() string {
	return filepath.Join(s.rootPath, stagingDirName, stagingSubdir)
}

// intentsRoot returns the intents directory of this store's root.
func (s *TreeStore) intentsRoot() string {
	return filepath.Join(s.rootPath, stagingDirName, intentsSubdir)
}

// intentPath returns the path to the intent JSON for a session.
func (s *TreeStore) intentPath(id string) string {
	return filepath.Join(s.intentsRoot(), id+".json")
}

func (s *TreeStore) intentPathLocked(id string) string {
	return s.intentPath(id)
}

func (s *TreeStore) ensureIntentsDirLocked() error {
	return os.MkdirAll(s.intentsRoot(), 0o700)
}

// saveIntentLocked writes the intent JSON atomically (temp file + rename).
func (s *TreeStore) saveIntentLocked(intent TreeIntent) error {
	if err := s.ensureIntentsDirLocked(); err != nil {
		return err
	}
	body, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return err
	}
	target := s.intentPathLocked(intent.ID)
	temp, err := os.CreateTemp(s.intentsRoot(), ".intent-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(append(body, '\n')); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, target)
}

func (s *TreeStore) loadIntentLocked(id string) (TreeIntent, error) {
	body, err := os.ReadFile(s.intentPathLocked(id))
	if err != nil {
		return TreeIntent{}, err
	}
	var intent TreeIntent
	if err := json.Unmarshal(body, &intent); err != nil {
		return TreeIntent{}, err
	}
	if intent.Version != treeIntentVersion {
		return TreeIntent{}, errors.New("incompatible tree intent version")
	}
	return intent, nil
}

func (s *TreeStore) listIntentsLocked() (map[string]TreeIntent, error) {
	entries, err := os.ReadDir(s.intentsRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]TreeIntent{}, nil
		}
		return nil, err
	}
	out := make(map[string]TreeIntent, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		intent, err := s.loadIntentLocked(id)
		if err != nil {
			return nil, err
		}
		out[id] = intent
	}
	return out, nil
}

// ValidateRelative rejects empty / dot / dot-dot / absolute / escape paths.
// Walks the components manually so that "a/../b" (which filepath.Clean
// collapses to "b") is still caught. Also rejects any segment that
// resolves to the empty string after cleaning. Exported so HTTP
// handlers can reuse the same rule for user-supplied paths.
func ValidateRelative(p string) error {
	if p == "" || p == "." {
		return errors.New("path is empty")
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return errors.New("path is absolute")
	}
	raw := strings.Split(filepath.FromSlash(p), string(filepath.Separator))
	for _, segment := range raw {
		switch segment {
		case "", ".", "..":
			return errors.New("path escapes root")
		}
	}
	cleaned := filepath.ToSlash(filepath.Clean(p))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("path escapes root")
	}
	return nil
}

// isReservedPath reports whether a user-supplied destination path lives
// inside Eta's hidden staging/intents directory.
func isReservedPath(p string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(p))
	if cleaned == stagingDirName {
		return true
	}
	return strings.HasPrefix(cleaned, stagingDirName+"/")
}

func newTreeID() (string, error) {
	var raw [12]byte
	_, err := rand.Read(raw[:])
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
