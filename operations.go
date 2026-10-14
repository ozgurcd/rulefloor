package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/ozgurcd/rulefloor/internal/ledger"
	"github.com/ozgurcd/rulefloor/internal/repository"
)

const operationSchema = "rulefloor.operations.v1"

// Operations are review inputs, not authenticated grants or accepted evidence.
// Callers must supply the sealed identities again when validating/applying.
type operationSet struct {
	Schema         string           `json:"schema_version"`
	OriginalID     string           `json:"original_identity"`
	OriginalLedger string           `json:"original_ledger"`
	OriginalSHA    string           `json:"original_sha256"`
	SourceID       string           `json:"source_identity"`
	ReviewID       string           `json:"review_identity"`
	Authority      string           `json:"authority"`
	Operations     []floorOperation `json:"operations"`
}

type floorOperation struct {
	ID              string          `json:"rule_id"`
	Kind            string          `json:"kind"`
	Old             *ledger.Row     `json:"expected_old"`
	New             ledger.Row      `json:"new"`
	OldFingerprints rowFingerprints `json:"old_fingerprints"`
	NewFingerprints rowFingerprints `json:"new_fingerprints"`
	CheckSHA        string          `json:"check_file_sha256"`
}

type rowFingerprints struct {
	Sentence string `json:"sentence_sha256"`
	Test     string `json:"test_fingerprint"`
	Proof    string `json:"proof_sha256"`
	Binding  string `json:"binding_sha256"`
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fingerprints(row *ledger.Row) rowFingerprints {
	if row == nil {
		return rowFingerprints{}
	}
	binding := *row
	binding.Rule, binding.RedProof, binding.Hash = "", "", ""
	data, _ := json.Marshal(binding, json.Deterministic(true))
	return rowFingerprints{digest([]byte(row.Rule)), row.Hash, digest([]byte(row.RedProof)), digest(data)}
}

func logicalRowEqual(a, b *ledger.Row) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	x, _ := json.Marshal(a, json.Deterministic(true))
	y, _ := json.Marshal(b, json.Deterministic(true))
	return bytes.Equal(x, y)
}

func operationFlags(emit bool) map[string]bool {
	flags := map[string]bool{"--repo": true, "--original-id": true, "--source-id": true, "--review-id": true, "--authority": true}
	if emit {
		flags["--baseline"] = true
	} else {
		flags["--operations"] = true
	}
	return flags
}

// Required fields are checked recursively, including explicit null old values.
// json/v2 rejects duplicate names, unknown names, malformed and trailing bytes.
func decodeOperations(data []byte) (operationSet, error) {
	var set operationSet
	if len(data) > 16<<20 {
		return set, fmt.Errorf("operation input exceeds 16 MiB")
	}
	if err := json.Unmarshal(data, &set, json.RejectUnknownMembers(true)); err != nil {
		return set, err
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return set, err
	}
	if err := requiredFields(raw, reflect.TypeFor[operationSet]()); err != nil {
		return set, err
	}
	if set.Schema != operationSchema {
		return set, fmt.Errorf("unsupported operation schema")
	}
	if set.OriginalID == "" || set.SourceID == "" || set.ReviewID == "" || set.Authority == "" {
		return set, fmt.Errorf("missing sealed identity or authority")
	}
	if set.OriginalSHA != digest([]byte(set.OriginalLedger)) {
		return set, fmt.Errorf("original ledger fingerprint mismatch")
	}
	if len(set.Operations) == 0 {
		return set, fmt.Errorf("empty/no-op operation set")
	}
	return set, nil
}

func readOperationInput(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, fmt.Errorf("operation input exceeds 16 MiB")
	}
	return data, nil
}

func requiredFields(raw any, t reflect.Type) error {
	if t.Kind() == reflect.Pointer {
		if raw == nil {
			return nil
		}
		return requiredFields(raw, t.Elem())
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("required object missing")
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := f.Tag.Get("json")
			if name == "" {
				name = f.Name
			}
			value, ok := m[name]
			if !ok {
				return fmt.Errorf("missing field %s", name)
			}
			if err := requiredFields(value, f.Type); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	case reflect.Slice:
		values, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("required array missing or null")
		}
		for _, value := range values {
			if err := requiredFields(value, t.Elem()); err != nil {
				return err
			}
		}
	default:
		if raw == nil {
			return fmt.Errorf("required value is null")
		}
	}
	return nil
}

