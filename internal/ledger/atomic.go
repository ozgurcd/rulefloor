package ledger

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ozgurcd/rulefloor/internal/repository"
)

// ReplacementError distinguishes an unchanged ledger from a whole replacement
// whose directory sync failed. Installed errors must be settled by reading the
// file; blind retries and rolling back a concurrent accepted update are unsafe.
type ReplacementError struct {
	Phase     string
	Installed bool
	Err       error
}

func (e *ReplacementError) Error() string {
	return fmt.Sprintf("ledger replacement %s failed (installed=%t): %v", e.Phase, e.Installed, e.Err)
}
func (e *ReplacementError) Unwrap() error { return e.Err }

type replacementIO struct {
	syncFile func(*os.File) error
	rename   func(string, string) error
	syncDir  func(*os.File) error
}

// ReplaceExpected serializes cooperating writers with an advisory lock on the
// repository directory (a stable inode across ledger renames). Temp bytes are
// synced before rename; the directory is synced before success. Failed temp
// files are retained for diagnosis. No source fragment is consumed.
// Noncooperating writers and hostile directory substitution require external
// mediation; POSIX rename offers no uncooperative compare-and-swap primitive.
func ReplaceExpected(repo string, expected, next []byte) error {
	return replaceExpected(repo, expected, next, replacementIO{
		syncFile: (*os.File).Sync, rename: os.Rename, syncDir: (*os.File).Sync,
	})
}

func replaceExpected(repo string, expected, next []byte, io replacementIO) error {
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	// Nonblocking: an occupied writer parks this call instead of waiting.
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("ledger writer occupied: %w", err)
	}
	defer syscall.Flock(int(dir.Fd()), syscall.LOCK_UN)
	path, err := repository.ConfinedWritePath(root, Filename)
	if err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if expected == nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("expected absent ledger; refusing replacement")
		}
	} else if err != nil || !bytes.Equal(old, expected) {
		return fmt.Errorf("stale expected-old ledger; refusing replacement")
	}
	mode := os.FileMode(0o644)
	if expected != nil {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		mode = info.Mode().Perm()
	}
	f, err := os.CreateTemp(root, ".rulefloor-pending-*")
	if err != nil {
		return err
	}
	defer f.Close()
	fail := func(phase string, installed bool, err error) error {
		return &ReplacementError{Phase: phase, Installed: installed, Err: err}
	}
	if err := f.Chmod(mode); err != nil {
		return fail("mode", false, err)
	}
	if _, err := f.Write(next); err != nil {
		return fail("write", false, err)
	}
	if err := io.syncFile(f); err != nil {
		return fail("file-sync", false, err)
	}
	if err := f.Close(); err != nil {
		return fail("close", false, err)
	}
	// Re-resolve and compare immediately before publication too.
	resolved, err := repository.ConfinedWritePath(root, Filename)
	if err != nil || resolved != filepath.Join(root, Filename) {
		return fail("path", false, fmt.Errorf("ledger path changed"))
	}
	current, readErr := os.ReadFile(resolved)
	if (expected == nil && !os.IsNotExist(readErr)) || (expected != nil && (readErr != nil || !bytes.Equal(current, expected))) {
		return fail("expected-old", false, fmt.Errorf("stale expected-old ledger"))
	}
	if err := io.rename(f.Name(), resolved); err != nil {
		return fail("rename", false, err)
	}
	if err := io.syncDir(dir); err != nil {
		return fail("directory-sync", true, err)
	}
	return nil
}
