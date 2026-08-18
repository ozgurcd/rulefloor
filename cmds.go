package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func cmdInit(repo string, stdout io.Writer) error {
	p := ledgerPath(repo)
	if _, err := os.Stat(p); err == nil {
		return failf("refusing: %s already exists", p)
	}
	l := &Ledger{Floor: 0, HasRedProofs: true}
	if err := saveLedger(repo, l); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "initialized %s (FLOOR: 0, RED-PROOFS: 0)\n", p)
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
	l.maintainRedProofs()
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
	// The red-proof is a first-class obligation: arming a check nobody
	// has watched FAIL is the vacuity this ledger exists to prevent
	// (SA-ORG-COPY-1 was armed, green, and false).
	if redProof == "" {
		return fatalf("arm: --red-proof is required — describe the watched failure (arming an unproved check is refused)")
	}
	if redProof == "-" {
		return fatalf("arm: --red-proof must be a real proof, not the \"-\" placeholder")
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
	if err := refuseSkips(ref, profile); err != nil {
		return err
	}
	if err := validCell(redProof, "red-proof"); err != nil {
		return err
	}
	r.RedProof = redProof
	r.EnforcedBy = kind
	r.Check = file + " @ " + profile
	r.Hash = ref.hash()
	l.raiseFloor(len(l.Rows))
	l.maintainRedProofs()
	if err := saveLedger(repo, l); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "armed %s: %s (hash %s); FLOOR %d, RED-PROOFS %d\n", id, r.Check, r.Hash, l.Floor, l.RedProofs)
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
	file, profile, err := splitCheck(r.Check)
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
	if err := refuseSkips(ref, profile); err != nil {
		return err
	}
	h := ref.hash()
	if h == r.Hash {
		return failf("refusing: no-op rehash for %s (hash unchanged: %s)", id, h)
	}
	old := r.Hash
	r.Hash = h
	l.maintainRedProofs()
	if err := saveLedger(repo, l); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "rehashed %s: %s -> %s\n", id, old, h)
	return nil
}

// cmdUnproved lists armed rows still carrying the "-" red-proof
// placeholder — the historical debt the RED-PROOFS ratchet refuses to
// let grow. Read-only.
func cmdUnproved(repo string, stdout io.Writer) error {
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	armed, n := 0, 0
	for _, r := range l.Rows {
		if !r.armed() {
			continue
		}
		armed++
		if r.RedProof == "-" {
			fmt.Fprintf(stdout, "%-24s %s\n", r.ID, r.Rule)
			n++
		}
	}
	fmt.Fprintf(stdout, "unproved: %d of %d armed rows carry no red-proof\n", n, armed)
	return nil
}

// dateInProofRe matches an ISO date (YYYY-MM-DD) anywhere in a cell.
// A genuine watched proof is dated; a placeholder or a pre-arming note
// is not.
var dateInProofRe = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

// looksLikeRealProof reports whether a red-proof cell is a genuine,
// dated watched-failure record rather than a placeholder the tool
// itself can see is a non-proof. A non-proof is: the "-" placeholder,
// a cell starting with "blocked:" (a pre-arming block note), or any
// cell carrying no ISO date. `prove --replace` may overwrite ONLY a
// non-proof; it refuses to clobber a real proof.
func looksLikeRealProof(cell string) bool {
	c := strings.TrimSpace(cell)
	if c == "-" || c == "" {
		return false
	}
	if strings.HasPrefix(c, "blocked:") {
		return false
	}
	return dateInProofRe.MatchString(c)
}

// cmdProve records a watched red-proof on an ALREADY-ARMED row — the
// burndown path for the pre-ratchet "-" debt. It refuses to touch a row
// that already carries a proof: recorded history is never silently
// replaced. With replace=true it additionally overwrites a cell the
// tool can SEE is a non-proof (a "blocked:" pre-arming note or a
// dateless cell) — never a genuine dated proof. Raises RED-PROOFS
// through the same maintain path as arm.
func cmdProve(repo, id, redProof string, replace bool, stdout io.Writer) error {
	if redProof == "" {
		return fatalf("prove: --red-proof is required — describe the watched failure")
	}
	if redProof == "-" {
		return fatalf("prove: --red-proof must be a real proof, not the \"-\" placeholder")
	}
	if err := validCell(redProof, "red-proof"); err != nil {
		return err
	}
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	r := l.find(id)
	if r == nil {
		return failf("no rule %s", id)
	}
	if !r.armed() {
		return failf("refusing: rule %s is not armed (arm records its proof itself)", id)
	}
	switch {
	case r.RedProof == "-":
		// The ordinary burndown path: an unproved "-" cell.
	case replace && !looksLikeRealProof(r.RedProof):
		// --replace narrowly overwrites a tool-visible non-proof (a
		// "blocked:" pre-arming note or a dateless cell). Refuses a
		// genuine dated proof below.
	case replace:
		return failf("refusing: rule %s carries a genuine dated red-proof — --replace only overwrites a non-proof (\"-\", \"blocked:…\", or a dateless cell)", id)
	default:
		return failf("refusing: rule %s already carries a red-proof (recorded history is not replaced; use --replace only for a pre-arming/dateless non-proof)", id)
	}
	old := r.RedProof
	r.RedProof = redProof
	l.maintainRedProofs()
	if err := saveLedger(repo, l); err != nil {
		return err
	}
	if replace && old != "-" {
		fmt.Fprintf(stdout, "proved %s (replaced non-proof %q); RED-PROOFS %d\n", id, old, l.RedProofs)
	} else {
		fmt.Fprintf(stdout, "proved %s; RED-PROOFS %d\n", id, l.RedProofs)
	}
	return nil
}

// cmdRedProofs prints the ratchet state; with --adopt it writes the
// RED-PROOFS header onto a legacy ledger at the MEASURED current count —
// adoption never invents history ("-" rows stay unproved).
func cmdRedProofs(repo string, adopt bool, stdout io.Writer) error {
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	m := l.measuredRedProofs()
	if adopt {
		if l.HasRedProofs {
			return failf("refusing: RED-PROOFS header already present (%d)", l.RedProofs)
		}
		l.RedProofs = m
		l.HasRedProofs = true
		if err := saveLedger(repo, l); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "adopted RED-PROOFS: %d (measured; \"-\" rows stay unproved)\n", m)
		return nil
	}
	if !l.HasRedProofs {
		fmt.Fprintf(stdout, "RED-PROOFS header ABSENT (legacy ledger); measured %d — adopt with: rulefloor redproofs --adopt\n", m)
		return nil
	}
	fmt.Fprintf(stdout, "RED-PROOFS %d, measured %d\n", l.RedProofs, m)
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

// refuseSkips rejects skippable proofs. Go rows on a non-"unit" profile are
// exempt from the t.Skip refusal: such tests legitimately guard on their
// environment, are STATIC-ONLY in plain check, and an actual runtime skip
// under `check --run-profile` is CANNOT-EVALUATE there.
func refuseSkips(ref *testRef, profile string) error {
	if ref.Modifier == "skip" || ref.Modifier == "only" {
		return failf("refusing: tagged test uses .%s", ref.Modifier)
	}
	if ref.GoSkips && ref.Kind == kindGoTest && profile == "unit" {
		return failf("refusing: tagged Go test calls t.Skip")
	}
	return nil
}
