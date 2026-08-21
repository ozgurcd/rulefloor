package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ozgurcd/rulefloor/internal/ledger"
)

func TestStructuredProofCreationAndMachineFingerprintCompatibility(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "e2e/proof.spec.ts", `import { test, expect } from "@playwright/test";

test("proof metadata is retained [PROOF-1]", async () => {
  expect(true).toBe(true);
});
`)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Proof metadata remains auditable.", "--id", "PROOF-1", "--repo", repo)
	mustRun(t,
		"arm", "PROOF-1",
		"--check", "e2e/proof.spec.ts @ browser-ci",
		"--red-proof", "2026-08-21 mutation watched failing and restored",
		"--proof-kind", "mutation_observation",
		"--proof-ref", "https://ci.example.test/runs/42",
		"--repo", repo,
	)
	model, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	row := model.find("PROOF-1")
	proof, err := ledger.ParseProof(row.RedProof)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Kind != ledger.ProofKindMutationObservation || proof.Reference == "" || proof.RecordedAt != "2026-08-21" {
		t.Fatalf("proof = %+v", proof)
	}
	code, result, _, stderr := runValidationCommand(t, "validate", "PROOF-1", "--repo", repo, "--mode", "static", "--json")
	if code != 0 || stderr != "" || result.Evaluation.Outcome != ValidationPass || result.Rule.ProofFingerprint == nil {
		t.Fatalf("validation: code=%d stderr=%q result=%+v", code, stderr, result)
	}
	sum := sha256.Sum256([]byte(row.RedProof))
	if *result.Rule.ProofFingerprint != hex.EncodeToString(sum[:]) {
		t.Fatalf("machine fingerprint = %q", *result.Rule.ProofFingerprint)
	}
}

func TestStructuredProofReplacementSafeguardsAndForce(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "e2e/proof.spec.ts", `import { test } from "@playwright/test";
test("replacement remains guarded [PROOF-2]", async () => {});
`)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Proof replacement stays explicit.", "--id", "PROOF-2", "--repo", repo)
	mustRun(t, "arm", "PROOF-2", "--check", "e2e/proof.spec.ts @ browser-ci", "--red-proof", "watched failure", "--proof-kind", "manual_observation", "--repo", repo)

	code, output := run2(t, "prove", "PROOF-2", "--red-proof", "replacement", "--proof-kind", "mutation_observation", "--replace", "--repo", repo)
	if code != 1 || !strings.Contains(output, "protected structured red-proof record") {
		t.Fatalf("ordinary replacement: code=%d output=%s", code, output)
	}
	output = mustRun(t, "prove", "PROOF-2", "--red-proof", "forced replacement", "--proof-kind", "ci_reference", "--proof-ref", "https://ci.example.test/runs/99", "--force", "--repo", repo)
	if !strings.Contains(output, "FORCED overwrite") || !strings.Contains(output, "proof-v1") {
		t.Fatalf("force output = %s", output)
	}
	model, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := ledger.ParseProof(model.find("PROOF-2").RedProof)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Kind != ledger.ProofKindCIReference || model.RedProofs != 1 || model.measuredRedProofs() != 1 {
		t.Fatalf("model = %+v proof = %+v", model, proof)
	}
}

func TestStructuredProofFlagValidation(t *testing.T) {
	repo := t.TempDir()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"invalid kind", []string{"--proof-kind", "arbitrary"}, "invalid proof kind"},
		{"invalid reference", []string{"--proof-kind", "manual_observation", "--proof-ref", "not-a-url"}, "absolute HTTP"},
		{"reference needs kind", []string{"--proof-ref", "https://ci.example.test/runs/1"}, "requires --proof-kind"},
		{"ci needs reference", []string{"--proof-kind", "ci_reference"}, "requires --proof-ref"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"arm", "PROOF-3", "--check", "e2e/proof.spec.ts @ browser-ci", "--red-proof", "watched failure"}
			args = append(args, test.args...)
			args = append(args, "--repo", repo)
			code, output := run2(t, args...)
			if code != 2 || !strings.Contains(output, test.want) {
				t.Fatalf("code=%d output=%s", code, output)
			}
		})
	}
}
