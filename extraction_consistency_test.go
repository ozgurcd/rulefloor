package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestArmRehashCheckAndValidateShareGoExtraction(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module fixture\n\ngo 1.26\n")
	writeFile(t, repo, "shared_test.go", `package fixture

import "testing"

const embeddedFixture = `+"`"+`// RULE: SHARED-1
func TestFixture(t *testing.T) {}
`+"`"+`

// RULE: SHARED-1
func TestShared(t *testing.T) {
	t.Log("original")
}
`)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "All operations bind the same parsed Go test.", "--id", "SHARED-1", "--repo", repo)
	mustRun(t, "arm", "SHARED-1", "--check", "shared_test.go @ unit", "--red-proof", fixtureProof, "--repo", repo)
	mustRun(t, "check", "--repo", repo)
	code, result, _, stderr := runValidationCommand(t, "validate", "SHARED-1", "--repo", repo, "--mode", "static", "--json")
	if code != 0 || stderr != "" || result.Evaluation.Outcome != ValidationPass {
		t.Fatalf("initial validation: code=%d stderr=%q result=%+v", code, stderr, result)
	}

	replaceInFile(t, filepath.Join(repo, "shared_test.go"), `t.Log("original")`, `t.Log("reviewed")`)
	if code, output := run2(t, "check", "--repo", repo); code != 1 || !strings.Contains(output, "hash mismatch") {
		t.Fatalf("changed check: code=%d output=%s", code, output)
	}
	mustRun(t, "rehash", "SHARED-1", "--repo", repo)
	mustRun(t, "check", "--repo", repo)
	code, result, _, stderr = runValidationCommand(t, "validate", "SHARED-1", "--repo", repo, "--mode", "static", "--json")
	if code != 0 || stderr != "" || result.Rule.TestFingerprint.Actual != result.Rule.TestFingerprint.Expected {
		t.Fatalf("post-rehash validation: code=%d stderr=%q result=%+v", code, stderr, result)
	}
}
