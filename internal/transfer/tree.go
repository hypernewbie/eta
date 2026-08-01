package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Tree is a deterministic, symlink-free description of a source directory.
// Paths are slash-separated and relative to the source directory.
type Tree struct {
	Directories []string
	Files       []TreeFile
	TotalChunks int
}

type TreeFile struct {
	Path     string
	Manifest Manifest
}

// BuildTree validates a directory tree and records the exact file manifests
// needed for a direct transfer. Symlinks and special files are intentionally
// excluded rather than silently following or copying them.
func BuildTree(source string) (Tree, error) {
	var result Tree
	err := filepath.WalkDir(source, func(path string, item os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("tree contains a symlink")
		}
		portable := filepath.ToSlash(relative)
		if item.IsDir() {
			result.Directories = append(result.Directories, portable)
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("tree contains a non-regular file")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		manifest, err := BuildManifest(file, DefaultChunkSize)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		result.Files = append(result.Files, TreeFile{Path: portable, Manifest: manifest})
		result.TotalChunks += len(manifest.Chunks)
		return nil
	})
	if err != nil {
		return Tree{}, err
	}
	sort.Strings(result.Directories)
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	return result, nil
}

// SendTreeWithProgress delivers a source directory tree to the peer via
// the atomic tree-transfer protocol when the peer supports it
// (/api/transfer-trees). Files are still sent through the existing
// per-file /api/transfers flow against staging paths so chunk-level
// verification and per-file resume reuse is unchanged. On commit the
// receiver performs a single os.Rename of the staging tree to the
// destination, giving POSIX-atomic destination materialization.
//
// The sender is robust against legacy peers: a 404 from
// /api/transfer-trees falls back to the prior per-directory create +
// piecemeal finalize flow so mixed-version fleets degrade gracefully.
func SendTreeWithProgress(ctx context.Context, client *http.Client, baseURL string, root int, destination, source string, tree Tree, progress func(completed, total int)) error {
	id, created, err := createTreeSession(ctx, client, baseURL, root, destination, tree)
	if err != nil {
		return err
	}
	if !created {
		return legacySendTree(ctx, client, baseURL, root, destination, source, tree, progress)
	}
	if err := runTreeTransfer(ctx, client, baseURL, root, id, source, tree, progress); err != nil {
		// Clean up partial staging so the receiver doesn't carry an
		// abandoned session until the sweeper notices. Errors here
		// are intentionally ignored — the underlying failure is what
		// the caller cares about.
		_ = abortTree(ctx, client, baseURL, root, id)
		return err
	}
	return nil
}

// createTreeSession asks the peer to reserve a tree session. Returns
// (id, true, nil) when the peer accepted the new protocol,
// (_, false, nil) when the peer returned 404 and the caller should fall
// back, or (_, _, err) on any other failure.
func createTreeSession(ctx context.Context, client *http.Client, baseURL string, root int, destination string, tree Tree) (string, bool, error) {
	files := make([]map[string]any, 0, len(tree.Files))
	for _, item := range tree.Files {
		files = append(files, map[string]any{
			"path": item.Path,
			"size": item.Manifest.Size,
		})
	}
	payload := map[string]any{
		"root":        root,
		"destination": destination,
		"directories": tree.Directories,
		"files":       files,
	}
	endpoint := strings.TrimSuffix(baseURL, "/") + "/api/transfer-trees"
	var created struct {
		ID string `json:"id"`
	}
	err := requestJSON(ctx, client, http.MethodPost, endpoint, asJSONReader(payload), &created)
	if err == nil {
		if created.ID == "" {
			return "", false, fmt.Errorf("peer did not return tree session id")
		}
		return created.ID, true, nil
	}
	if isPeerEndpointMissing(err) {
		return "", false, nil
	}
	return "", false, err
}

