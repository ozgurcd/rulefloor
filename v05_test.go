package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurcd/rulefloor/internal/ledger"
)

func TestAmendChangesOnlySentenceAndPreservesRatchets(t *testing.T) {
	repo := newPWRepo(t)
	before, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	old := *before.find("R-1")
	output := mustRun(t, "amend", "R-1", "Login failures remain indistinguishable.", "--repo", repo)
	if !strings.Contains(output, "binding and ratchets preserved") {
		t.Fatalf("amend output: %s", output)
	}
	after, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	row := after.find("R-1")
	if row.Rule != "Login failures remain indistinguishable." || row.ID != old.ID || row.EnforcedBy != old.EnforcedBy || row.Check != old.Check || row.RedProof != old.RedProof || row.Hash != old.Hash {
		t.Fatalf("amend changed protected fields: before=%+v after=%+v", old, *row)
	}
	if after.Floor != before.Floor || after.RedProofs != before.RedProofs {
		t.Fatalf("amend changed ratchets: before=%+v after=%+v", before, after)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"amend", "R-9", "Unknown.", "--repo", repo}, "no rule R-9"},
		{[]string{"amend", "R-1", row.Rule, "--repo", repo}, "no-op amendment"},
		{[]string{"amend", "R-1", "bad | sentence", "--repo", repo}, "single line without"},
	} {
		if code, output := run2(t, tc.args...); code == 0 || !strings.Contains(output, tc.want) {
			t.Fatalf("amend refusal %v: code=%d output=%s", tc.args, code, output)
		}
	}
}

func TestRehashHelpExplainsProofRefreshWorkflow(t *testing.T) {
	for _, args := range [][]string{{"rehash", "--help"}, {"help", "rehash"}} {
		code, stdout, stderr := runSeparate(args...)
		if code != 0 || stderr != "" || !strings.Contains(stdout, "prove --supersede") || !strings.Contains(stdout, "no-op") {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"amend", "--help"}, "no-op amendment"},
		{[]string{"check", "--help"}, "complete repository gate"},
		{[]string{"prove", "--help"}, "--supersede"},
		{[]string{"prove", "--help"}, "--run"},
	} {
		code, stdout, stderr := runSeparate(tc.args...)
		if code != 0 || stderr != "" || !strings.Contains(stdout, tc.want) {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", tc.args, code, stdout, stderr)
		}
	}
}

func TestCheckOnlyEvaluatesSelectedBinding(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")
	appendToFile(t, filepath.Join(repo, "rule_test.go"), "\n\n"+"// RULE: OTHER-ONLY-2\n"+`func TestOtherOnly(t *testing.T) {
	t.Log("other binding")
}
`)
	mustRun(t, "declare", "The unrelated selected invariant holds.", "--id", "OTHER-ONLY-2", "--repo", repo)
	mustRun(t, "arm", "OTHER-ONLY-2", "--check", "rule_test.go @ unit", "--red-proof", fixtureProof, "--repo", repo)
	replaceInFile(t, filepath.Join(repo, "rule_test.go"), "other binding", "drifted unrelated binding")

	output := mustRun(t, "check", "--only", "JSON-VALIDATE-1", "--repo", repo)
	if !strings.Contains(output, "PASS JSON-VALIDATE-1") || !strings.Contains(output, "not a full repository gate") || strings.Contains(output, "OTHER-ONLY-2") {
		t.Fatalf("selected output: %s", output)
	}
	if code, _ := run2(t, "check", "--repo", repo); code != 1 {
		t.Fatalf("full check code=%d, want unrelated drift failure", code)
	}
	writeFile(t, repo, "FAIL", "selected test must fail")
	if code, output := run2(t, "check", "--only", "JSON-VALIDATE-1", "--repo", repo); code != 1 || !strings.Contains(output, "go test -run ^TestValidatedRule$ failed") {
		t.Fatalf("selected execution: code=%d output=%s", code, output)
	}
	if code, output := run2(t, "check", "--only", "JSON-VALIDATE-1", "--run-profile", "nightly", "--repo", repo); code != 2 || !strings.Contains(output, "declares \"unit\"") {
		t.Fatalf("selected profile mismatch: code=%d output=%s", code, output)
	}
}