func operationKind(old *ledger.Row, next ledger.Row) (string, error) {
	if !ledger.ValidID(next.ID) {
		return "", fmt.Errorf("invalid rule ID")
	}
	if old == nil {
		if next.Armed() || next.EnforcedBy != "-" || next.Check != "NONE" || next.Hash != "-" {
			return "", fmt.Errorf("addition must be declared first")
		}
		return "declare", nil
	}
	if old.ID != next.ID {
		return "", fmt.Errorf("rule identity changed")
	}
	if logicalRowEqual(old, &next) {
		return "", fmt.Errorf("no-op operation")
	}
	if !old.Armed() && next.Armed() {
		if old.Rule != next.Rule {
			return "", fmt.Errorf("arm cannot change sentence")
		}
		proof, err := ledger.ParseProof(next.RedProof)
		if err != nil || !proof.Present() {
			return "", fmt.Errorf("arm requires proof")
		}
		return "arm", nil
	}
	copy := next
	copy.Rule, copy.CoveredSymbols, copy.ReachabilityPolicy, copy.TestSymbol, copy.ExecutionPolicy = old.Rule, old.CoveredSymbols, old.ReachabilityPolicy, old.TestSymbol, old.ExecutionPolicy
	if logicalRowEqual(old, &copy) {
		return "amend", nil
	}
	copy = next
	copy.Hash, copy.TestSymbol = old.Hash, old.TestSymbol
	if old.Armed() && old.Hash != next.Hash && logicalRowEqual(old, &copy) {
		return "rehash", nil
	}
	copy = next
	copy.RedProof = old.RedProof
	if old.Armed() && logicalRowEqual(old, &copy) {
		previous, err := ledger.ParseProof(old.RedProof)
		if err != nil {
			return "", err
		}
		proof, err := ledger.ParseProof(next.RedProof)
		if err != nil || !proof.Present() {
			return "", fmt.Errorf("proof deletion refused")
		}
		if previous.GenuineRecord() {
			if !proof.GenuineRecord() || proof.SupersedesFingerprint != previous.Fingerprint() {
				return "", fmt.Errorf("protected proof requires exact supersession")
			}
		} else if proof.SupersedesFingerprint != "" {
			return "", fmt.Errorf("cannot supersede non-proof")
		}
		return "prove", nil
	}
	return "", fmt.Errorf("incompatible binding/proof changes or unsupported deletion")
}

func validOperationLedger(l *ledger.Ledger) error {
	parsed, err := ledger.Parse(ledger.Serialize(l))
	if err != nil {
		return err
	}
	if len(parsed.Rows) != len(l.Rows) {
		return fmt.Errorf("noncanonical or invalid row values")
	}
	for i := range l.Rows {
		if !logicalRowEqual(&parsed.Rows[i], &l.Rows[i]) {
			return fmt.Errorf("noncanonical or invalid row values")
		}
	}
	if l.EffectiveCount() < l.Floor {
		return fmt.Errorf("invalid FLOOR: rows below ratchet")
	}
	if l.HasRedProofs && l.MeasuredRedProofs() < l.RedProofs {
		return fmt.Errorf("invalid RED-PROOFS: proofs below ratchet")
	}
	return nil
}

func checkFileDigest(repo string, row *ledger.Row) (string, error) {
	if !row.Armed() {
		return "", nil
	}
	binding, err := ledger.InterpretBinding(row)
	if err != nil {
		return "", err
	}
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return "", err
	}
	path, err := repository.ConfinedRegularFile(root, binding.File)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

