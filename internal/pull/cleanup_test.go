package pull

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveOldPathPrunesEmptyParentDirectories(t *testing.T) {
	root := t.TempDir()
	oldRel := filepath.Join("course", "material", "file.pdf")
	newRel := filepath.Join("course", "group", "material", "file.pdf")

	oldPath := filepath.Join(root, oldRel)
	newPath := filepath.Join(root, newRel)
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}

	removeOldPath(root, oldRel, newRel)

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "course", "material")); !os.IsNotExist(err) {
		t.Fatalf("empty old material directory was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "course")); err != nil {
		t.Fatalf("course directory containing relocated file should remain: %v", err)
	}
}

func TestRemoveOldPathKeepsNonEmptyParentDirectory(t *testing.T) {
	root := t.TempDir()
	oldRel := filepath.Join("course", "material", "file.pdf")
	newRel := filepath.Join("course", "group", "material", "file.pdf")
	oldDir := filepath.Join(root, "course", "material")

	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, oldRel), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "keep.txt"), []byte("user file"), 0o644); err != nil {
		t.Fatal(err)
	}

	removeOldPath(root, oldRel, newRel)

	if _, err := os.Stat(oldDir); err != nil {
		t.Fatalf("non-empty old directory should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(oldDir, "keep.txt")); err != nil {
		t.Fatalf("unmanaged file should remain: %v", err)
	}
}

func TestRemoveOldPathRefusesPathOutsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	outside := filepath.Join(parent, "outside", "file.pdf")
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	removeOldPath(root, filepath.Join("..", "outside", "file.pdf"), filepath.Join("course", "file.pdf"))

	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("path outside root must not be removed: %v", err)
	}
}
