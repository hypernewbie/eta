package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildTreeIncludesEmptyDirectoriesAndRejectsSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "nested", "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "file.txt"), []byte("eta"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tree, err := BuildTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Directories) != 2 || tree.Directories[0] != "nested" || tree.Directories[1] != "nested/empty" {
		t.Fatalf("directories=%#v", tree.Directories)
	}
	if len(tree.Files) != 2 || tree.Files[0].Path != "empty.txt" || tree.Files[1].Path != "nested/file.txt" {
		t.Fatalf("files=%#v", tree.Files)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildTree(root); err == nil {
		t.Fatal("symlink accepted")
	}
}
