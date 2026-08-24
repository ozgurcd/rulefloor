package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffUsesMatchingGitBoundSpanWithoutChangingLedger(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module example.com/drift\n\ngo 1.27.0\n")
	writeFile(t, repo, "rule_test.go", "package drift\n\nimport \"testing\"\n\n// RULE: DRIFT-1\nfunc TestDrift(t *testing.T) {\n\t// before\n}\n")
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Drift is reviewed.", "--id", "DRIFT-1", "--repo", repo)
	mustRun(t, "arm", "DRIFT-1", "--check", "rule_test.go @ unit", "--red-proof", fixtureProof, "--repo", repo)
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.name", "Rulefloor Test")
	gitRun(t, repo, "config", "user.email", "rulefloor@example.test")
	gitRun(t, repo, "add", "go.mod", "rule_test.go", ledgerFile)
	gitRun(t, repo, "commit", "-m", "baseline")

	ledgerBefore, err := os.ReadFile(filepath.Join(repo, ledgerFile))
	if err != nil {
		t.Fatal(err)
	}
	replaceInFile(t, filepath.Join(repo, "rule_test.go"), "// before", "// after")
	output := mustRun(t, "diff", "DRIFT-1", "--repo", repo)
	if !strings.Contains(output, "drift DRIFT-1") || !strings.Contains(output, "-\t// before") || !strings.Contains(output, "+\t// after") {
		t.Fatalf("unexpected diff output:\n%s", output)
	}
	ledgerAfter, err := os.ReadFile(filepath.Join(repo, ledgerFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(ledgerAfter) != string(ledgerBefore) {
		t.Fatal("diff modified the ledger")
	}
}

func TestDiffReportsCleanBindingAndMissingGitBaseline(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module example.com/drift-clean\n\ngo 1.27.0\n")
	writeFile(t, repo, "rule_test.go", "package driftclean\n\nimport \"testing\"\n\n// RULE: DRIFT-CLEAN-1\nfunc TestDriftClean(t *testing.T) {}\n")
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Clean binding is reported.", "--id", "DRIFT-CLEAN-1", "--repo", repo)
	mustRun(t, "arm", "DRIFT-CLEAN-1", "--check", "rule_test.go @ unit", "--red-proof", fixtureProof, "--repo", repo)
	if output := mustRun(t, "diff", "DRIFT-CLEAN-1", "--repo", repo); !strings.Contains(output, "no drift DRIFT-CLEAN-1") {
		t.Fatalf("unexpected clean output: %s", output)
	}
	replaceInFile(t, filepath.Join(repo, "rule_test.go"), "TestDriftClean", "TestDriftChanged")
	code, output := run2(t, "diff", "DRIFT-CLEAN-1", "--repo", repo)
	if code != 2 || !strings.Contains(output, "CANNOT-EVALUATE") {
		t.Fatalf("missing baseline: exit %d\n%s", code, output)
	}
}

func TestCommandSpecificProofAndDiffHelp(t *testing.T) {
	for _, args := range [][]string{{"help", "arm"}, {"arm", "--help"}, {"help", "prove"}} {
		code, stdout, stderr := runSeparate(args...)
		if code != 0 || stderr != "" || !strings.Contains(stdout, `cannot contain "|"`) {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
	code, stdout, stderr := runSeparate("diff", "--help")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "does not classify") {
		t.Fatalf("diff help: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func gitRun(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
