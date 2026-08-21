package repository

import (
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalRoot resolves a repository path to a real directory.
func CanonicalRoot(repo string) (string, error) {
	if repo == "" {
		repo = "."
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve repository root %s: %w", abs, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("inspect repository root %s: %w", real, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root %s is not a directory", real)
	}
	return filepath.Clean(real), nil
}

// ConfinedRegularFile resolves relative below an already-canonical repository
// root and refuses escapes and non-regular files.
func ConfinedRegularFile(root, relative string) (string, error) {
	if !filepath.IsLocal(relative) {
		return "", fmt.Errorf("path %q must be relative and confined to the repository", relative)
	}
	joined := filepath.Join(root, relative)
	real, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", relative, err)
	}
	if err := requireWithin(root, real, relative); err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", relative, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path %q is not a regular file", relative)
	}
	return real, nil
}

// ConfinedWritePath resolves a repository-local write target. Existing files
// may be symlinks only when their resolved target remains inside the root.
func ConfinedWritePath(root, relative string) (string, error) {
	if !filepath.IsLocal(relative) {
		return "", fmt.Errorf("path %q must be relative and confined to the repository", relative)
	}
	joined := filepath.Join(root, relative)
	if _, err := os.Lstat(joined); err == nil {
		return ConfinedRegularFile(root, relative)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect %s: %w", relative, err)
	}

	parent, err := filepath.EvalSymlinks(filepath.Dir(joined))
	if err != nil {
		return "", fmt.Errorf("resolve parent of %s: %w", relative, err)
	}
	if err := requireWithin(root, parent, relative); err != nil {
		return "", err
	}
	info, err := os.Stat(parent)
	if err != nil {
		return "", fmt.Errorf("inspect parent of %s: %w", relative, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("parent of path %q is not a directory", relative)
	}
	return filepath.Join(parent, filepath.Base(joined)), nil
}

func requireWithin(root, target, original string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil || !filepath.IsLocal(relative) {
		return fmt.Errorf("path %q escapes repository root", original)
	}
	return nil
}
