// Package fileops contains the narrowly scoped local mutations Eta permits.
package fileops

import (
	"errors"
	"fmt"
	"io"
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
// Copy copies a contained regular file or complete directory tree into this
// root. Directory copies are assembled in a sibling staging directory and
// atomically promoted only after every entry has been copied.
func (r Root) Copy(from Root, sourceRelative, destinationRelative string) error {
	source, err := from.target(sourceRelative)
	if err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("source is a symlink")
	}
	if info.Mode().IsRegular() {
		return r.CopyRegular(from, sourceRelative, destinationRelative)
	}
	if !info.IsDir() {
		return errors.New("source is not a regular file or directory")
	}
	destination, err := r.destination(destinationRelative)
	if err != nil {
		return err
	}
	if within(source, destination) {
		return errors.New("cannot copy a directory into itself")
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(destination), ".eta-copy-")
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := copyTree(source, staging); err != nil {
		return err
	}
	if err := os.Rename(staging, destination); err != nil {
		return err
	}
	complete = true
	return nil
}

// CopyRegular copies one contained regular file into this root. It refuses
// overwrite, directories, and symlink destinations; callers can therefore use
// it for ordinary Explorer copy/paste without special destination semantics.
func (r Root) CopyRegular(from Root, sourceRelative, destinationRelative string) error {
	source, err := from.target(sourceRelative)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("source is not a regular file")
	}
	destination, err := r.destination(destinationRelative)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	name := output.Name()
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(name)
		}
	}()
	if _, err = io.Copy(output, input); err == nil {
		err = output.Sync()
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Chtimes(destination, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	complete = true
	return nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, item os.DirEntry, err error) error {
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
		target := filepath.Join(destination, relative)
		info, err := item.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("directory contains a symlink")
		}
		if item.IsDir() {
			return os.Mkdir(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return errors.New("directory contains a non-regular file")
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err == nil {
			_, err = io.Copy(output, input)
			if err == nil {
				err = output.Sync()
			}
			if closeErr := output.Close(); err == nil {
				err = closeErr
			}
		}
		closeErr := input.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		return os.Chtimes(target, info.ModTime(), info.ModTime())
	})
}

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
