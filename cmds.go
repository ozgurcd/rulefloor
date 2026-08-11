package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func cmdInit(repo string, stdout io.Writer) error {
	p := ledgerPath(repo)
	if _, err := os.Stat(p); err == nil {
		return failf("refusing: %s already exists", p)
	}
	l := &Ledger{Floor: 0}
	if err := saveLedger(repo, l); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "initialized %s (FLOOR: 0)\n", p)
	return nil
}

func cmdList(repo string, stdout io.Writer) error {
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	armed := 0
	for _, r := range l.Rows {
		if r.armed() {
			armed++
		}
	}
	fmt.Fprintf(stdout, "FLOOR %d, %d rows (%d armed)\n", l.Floor, len(l.Rows), armed)
	for _, r := range l.Rows {
		state := "declared"
		if r.armed() {
			state = "armed"
		}
		fmt.Fprintf(stdout, "%-10s %-9s %s\n", r.ID, state, r.Rule)
	}
	return nil
}

func cmdShow(repo, id string, stdout io.Writer) error {
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	r := l.find(id)
	if r == nil {
		return failf("no rule %s", id)
	}
	fmt.Fprintf(stdout, "ID:          %s\n", r.ID)
	fmt.Fprintf(stdout, "rule:        %s\n", r.Rule)
	fmt.Fprintf(stdout, "enforced-by: %s\n", r.EnforcedBy)
	fmt.Fprintf(stdout, "check:       %s\n", r.Check)
	fmt.Fprintf(stdout, "red-proof:   %s\n", r.RedProof)
	fmt.Fprintf(stdout, "hash:        %s\n", r.Hash)
	return nil
}

func cmdUnarmed(repo string, stdout io.Writer) error {
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	n := 0
	for _, r := range l.Rows {
		if !r.armed() {
			fmt.Fprintf(stdout, "%-10s %s\n", r.ID, r.Rule)
			n++
		}
	}
	if n == 0 {
		fmt.Fprintf(stdout, "all %d rules are armed\n", len(l.Rows))
	}
	return nil
}

func cmdDeclare(repo, sentence, id, redProof string, stdout io.Writer) error {
	if id == "" {
		return fatalf("declare: --id is required")
	}
	if !idRe.MatchString(id) {
		return fatalf("declare: invalid ID %q (want uppercase letters/digits/hyphens ending in a digit, e.g. R-1)", id)
	}
	if err := validCell(sentence, "rule sentence"); err != nil {
		return err
	}
	if redProof == "" {
		redProof = "-"
	} else if err := validCell(redProof, "red-proof"); err != nil {
		return err
	}
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	if l.find(id) != nil {
		return failf("refusing: rule %s already exists", id)
	}
	l.Rows = append(l.Rows, Row{ID: id, Rule: sentence, EnforcedBy: "-", Check: "NONE", RedProof: redProof, Hash: "-"})
	l.raiseFloor(len(l.Rows))
	if err := saveLedger(repo, l); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "declared %s (unarmed); %d rows, FLOOR %d\n", id, len(l.Rows), l.Floor)
	return nil
}

func cmdArm(repo, id, checkSpec, redProof string, stdout io.Writer) error {
	if checkSpec == "" {
		return fatalf("arm: --check \"<file> @ <profile>\" is required")
	}
	file, profile, err := splitCheck(checkSpec)
	if err != nil {
		return fatalf("arm: %v", err)
	}
	kind, err := kindForFile(file)
	if err != nil {
		return err
	}
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	r := l.find(id)
	if r == nil {
		return failf("refusing: no rule %s (declare it first)", id)
	}
	if r.armed() {
		return failf("refusing: rule %s is already armed (use rehash to accept a changed body)", id)
	}
	ref, err := resolveRef(repo, file, id, kind)
	if err != nil {
		return err
	}
	if err := refuseSkips(ref); err != nil {
		return err
	}
	if redProof != "" {
		if err := validCell(redProof, "red-proof"); err != nil {
			return err
		}
		r.RedProof = redProof
	}
	r.EnforcedBy = kind
	r.Check = file + " @ " + profile
	r.Hash = ref.hash()
	l.raiseFloor(len(l.Rows))
	if err := saveLedger(repo, l); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "armed %s: %s (hash %s); FLOOR %d\n", id, r.Check, r.Hash, l.Floor)
	return nil
}

func cmdRehash(repo, id string, stdout io.Writer) error {
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	r := l.find(id)
	if r == nil {
		return failf("no rule %s", id)
	}
	if !r.armed() {
		return failf("refusing: rule %s is not armed", id)
	}
	file, _, err := splitCheck(r.Check)
	if err != nil {
		return fatalf("row %s: %v", id, err)
	}
	kind, err := kindForFile(file)
	if err != nil {
		return err
	}
	ref, err := resolveRef(repo, file, id, kind)
	if err != nil {
		return err
	}
	if err := refuseSkips(ref); err != nil {
		return err
	}
	h := ref.hash()
	if h == r.Hash {
		return failf("refusing: no-op rehash for %s (hash unchanged: %s)", id, h)
	}
	old := r.Hash
	r.Hash = h
	if err := saveLedger(repo, l); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "rehashed %s: %s -> %s\n", id, old, h)
	return nil
}

// resolveRef reads the check file and extracts the tagged test.
func resolveRef(repo, file, id, kind string) (*testRef, error) {
	data, err := os.ReadFile(filepath.Join(repo, file))
	if err != nil {
		return nil, failf("cannot read check file %s: %v", file, err)
	}
	return extractTagged(string(data), id, kind)
}

func refuseSkips(ref *testRef) error {
	if ref.Modifier == "skip" || ref.Modifier == "only" {
		return failf("refusing: tagged test uses .%s", ref.Modifier)
	}
	if ref.GoSkips {
		return failf("refusing: tagged Go test calls t.Skip")
	}
	return nil
}
