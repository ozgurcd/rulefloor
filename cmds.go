package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
	"github.com/ozgurcd/rulefloor/internal/ledger"
	"github.com/ozgurcd/rulefloor/internal/repository"
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
		if r.Armed() {
			armed++
		}
	}
	fmt.Fprintf(stdout, "FLOOR %d, %d rows (%d armed)\n", l.Floor, len(l.Rows), armed)
	for _, r := range l.Rows {
		state := "declared"
		if r.Armed() {
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
		if !r.Armed() {
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
	if !ledger.ValidID(id) {
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
	if _, err := ledger.ParseProof(redProof); err != nil {
		return fatalf("declare: invalid red-proof: %v", err)
	}
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	if l.find(id) != nil {
		return failf("refusing: rule %s already exists", id)
	}
	if l.isRepairedFixture(id) {
		return failf("refusing: rule %s is recorded as a repaired historical fixture", id)
	}
	l.Rows = append(l.Rows, Row{ID: id, Rule: sentence, EnforcedBy: "-", Check: "NONE", RedProof: redProof, Hash: "-"})
	l.raiseFloor(l.effectiveCount())
	l.maintainRedProofs()
	if err := saveLedger(repo, l); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "declared %s (unarmed); %d rows, FLOOR %d\n", id, len(l.Rows), l.Floor)
	return nil
}

type proofInput struct {
	Text      string
	Kind      string
	Reference string
}

func cmdArm(repo, id, checkSpec string, proofInput proofInput, stdout io.Writer) error {
	if checkSpec == "" {
		return fatalf("arm: --check \"<file> @ <profile>\" is required")
	}
	// The red-proof is a first-class obligation: arming a check nobody
	// has watched FAIL is the vacuity this ledger exists to prevent
	// (SA-ORG-COPY-1 was armed, green, and false).
	if proofInput.Text == "" {
		return fatalf("arm: --red-proof is required — describe the watched failure (arming an unproved check is refused)")
	}
	if proofInput.Text == "-" {
		return fatalf("arm: --red-proof must be a real proof, not the \"-\" placeholder")
	}
	proofCell, err := buildProofCell("arm", proofInput)
	if err != nil {
		return err
	}
	file, profile, err := splitCheck(checkSpec)
	if err != nil {
		return fatalf("arm: %v", err)
	}
	kind, err := kindForFile(file)
	if err != nil {
		return err
	}
	binding, err := ledger.InterpretBinding(&ledger.Row{EnforcedBy: kind, Check: file + " @ " + profile})
	if err != nil {
		return fatalf("arm: %v", err)
	}
	l, err := loadLedger(repo)
	if err != nil {
		return err
	}
	r := l.find(id)
	if r == nil {
		return failf("refusing: no rule %s (declare it first)", id)
	}
	if r.Armed() {
		return failf("refusing: rule %s is already armed (use rehash to accept a changed body)", id)
	}
	ref, err := resolveRef(repo, file, id, kind)
	if err != nil {
		return err
	}
	if err := refuseSkips(ref, binding.Execution); err != nil {
		return err
	}
	r.RedProof = proofCell
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
	if !r.Armed() {
		return failf("refusing: rule %s is not armed", id)
	}
	binding, err := ledger.InterpretBinding((*ledger.Row)(r))
	if err != nil {
		return fatalf("row %s: %v", id, err)
	}
	file := binding.File
	kind, err := kindForFile(file)
	if err != nil {
		return err
	}
	ref, err := resolveRef(repo, file, id, kind)
	if err != nil {
		return err
	}
	if err := refuseSkips(ref, binding.Execution); err != nil {
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

// cmdRepairFixtureRow is the narrow migration path for historical declared
// rows created only to silence the former lexical Go orphan scanner. It
// records the removed ID so the FLOOR ratchet's effective count never falls.
func cmdRepairFixtureRow(repo, id string, stdout io.Writer) error {
	model, err := loadLedger(repo)
	if err != nil {
		return err
	}
	row := model.find(id)
	if row == nil {
		if (*ledger.Ledger)(model).IsRepairedFixture(id) {
			return failf("refusing: fixture row %s is already repaired", id)
		}
		return failf("refusing: no rule %s", id)
	}
	if row.Armed() {
		return failf("refusing: rule %s is armed; only declared fixture-only rows can be repaired", id)
	}
	if !strings.HasPrefix(row.Rule, "Fixture marker, not a rule:") {
		return failf("refusing: rule %s is not an explicitly recorded fixture-marker row", id)
	}
	discovered, err := checkengine.DiscoverRepository(extractorRegistry, repo)
	if err != nil {
		return fatalf("%v", err)
	}
	for _, located := range discovered {
		if located.Tag.ID == id {
			return failf("refusing: %s is still a real discovered tag in %s", id, located.Path)
		}
	}
	for index := range model.Rows {
		if model.Rows[index].ID == id {
			model.Rows = append(model.Rows[:index], model.Rows[index+1:]...)
			break
		}
	}
	model.RepairedFixtures = append(model.RepairedFixtures, id)
	model.maintainRedProofs()
	if err := saveLedger(repo, model); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "repaired fixture-only row %s; FLOOR %d preserved by REPAIRED-FIXTURES audit record\n", id, model.Floor)
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
		if !r.Armed() {
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

// looksLikeRealProof reports whether a red-proof cell is a genuine,
// dated watched-failure record rather than a placeholder the tool
// itself can see is a non-proof. A non-proof is: the "-" placeholder,
// a cell starting with "blocked:" (a pre-arming block note), or any
// cell carrying no ISO date. `prove --replace` may overwrite ONLY a
// non-proof; it refuses to clobber a real proof.
func looksLikeRealProof(cell string) bool {
	proof, err := ledger.ParseProof(cell)
	return err == nil && proof.GenuineRecord()
}

// cmdProve records a watched red-proof on an ALREADY-ARMED row — the
// burndown path for the pre-ratchet "-" debt. It refuses to touch a row
// that already carries a proof: recorded history is never silently
// replaced. With replace=true it additionally overwrites a cell the
// tool can SEE is a non-proof (a "blocked:" pre-arming note or a
// dateless cell) — never a genuine dated proof. Raises RED-PROOFS
// through the same maintain path as arm.
func cmdProve(repo, id string, proofInput proofInput, replace, force bool, stdout io.Writer) error {
	if proofInput.Text == "" {
		return fatalf("prove: --red-proof is required — describe the watched failure")
	}
	if proofInput.Text == "-" {
		return fatalf("prove: --red-proof must be a real proof, not the \"-\" placeholder")
	}
	proofCell, err := buildProofCell("prove", proofInput)
	if err != nil {
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
	if !r.Armed() {
		return failf("refusing: rule %s is not armed (arm records its proof itself)", id)
	}
	existingProof, err := ledger.ParseProof(r.RedProof)
	if err != nil {
		return fatalf("row %s: invalid red-proof: %v", id, err)
	}
	switch {
	case force:
		// --force overwrites ANY red-proof, including a genuine dated one.
		// It exists for a proof that is REAL but NOT row-specific — e.g.
		// byte-identical text copied across two different rules, where the
		// honest fix is to RE-WATCH each rule's own mutation and record it.
		// --replace cannot reach a dated cell, and cross-row duplicate
		// detection cannot either (once the first row is corrected the second
		// is no longer a duplicate). Loud and explicit; never implicit.
	case r.RedProof == "-":
		// The ordinary burndown path: an unproved "-" cell.
	case replace && !existingProof.GenuineRecord():
		// --replace narrowly overwrites a tool-visible non-proof (a
		// "blocked:" pre-arming note or a dateless cell). Refuses a
		// genuine dated proof below.
	case replace:
		if existingProof.Structured {
			return failf("refusing: rule %s carries a protected structured red-proof record — --replace cannot overwrite an observation record; use --force after re-watching", id)
		}
		return failf("refusing: rule %s carries a genuine dated red-proof — --replace only overwrites a non-proof (\"-\", \"blocked:…\", or a dateless cell); use --force to overwrite a real-but-not-row-specific proof after re-watching", id)
	default:
		return failf("refusing: rule %s already carries a red-proof (recorded history is not replaced; use --replace for a pre-arming/dateless non-proof, or --force after re-watching a real-but-not-row-specific proof)", id)
	}
	old := r.RedProof
	r.RedProof = proofCell
	l.maintainRedProofs()
	if err := saveLedger(repo, l); err != nil {
		return err
	}
	switch {
	case force && old != "-":
		fmt.Fprintf(stdout, "proved %s (FORCED overwrite of prior proof %q); RED-PROOFS %d\n", id, old, l.RedProofs)
	case replace && old != "-":
		fmt.Fprintf(stdout, "proved %s (replaced non-proof %q); RED-PROOFS %d\n", id, old, l.RedProofs)
	default:
		fmt.Fprintf(stdout, "proved %s; RED-PROOFS %d\n", id, l.RedProofs)
	}
	return nil
}

func buildProofCell(command string, input proofInput) (string, error) {
	if input.Kind == "" && input.Reference == "" {
		if err := validCell(input.Text, "red-proof"); err != nil {
			return "", err
		}
		if _, err := ledger.ParseProof(input.Text); err != nil {
			return "", fatalf("%s: invalid red-proof: %v", command, err)
		}
		return input.Text, nil
	}
	if input.Kind == "" {
		return "", fatalf("%s: --proof-ref requires --proof-kind", command)
	}
	proof, err := ledger.NewProof(input.Text, ledger.ProofKind(input.Kind), input.Reference)
	if err != nil {
		return "", fatalf("%s: %v", command, err)
	}
	cell := proof.CanonicalText()
	if err := validCell(cell, "red-proof"); err != nil {
		return "", err
	}
	return cell, nil
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
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return nil, failf("cannot resolve repository: %v", err)
	}
	resolved, err := repository.ConfinedRegularFile(root, file)
	if err != nil {
		return nil, failf("cannot read check file %s: %v", file, err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, failf("cannot read check file %s: %v", file, err)
	}
	return extractTagged(string(data), id, kind)
}

// refuseSkips rejects skippable proofs. Go rows on a non-"unit" profile are
// exempt from the t.Skip refusal: such tests legitimately guard on their
// environment, are STATIC-ONLY in plain check, and an actual runtime skip
// under `check --run-profile` is CANNOT-EVALUATE there.
func refuseSkips(ref *testRef, policy ledger.ExecutionPolicy) error {
	if ref.Modifier == "skip" || ref.Modifier == "only" {
		return failf("refusing: tagged test uses .%s", ref.Modifier)
	}
	if ref.GoSkips && string(ref.Kind) == kindGoTest && policy == ledger.ExecutionExecute {
		return failf("refusing: tagged Go test calls t.Skip")
	}
	return nil
}
