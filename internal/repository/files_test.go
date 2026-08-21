package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfinedFilesRejectRepositoryEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "escaped.txt")); err != nil {
		t.Fatal(err)
	}

	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfinedRegularFile(canonical, "../outside.txt"); err == nil {
		t.Fatal("parent traversal was accepted")
	}
	if _, err := ConfinedRegularFile(canonical, "escaped.txt"); err == nil {
		t.Fatal("external symlink was accepted for reading")
	}
	if _, err := ConfinedWritePath(canonical, "escaped.txt"); err == nil {
		t.Fatal("external symlink was accepted for writing")
	}
}

func TestConfinedFilesAllowRepositoryLocalTargets(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(root, "linked.txt")); err != nil {
		t.Fatal(err)
	}

	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConfinedRegularFile(canonical, "linked.txt"); err != nil {
		t.Fatalf("repository-local symlink: %v", err)
	}
	path, err := ConfinedWritePath(canonical, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(canonical, "new.txt") {
		t.Fatalf("write path = %q", path)
	}
}
