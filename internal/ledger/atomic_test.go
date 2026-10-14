package ledger

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestSaveRejectsStaleSnapshot(t *testing.T) {
	repo := t.TempDir()
	if err := Save(repo, &Ledger{}); err != nil {
		t.Fatal(err)
	}
	a, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	a.Rows = append(a.Rows, Row{ID: "A-1", Rule: "A holds.", EnforcedBy: "-", Check: "NONE", RedProof: "-", Hash: "-"})
	if err := Save(repo, a); err != nil {
		t.Fatal(err)
	}
	b.Rows = append(b.Rows, Row{ID: "B-1", Rule: "B holds.", EnforcedBy: "-", Check: "NONE", RedProof: "-", Hash: "-"})
	if err := Save(repo, b); err == nil {
		t.Fatal("stale writer accepted")
	}
	data, err := os.ReadFile(filepath.Join(repo, Filename))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "A-1") || strings.Contains(string(data), "B-1") {
		t.Fatal("stale writer lost accepted rows")
	}
}

func TestReplacementFailureSemantics(t *testing.T) {
	for _, phase := range []string{"file-sync", "rename", "directory-sync", "concurrent-writer"} {
		t.Run(phase, func(t *testing.T) {
			repo := t.TempDir()
			old := []byte(Serialize(&Ledger{}))
			next := []byte(Serialize(&Ledger{Floor: 1, Rows: []Row{{ID: "R-1", Rule: "A holds.", EnforcedBy: "-", Check: "NONE", RedProof: "-", Hash: "-"}}}))
			if err := ReplaceExpected(repo, nil, old); err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected failure")
			ops := replacementIO{syncFile: (*os.File).Sync, rename: os.Rename, syncDir: (*os.File).Sync}
			switch phase {
			case "file-sync":
				ops.syncFile = func(*os.File) error { return injected }
			case "rename":
				ops.rename = func(string, string) error { return injected }
			case "directory-sync":
				ops.syncDir = func(*os.File) error { return injected }
			case "concurrent-writer":
				ops.syncFile = func(f *os.File) error {
					if err := os.WriteFile(Path(repo), []byte("foreign whole bytes"), 0o644); err != nil {
						return err
					}
					return f.Sync()
				}
			}
			err := replaceExpected(repo, old, next, ops)
			if err == nil {
				t.Fatal("failed operation reported success")
			}
			var failure *ReplacementError
			if !errors.As(err, &failure) {
				t.Fatalf("missing failure disposition: %v", err)
			}
			if failure.Installed != (phase == "directory-sync") {
				t.Fatalf("wrong install disposition: %+v", failure)
			}
			actual, err := os.ReadFile(Path(repo))
			if err != nil {
				t.Fatal(err)
			}
			want := old
			if phase == "directory-sync" {
				want = next
			}
			if phase == "concurrent-writer" {
				want = []byte("foreign whole bytes")
			}
			if !bytes.Equal(actual, want) {
				t.Fatal("failed write changed unexpected bytes")
			}
			// Retain the assertion above, then prove the production writer works.
			if err := ReplaceExpected(repo, actual, next); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReplacementOccupiedAndCreateOnly(t *testing.T) {
	repo := t.TempDir()
	dir, err := os.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceExpected(repo, nil, []byte("whole")); err == nil {
		t.Fatal("occupied writer accepted")
	}
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceExpected(repo, nil, []byte("whole")); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceExpected(repo, nil, []byte("other")); err == nil {
		t.Fatal("create-only overwrote existing bytes")
	}
}
