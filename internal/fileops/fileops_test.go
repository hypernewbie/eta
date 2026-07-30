package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyRegularAcrossRoots(t *testing.T) {
	sourcePath, destinationPath := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(sourcePath, "source.txt"), []byte("eta"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := New(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := New(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.CopyRegular(source, "source.txt", "copied.txt"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(destinationPath, "copied.txt"))
	if err != nil || string(body) != "eta" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if err := destination.CopyRegular(source, "source.txt", "copied.txt"); err == nil {
		t.Fatal("overwrite allowed")
	}
}

func TestRenameDeleteAndContainment(t *testing.T) {
	rootPath, outside := t.TempDir(), t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(rootPath, "old.txt"), []byte("x"), 0o600))
	must(os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o600))
	must(os.Symlink(outside, filepath.Join(rootPath, "escape")))
	root, err := New(rootPath)
	must(err)
	must(root.Rename("old.txt", "new.txt"))
	if _, err := os.Stat(filepath.Join(rootPath, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if err := root.Rename("new.txt", "new.txt"); err == nil {
		t.Fatal("overwrite allowed")
	}
	if err := root.Rename("escape/secret.txt", "stolen.txt"); err == nil {
		t.Fatal("symlink escape allowed")
	}
	must(root.Delete("new.txt"))
	if _, err := os.Stat(filepath.Join(rootPath, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("file not deleted")
	}
	if err := root.Delete(".."); err == nil {
		t.Fatal("root escape allowed")
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Fatal("outside changed")
	}
}
