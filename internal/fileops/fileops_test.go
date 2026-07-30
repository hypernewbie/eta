package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTreeAcrossRootsIsAtomicAndRejectsSymlinks(t *testing.T) {
	sourcePath, destinationPath, outside := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(sourcePath, "folder", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourcePath, "folder", "nested", "file.txt"), []byte("eta"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sourcePath, "folder", "empty"), 0o700); err != nil {
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
	if err := destination.Copy(source, "folder", "copied"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(destinationPath, "copied", "nested", "file.txt"))
	if err != nil || string(body) != "eta" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if info, err := os.Stat(filepath.Join(destinationPath, "copied", "empty")); err != nil || !info.IsDir() {
		t.Fatalf("empty directory missing: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(sourcePath, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := destination.Copy(source, "escape", "escaped"); err == nil {
		t.Fatal("symlink source allowed")
	}
	if _, err := os.Stat(filepath.Join(destinationPath, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("failed copy left destination: %v", err)
	}
	if err := destination.Copy(source, "folder", "copied"); err == nil {
		t.Fatal("overwrite allowed")
	}
	if err := source.Copy(source, "folder", "folder/inside"); err == nil {
		t.Fatal("copy into source tree allowed")
	}
}

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