// This is static integrity, never a native accepted gate. It executes no tests.
func validateOperationRows(repo string, l *ledger.Ledger) error {
	for i := range l.Rows {
		r := &l.Rows[i]
		// Operation fields must describe the same binding the CLI would derive.
		// In particular, stable covered symbols cannot opt out of exact reach.
		covered, policy, err := prepareCoveredSymbols("operations", repo, r.CoveredSymbols)
		if err != nil {
			return err
		}
		if policy != r.ReachabilityPolicy || !slices.Equal(covered, r.CoveredSymbols) {
			return fmt.Errorf("changed protected binding for %s", r.ID)
		}
		if (!r.Armed() || policy != ledger.ReachabilityExact) && r.TestSymbol != "" {
			return fmt.Errorf("unexpected protected test symbol for %s", r.ID)
		}
		if !r.Armed() {
			continue
		}
		binding, err := ledger.InterpretBinding(r)
		if err != nil {
			return err
		}
		ref, err := resolveRef(repo, binding.File, r.ID, r.EnforcedBy)
		if err != nil {
			return err
		}
		if err := refuseSkips(ref, binding.Execution); err != nil {
			return err
		}
		kind, err := kindForFile(binding.File)
		if err != nil || kind != r.EnforcedBy {
			return fmt.Errorf("binding kind mismatch for %s", r.ID)
		}
		if ref.hash() != r.Hash {
			return fmt.Errorf("altered test fingerprint for %s", r.ID)
		}
		proof, err := ledger.ParseProof(r.RedProof)
		if err != nil || !proof.Present() {
			return fmt.Errorf("missing/invalid proof for %s", r.ID)
		}
		if r.ReachabilityPolicy == ledger.ReachabilityExact {
			next := Row(*r)
			if err := verifyInitialProtectedReach(repo, &next, ref.FuncName, newGraphClient()); err != nil {
				return err
			}
			if next.TestSymbol != r.TestSymbol {
				return fmt.Errorf("changed protected binding for %s", r.ID)
			}
		}
	}
	return nil
}

func emitOperations(repo, baseline, original, source, review, authority string) (operationSet, error) {
	data, err := readOperationInput(baseline)
	if err != nil {
		return operationSet{}, err
	}
	old, err := ledger.Parse(string(data))
	if err != nil {
		return operationSet{}, err
	}
	next, err := ledger.Load(repo)
	if err != nil {
		return operationSet{}, err
	}
	if err := validOperationLedger(old); err != nil {
		return operationSet{}, err
	}
	if err := validOperationLedger(next); err != nil {
		return operationSet{}, err
	}
	if !slices.Equal(old.RepairedFixtures, next.RepairedFixtures) || next.Floor < old.Floor || (old.HasRedProofs && (!next.HasRedProofs || next.RedProofs < old.RedProofs)) {
		return operationSet{}, fmt.Errorf("repair or lowered ratchet unsupported")
	}
	for _, row := range old.Rows {
		if next.Find(row.ID) == nil {
			return operationSet{}, fmt.Errorf("unsupported deletion")
		}
	}
	set := operationSet{operationSchema, original, string(data), digest(data), source, review, authority, []floorOperation{}}
	for _, row := range next.Rows {
		previous := old.Find(row.ID)
		if previous != nil && logicalRowEqual(previous, &row) {
			continue
		}
		kind, err := operationKind(previous, row)
		if err != nil {
			return operationSet{}, fmt.Errorf("%s: %w", row.ID, err)
		}
		check, err := checkFileDigest(repo, &row)
		if err != nil {
			return operationSet{}, err
		}
		set.Operations = append(set.Operations, floorOperation{row.ID, kind, previous, row, fingerprints(previous), fingerprints(&row), check})
	}
	if err := validateOperationRows(repo, next); err != nil {
		return operationSet{}, err
	}
	encoded, err := json.Marshal(set, json.Deterministic(true))
	if err != nil {
		return operationSet{}, err
	}
	return decodeOperations(encoded)
}

