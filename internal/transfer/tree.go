package transfer

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

// SendTreeWithProgress creates each destination directory before delivering
// verified files. Each file remains individually resumable and atomically
// finalized; callers must not treat the destination tree as complete until the
// function returns successfully.
func SendTreeWithProgress(ctx context.Context, client *http.Client, baseURL string, root int, destination, source string, tree Tree, progress func(completed, total int)) error {
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
