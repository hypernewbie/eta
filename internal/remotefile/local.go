package remotefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalSource is the baseline Source implementation used by a serving Eta host.
// It provides the same containment boundary a future peer transport must honor.
type LocalSource struct{ root string }

func NewLocalSource(root string) (*LocalSource, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("source root is not a directory")
	}
	return &LocalSource{root: real}, nil
}
func (s *LocalSource) Stat(_ context.Context, path string) (Info, error) {
	target, err := s.target(path)
	if err != nil {
		return Info{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return Info{}, err
	}
	if !info.Mode().IsRegular() {
		return Info{}, fmt.Errorf("source is not a regular file")
	}
	return Info{Size: info.Size(), Modified: info.ModTime(), Version: fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())}, nil
}
func (s *LocalSource) OpenRange(ctx context.Context, path string, offset, length int64) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if offset < 0 || length < 0 {
		return nil, fmt.Errorf("invalid range")
	}
	target, err := s.target(path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("source is not a regular file")
	}
	if offset > info.Size() {
		file.Close()
		return nil, fmt.Errorf("range starts beyond file")
	}
	return &rangeFile{SectionReader: io.NewSectionReader(file, offset, min(length, info.Size()-offset)), file: file}, nil
}

type rangeFile struct {
	*io.SectionReader
	file *os.File
}

func (r *rangeFile) Close() error { return r.file.Close() }
func (s *LocalSource) target(path string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes source root")
	}
	real, err := filepath.EvalSymlinks(filepath.Join(s.root, clean))
	if err != nil {
		return "", err
	}
	if real != s.root && !strings.HasPrefix(real, s.root+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes source root")
	}
	return real, nil
}
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