func materializeOperations(repo string, set operationSet, original, source, review, authority string) ([]byte, []byte, error) {
	if set.OriginalID != original || set.SourceID != source || set.ReviewID != review || set.Authority != authority {
		return nil, nil, fmt.Errorf("sealed identity/authority mismatch")
	}
	base, err := ledger.Parse(set.OriginalLedger)
	if err != nil {
		return nil, nil, err
	}
	current, err := ledger.Load(repo)
	if err != nil {
		return nil, nil, err
	}
	if err := validOperationLedger(base); err != nil {
		return nil, nil, err
	}
	if err := validOperationLedger(current); err != nil {
		return nil, nil, err
	}
	if !slices.Equal(base.RepairedFixtures, current.RepairedFixtures) || current.Floor < base.Floor || (base.HasRedProofs && (!current.HasRedProofs || current.RedProofs < base.RedProofs)) {
		return nil, nil, fmt.Errorf("stale original ratchet/repair state")
	}
	for _, row := range base.Rows {
		if current.Find(row.ID) == nil {
			return nil, nil, fmt.Errorf("original rule removed")
		}
	}
	expected := bytes.Clone(current.OriginalBytes)
	seen := map[string]bool{}
	for _, op := range set.Operations {
		if seen[op.ID] {
			return nil, nil, fmt.Errorf("duplicate/incompatible operation ID %s", op.ID)
		}
		seen[op.ID] = true
		if op.ID != op.New.ID || !logicalRowEqual(base.Find(op.ID), op.Old) || !logicalRowEqual(current.Find(op.ID), op.Old) {
			return nil, nil, fmt.Errorf("stale expected-old rule %s", op.ID)
		}
		if op.OldFingerprints != fingerprints(op.Old) || op.NewFingerprints != fingerprints(&op.New) {
			return nil, nil, fmt.Errorf("altered fingerprints for %s", op.ID)
		}
		kind, err := operationKind(op.Old, op.New)
		if err != nil || kind != op.Kind {
			return nil, nil, fmt.Errorf("invalid operation kind for %s", op.ID)
		}
		check, err := checkFileDigest(repo, &op.New)
		if err != nil || check != op.CheckSHA {
			return nil, nil, fmt.Errorf("changed check source for %s", op.ID)
		}
		if row := current.Find(op.ID); row != nil {
			*row = op.New
		} else {
			if current.IsRepairedFixture(op.ID) {
				return nil, nil, fmt.Errorf("repaired fixture ID reused")
			}
			current.Rows = append(current.Rows, op.New)
		}
	}
	current.RaiseFloor(current.EffectiveCount())
	current.MaintainRedProofs()
	if err := validOperationLedger(current); err != nil {
		return nil, nil, err
	}
	if err := validateOperationRows(repo, current); err != nil {
		return nil, nil, err
	}
	next := renderOperationBytes(expected, current, seen)
	parsed, err := ledger.Parse(string(next))
	if err != nil {
		return nil, nil, fmt.Errorf("rendered ledger invalid: %w", err)
	}
	if parsed.Floor != current.Floor || parsed.RedProofs != current.RedProofs || parsed.HasRedProofs != current.HasRedProofs || len(parsed.Rows) != len(current.Rows) || !slices.Equal(parsed.RepairedFixtures, current.RepairedFixtures) {
		return nil, nil, fmt.Errorf("rendered ledger differs from validated state")
	}
	for i := range current.Rows {
		if !logicalRowEqual(parsed.Find(current.Rows[i].ID), &current.Rows[i]) {
			return nil, nil, fmt.Errorf("rendered row differs from validated state")
		}
	}
	return expected, next, nil
}