// runTreeTransfer sends every non-complete file under the session's
// staging tree, then commits.
func runTreeTransfer(ctx context.Context, client *http.Client, baseURL string, root int, id, source string, tree Tree, progress func(completed, total int)) error {
	status, err := readTreeStatus(ctx, client, baseURL, root, id)
	if err != nil {
		return err
	}
	completed := 0
	if progress != nil {
		progress(completed, tree.TotalChunks)
	}
	for _, item := range tree.Files {
		if status[item.Path] {
			completed += len(item.Manifest.Chunks)
			if progress != nil {
				progress(completed, tree.TotalChunks)
			}
			continue
		}
		stagingRel := ".eta/staging/" + id + "/" + item.Path
		path := filepath.Join(source, filepath.FromSlash(item.Path))
		if _, err := sendFileWithManifest(ctx, client, baseURL, root, stagingRel, path, item.Manifest, func(fileCompleted, _ int) {
			if progress != nil {
				progress(completed+fileCompleted, tree.TotalChunks)
			}
		}); err != nil {
			return err
		}
		completed += len(item.Manifest.Chunks)
		if progress != nil {
			progress(completed, tree.TotalChunks)
		}
	}
	return commitTree(ctx, client, baseURL, root, id)
}

// legacySendTree is the pre-atomic flow retained for mixed-version
// peers. New code should not introduce callers; it stays so older
// peers continue to work.
func legacySendTree(ctx context.Context, client *http.Client, baseURL string, root int, destination, source string, tree Tree, progress func(completed, total int)) error {
	if err := makeDirectory(ctx, client, baseURL, root, destination); err != nil {
		return err
	}
	for _, directory := range tree.Directories {
		if err := makeDirectory(ctx, client, baseURL, root, filepath.ToSlash(filepath.Join(destination, directory))); err != nil {
			return err
		}
	}
	completed := 0
	if progress != nil {
		progress(completed, tree.TotalChunks)
	}
	for _, item := range tree.Files {
		path := filepath.Join(source, filepath.FromSlash(item.Path))
		if _, err := sendFileWithManifest(ctx, client, baseURL, root, filepath.ToSlash(filepath.Join(destination, item.Path)), path, item.Manifest, func(fileCompleted, _ int) {
			if progress != nil {
				progress(completed+fileCompleted, tree.TotalChunks)
			}
		}); err != nil {
			return err
		}
		completed += len(item.Manifest.Chunks)
	}
	return nil
}

// readTreeStatus queries the per-file complete map so a retry can
// skip files already staged.
func readTreeStatus(ctx context.Context, client *http.Client, baseURL string, root int, id string) (map[string]bool, error) {
	endpoint := strings.TrimSuffix(baseURL, "/") + "/api/transfer-trees/" + id + "?root=" + strconv.Itoa(root)
	var response struct {
		Complete map[string]bool `json:"complete"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, endpoint, nil, &response); err != nil {
		return nil, err
	}
	return response.Complete, nil
}

// commitTree asks the receiver to perform the atomic single-rename.
// Failure here leaves staging in place; the next retry will see the
// same complete files and try again.
func commitTree(ctx context.Context, client *http.Client, baseURL string, root int, id string) error {
	endpoint := strings.TrimSuffix(baseURL, "/") + "/api/transfer-trees/" + id + "/commit?root=" + strconv.Itoa(root)
	return requestJSON(ctx, client, http.MethodPost, endpoint, nil, nil)
}

// abortTree cleans up a partially staged session. Best-effort.
func abortTree(ctx context.Context, client *http.Client, baseURL string, root int, id string) error {
	endpoint := strings.TrimSuffix(baseURL, "/") + "/api/transfer-trees/" + id + "?root=" + strconv.Itoa(root)
	return requestJSON(ctx, client, http.MethodDelete, endpoint, nil, nil)
}

// asJSONReader is a tiny helper around json.Marshal that returns a
// typed *bytes.Reader to keep requestJSON's signature consistent.
func asJSONReader(v any) io.Reader {
	body, err := json.Marshal(v)
	if err != nil {
		return strings.NewReader("")
	}
	return bytes.NewReader(body)
}
