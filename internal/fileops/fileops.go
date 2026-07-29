// Package fileops contains the narrowly scoped local mutations Eta permits.
package fileops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Root struct{ path string }

func New(root string) (Root, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Root{}, err
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return Root{}, err
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return Root{}, fmt.Errorf("root is not a directory")
	}
	return Root{path: real}, nil
}

// Rename moves a local entry within its configured root. Existing targets are
// rejected rather than overwritten.
func (r Root) Rename(from, to string) error {
	source, err := r.target(from)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(source); err != nil {
		return err
	}
	destination, err := r.destination(to)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}

// Delete removes a local file, symlink, or directory tree within its root.
func (r Root) Delete(relative string) error {
	target, err := r.target(relative)
	if err != nil {
		return err
	}
	if target == r.path {
		return errors.New("cannot delete configured root")
	}
	return os.RemoveAll(target)
}

func (r Root) target(relative string) (string, error) {
	candidate, err := r.lexical(relative)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if !within(r.path, real) {
		return "", errors.New("path escapes configured root")
	}
	return real, nil
}
func (r Root) destination(relative string) (string, error) {
	candidate, err := r.lexical(relative)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(candidate))
	if err != nil {
		return "", err
	}
	if !within(r.path, parent) {
		return "", errors.New("destination escapes configured root")
	}
	return candidate, nil
}
func (r Root) lexical(relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("invalid relative path")
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes configured root")
	}
	return filepath.Join(r.path, clean), nil
}
func within(root, target string) bool {
	return target == root || strings.HasPrefix(target, root+string(filepath.Separator))
}