// Untouched lines retain their exact bytes; only changed row/header lines are
// replaced. New IDs are inserted in lexical order so disjoint additions commute.
func renderOperationBytes(old []byte, l *ledger.Ledger, changed map[string]bool) []byte {
	lines := strings.SplitAfter(string(old), "\n")
	rowLines := map[string]string{}
	for _, r := range l.Rows {
		one := &ledger.Ledger{Rows: []ledger.Row{r}}
		serialized := strings.Split(ledger.Serialize(one), "\n")
		rowLines[r.ID] = serialized[len(serialized)-2] + "\n"
	}
	remaining := map[string]bool{}
	for id := range changed {
		remaining[id] = true
	}
	var out strings.Builder
	prior, _ := ledger.Parse(string(old))
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "FLOOR: "):
			if trim == fmt.Sprintf("FLOOR: %d", l.Floor) {
				out.WriteString(line)
			} else {
				fmt.Fprintf(&out, "FLOOR: %d\n", l.Floor)
			}
			if !prior.HasRedProofs && l.HasRedProofs {
				fmt.Fprintf(&out, "RED-PROOFS: %d\n", l.RedProofs)
			}
		case strings.HasPrefix(trim, "RED-PROOFS: "):
			if trim == fmt.Sprintf("RED-PROOFS: %d", l.RedProofs) {
				out.WriteString(line)
			} else {
				fmt.Fprintf(&out, "RED-PROOFS: %d\n", l.RedProofs)
			}
		default:
			cells := strings.Split(trim, "|")
			id := ""
			if len(cells) > 2 {
				id = strings.TrimSpace(cells[1])
			}
			if ledger.ValidID(id) && changed[id] {
				out.WriteString(rowLines[id])
				delete(remaining, id)
			} else {
				out.WriteString(line)
			}
		}
	}
	data := out.String()
	ids := make([]string, 0, len(remaining))
	for id := range remaining {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		parts := strings.SplitAfter(data, "\n")
		insert := len(parts)
		for i, line := range parts {
			cells := strings.Split(strings.TrimSpace(line), "|")
			if len(cells) > 2 {
				other := strings.TrimSpace(cells[1])
				if ledger.ValidID(other) && other > id {
					insert = i
					break
				}
			}
		}
		prefix := strings.Join(parts[:insert], "")
		if prefix != "" && !strings.HasSuffix(prefix, "\n") {
			prefix += "\n"
		}
		data = prefix + rowLines[id] + strings.Join(parts[insert:], "")
	}
	return []byte(data)
}

func cmdOperations(c commandInvocation, verb string) error {
	for _, flag := range []string{"--original-id", "--source-id", "--review-id", "--authority"} {
		if c.flags[flag] == "" {
			return fatalf("%s requires %s", verb, flag)
		}
	}
	original, source, review, authority := c.flags["--original-id"], c.flags["--source-id"], c.flags["--review-id"], c.flags["--authority"]
	if verb == "ops-emit" {
		if c.flags["--baseline"] == "" {
			return fatalf("ops-emit requires --baseline")
		}
		set, err := emitOperations(c.repo, c.flags["--baseline"], original, source, review, authority)
		if err != nil {
			return failf("operation emission refused: %v", err)
		}
		data, err := json.Marshal(set, json.Deterministic(true))
		if err != nil {
			return err
		}
		_, err = c.stdout.Write(append(data, '\n'))
		return err
	}
	if c.flags["--operations"] == "" {
		return fatalf("%s requires --operations", verb)
	}
	data, err := readOperationInput(c.flags["--operations"])
	if err != nil {
		return fatalf("read operations: %v", err)
	}
	set, err := decodeOperations(data)
	if err != nil {
		return fatalf("invalid operations: %v", err)
	}
	expected, next, err := materializeOperations(c.repo, set, original, source, review, authority)
	if err != nil {
		return failf("operations refused: %v", err)
	}
	switch verb {
	case "ops-apply":
		if err := ledger.ReplaceExpected(c.repo, expected, next); err != nil {
			return failf("operations refused: %v", err)
		}
		fmt.Fprintln(c.stdout, "operations applied; fragments retained; native gate/review still required")
	case "ops-overlay":
		return writeJSON(c.stdout, struct {
			Schema     string `json:"schema_version"`
			Diagnostic bool   `json:"diagnostic_only"`
			Accepted   bool   `json:"accepted_evidence"`
			Ledger     string `json:"ledger"`
		}{"rulefloor.overlay.v1", true, false, string(next)})
	default:
		fmt.Fprintln(c.stdout, "operations valid (static diagnostic; not accepted evidence)")
	}
	return nil
}