func TestProveRunSupersedesOnlyAfterObservedFailure(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module example.com/prove-run\n\ngo 1.27.0\n")
	writeFile(t, repo, "rule_test.go", validationGoFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "The selected failure is observable.", "--id", "JSON-VALIDATE-1", "--repo", repo)
	mustRun(t, "arm", "JSON-VALIDATE-1", "--check", "rule_test.go @ unit", "--red-proof", "2026-08-24 original observation", "--proof-kind", "manual_observation", "--repo", repo)

	model, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	oldProof, err := ledger.ParseProof(model.find("JSON-VALIDATE-1").RedProof)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "FAIL", "deliberate mutation precondition")
	output := mustRun(t, "prove", "JSON-VALIDATE-1", "--red-proof", "2026-08-25 extended test re-watched", "--supersede", "--run", "--repo", repo)
	if !strings.Contains(output, "superseded prior proof sha256:"+oldProof.Fingerprint()) || !strings.Contains(output, "TestValidatedRule report FAIL") {
		t.Fatalf("supersede output: %s", output)
	}
	model, err = loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	newProof, err := ledger.ParseProof(model.find("JSON-VALIDATE-1").RedProof)
	if err != nil {
		t.Fatal(err)
	}
	if newProof.Kind != ledger.ProofKindManualObservation || newProof.SupersedesFingerprint != oldProof.Fingerprint() || model.RedProofs != 1 || model.measuredRedProofs() != 1 {
		t.Fatalf("superseded proof=%+v ledger=%+v", newProof, model)
	}
	cellAfterFailure := model.find("JSON-VALIDATE-1").RedProof
	if err := os.Remove(filepath.Join(repo, "FAIL")); err != nil {
		t.Fatal(err)
	}
	code, output := run2(t, "prove", "JSON-VALIDATE-1", "--red-proof", "2026-08-25 should not land", "--proof-kind", "mutation_observation", "--supersede", "--run", "--repo", repo)
	if code != 1 || !strings.Contains(output, "passed; no red-proof observation") {
		t.Fatalf("passing prove --run: code=%d output=%s", code, output)
	}
	model, err = loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	if model.find("JSON-VALIDATE-1").RedProof != cellAfterFailure {
		t.Fatal("passing prove --run changed the proof")
	}
}

func TestProveRunFlagSafeguards(t *testing.T) {
	repo := newPWRepo(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"prove", "R-1", "--red-proof", "new", "--replace", "--supersede", "--repo", repo}, "mutually exclusive"},
		{[]string{"prove", "R-1", "--red-proof", "new", "--tags", "integration", "--repo", repo}, "require --run"},
		{[]string{"prove", "R-1", "--red-proof", "new", "--force", "--run", "--profile", "chromium", "--tags", "integration;echo", "--repo", repo}, "invalid build tag"},
		{[]string{"prove", "R-1", "--red-proof", "new", "--force", "--run", "--profile", "chromium", "--repo", repo}, "does not support playwright"},
	} {
		if code, output := run2(t, tc.args...); code != 2 || !strings.Contains(output, tc.want) {
			t.Fatalf("prove safeguard %v: code=%d output=%s", tc.args, code, output)
		}
	}
}

func TestProveRunSupportsExplicitProfileAndTags(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module example.com/prove-run-tags\n\ngo 1.27.0\n")
	writeFile(t, repo, "tagged_test.go", `//go:build integration

package proveruntags

import "testing"

// RULE: PROVE-TAGS-1
func TestTaggedRedObservation(t *testing.T) {
	t.Fatal("deliberate red observation")
}
`)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "The tagged failure can be observed.", "--id", "PROVE-TAGS-1", "--repo", repo)
	mustRun(t, "arm", "PROVE-TAGS-1", "--check", "tagged_test.go @ integration", "--red-proof", "2026-08-24 original observation", "--proof-kind", "manual_observation", "--repo", repo)
	if code, output := run2(t, "prove", "PROVE-TAGS-1", "--red-proof", "2026-08-25 re-watched", "--supersede", "--run", "--repo", repo); code != 2 || !strings.Contains(output, "requires --profile integration") {
		t.Fatalf("missing profile: code=%d output=%s", code, output)
	}
	output := mustRun(t, "prove", "PROVE-TAGS-1", "--red-proof", "2026-08-25 re-watched", "--supersede", "--run", "--profile", "integration", "--tags", "integration", "--repo", repo)
	if !strings.Contains(output, "TestTaggedRedObservation report FAIL") {
		t.Fatalf("tagged observation output: %s", output)
	}
}
