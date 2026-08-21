package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompleteDeclareArmCheckDriftRehashWorkflow(t *testing.T) {
	repo := t.TempDir()
	checkFile := filepath.Join(repo, "e2e", "lifecycle.spec.ts")
	writeFile(t, repo, "e2e/lifecycle.spec.ts", `import { test, expect } from "@playwright/test";

test("binding lifecycle [LIFECYCLE-1]", async () => {
  expect(1).toBe(1);
});
`)

	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "The lifecycle binding remains intact.", "--id", "LIFECYCLE-1", "--repo", repo)
	mustRun(t,
		"arm", "LIFECYCLE-1",
		"--check", "e2e/lifecycle.spec.ts @ browser-ci",
		"--red-proof", "2026-08-21 mutation observed failing and restored",
		"--proof-kind", "mutation_observation",
		"--proof-ref", "https://ci.example.test/runs/99",
		"--repo", repo,
	)
	mustRun(t, "check", "--repo", repo)

	replaceInFile(t, checkFile, "toBe(1)", "toBe(2)")
	if code, output := run2(t, "check", "--repo", repo); code != 1 || !strings.Contains(output, "hash mismatch") {
		t.Fatalf("drift check: exit %d\n%s", code, output)
	}
	mustRun(t, "rehash", "LIFECYCLE-1", "--repo", repo)
	mustRun(t, "check", "--repo", repo)

	model, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	serialized := model.serialize()
	if !strings.HasSuffix(serialized, "\n") {
		t.Fatal("ledger output must end with one newline")
	}
	if err := saveLedger(repo, model); err != nil {
		t.Fatal(err)
	}
	afterSave, err := os.ReadFile(filepath.Join(repo, ledgerFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterSave) != serialized {
		t.Fatal("unchanged ledger save was not deterministic")
	}
}

func TestMachineExitCodesMatchV1Outcomes(t *testing.T) {
	repo := newValidationPWRepo(t)
	code, result, _, stderr := runValidationCommand(t, "validate", "JSON-PW-1", "--repo", repo, "--mode", "static", "--json")
	if code != 0 || stderr != "" || result.Evaluation.Outcome != ValidationPass {
		t.Fatalf("pass: code=%d stderr=%q result=%+v", code, stderr, result)
	}

	replaceInFile(t, filepath.Join(repo, "e2e", "rule.spec.ts"), "expect(true)", "expect(false)")
	code, result, _, stderr = runValidationCommand(t, "validate", "JSON-PW-1", "--repo", repo, "--mode", "static", "--json")
	if code != 1 || stderr != "" || result.Evaluation.Outcome != ValidationFail || result.Evaluation.Reason != "hash_mismatch" {
		t.Fatalf("fail: code=%d stderr=%q result=%+v", code, stderr, result)
	}

	code, result, _, stderr = runValidationCommand(t, "validate", "MISSING-1", "--repo", repo, "--mode", "static", "--json")
	if code != 2 || stderr != "" || result.Evaluation.Outcome != ValidationCannotEvaluate || result.Evaluation.Reason != "rule_not_found" {
		t.Fatalf("cannot evaluate: code=%d stderr=%q result=%+v", code, stderr, result)
	}
}

func TestHelpDistinguishesBindingWorkflowCommands(t *testing.T) {
	code, output := run2(t, "help")
	if code != 0 {
		t.Fatalf("help exit %d\n%s", code, output)
	}
	for _, phrase := range []string{
		"declare records a rule",
		"arm binds it and records a red observation",
		"check verifies all bindings",
		"validate emits one rule as versioned JSON",
		"rehash accepts reviewed source drift",
		"prove records red-proof debt",
	} {
		if !strings.Contains(output, phrase) {
			t.Fatalf("help is missing %q\n%s", phrase, output)
		}
	}
}

func TestHumanArmRejectsCheckFileSymlinkEscape(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "escaped.spec.ts"), []byte(`test("escaped [ESCAPE-1]", async () => {});`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "e2e"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "escaped.spec.ts"), filepath.Join(repo, "e2e", "escaped.spec.ts")); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "External files cannot satisfy bindings.", "--id", "ESCAPE-1", "--repo", repo)
	code, output := run2(t,
		"arm", "ESCAPE-1",
		"--check", "e2e/escaped.spec.ts @ browser-ci",
		"--red-proof", "2026-08-21 observed failing",
		"--repo", repo,
	)
	if code != 1 || !strings.Contains(output, "escapes repository root") {
		t.Fatalf("arm symlink escape: exit %d\n%s", code, output)
	}
}
