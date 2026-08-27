package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

const validationGoFixture = `package sample

import (
	"os"
	"testing"
)

` + "// RULE: JSON-VALIDATE-1\n" + `func TestValidatedRule(t *testing.T) {
	if _, err := os.Stat("FAIL"); err == nil {
		t.Fatal("forced failure")
	}
}
`

const validationProfileFixture = `package sample

import (
	"os"
	"testing"
)

` + "// RULE: JSON-PROFILE-1\n" + `func TestProfileRule(t *testing.T) {
	if _, err := os.Stat("SKIP"); err == nil {
		t.Skip("profile environment unavailable")
	}
}
`

const taggedValidationProfileFixture = `//go:build integration

package sample

import "testing"

` + "// RULE: JSON-TAGGED-1\n" + `func TestTaggedProfileRule(t *testing.T) {}
`

const validationPWFixture = `import { test, expect } from "@playwright/test";

test("[JSON-PW-1] invariant holds", async () => {
  expect(true).toBe(true);
});
`

// RULE: JSON-INTERFACE-1
func TestVersionJSON(t *testing.T) {
	code, stdout, stderr := runSeparate("version", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("version --json: code=%d stderr=%q", code, stderr)
	}
	var got map[string]any
	decodeSingleJSON(t, stdout, &got)
	if !reflect.DeepEqual(sortedKeys(got), []string{"schema_version", "toolchain_version", "version", "version_agreement"}) {
		t.Fatalf("version keys = %v", sortedKeys(got))
	}
	if got["schema_version"] != versionSchemaVersion || got["version"] != "dev" || got["toolchain_version"] != "(devel)" {
		t.Fatalf("version JSON = %#v", got)
	}
	if got["version_agreement"] != "pass" {
		t.Fatalf("version agreement = %v, want pass for a dev toolchain build", got["version_agreement"])
	}
}

// RULE: VALIDATE-MODES-1
func TestValidateStaticPassJSON(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")
	writeFile(t, repo, "FAIL", "static mode must not execute the test")
	code, result, stdout, stderr := runValidationCommand(t, "validate", "JSON-VALIDATE-1", "--repo", repo, "--mode", "static", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("static validate: code=%d stderr=%q result=%+v", code, stderr, result)
	}
	if result.SchemaVersion != validationSchemaVersion || result.Command != "validate" || result.RulefloorVersion != "dev" {
		t.Fatalf("identity fields = %+v", result)
	}
	if result.Evaluation.Outcome != ValidationPass || result.Evaluation.StaticIntegrity != ValidationStatusPass {
		t.Fatalf("evaluation = %+v", result.Evaluation)
	}
	if result.Evaluation.Execution.Requested || result.Evaluation.Execution.Performed || result.Evaluation.Execution.Status != ValidationStatusNotRequested {
		t.Fatalf("static execution = %+v", result.Evaluation.Execution)
	}
	if len(result.Rule.TestFingerprint.Expected) != 12 || result.Rule.TestFingerprint.Expected != result.Rule.TestFingerprint.Actual {
		t.Fatalf("test fingerprint = %+v", result.Rule.TestFingerprint)
	}
	if result.Rule.ProofFingerprint == nil || len(*result.Rule.ProofFingerprint) != 64 || result.Rule.RedProofStatus != RedProofPresent {
		t.Fatalf("proof fields = %+v", result.Rule)
	}
	ledger, err := os.ReadFile(filepath.Join(repo, ledgerFile))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(ledger)
	if result.Repository.LedgerFingerprint != hex.EncodeToString(sum[:]) {
		t.Fatalf("ledger fingerprint = %q", result.Repository.LedgerFingerprint)
	}
	var raw map[string]any
	decodeSingleJSON(t, stdout, &raw)
	wantKeys := []string{"command", "evaluation", "generated_at", "repository", "request", "rule", "rulefloor_version", "schema_version"}
	if !reflect.DeepEqual(sortedKeys(raw), wantKeys) {
		t.Fatalf("top-level keys = %v, want %v", sortedKeys(raw), wantKeys)
	}
	assertJSONKeys(t, raw["repository"], "ledger_fingerprint", "ledger_path", "root")
	assertJSONKeys(t, raw["request"], "mode", "profile", "rule_id")
	assertJSONKeys(t, raw["rule"], "armed", "check_file", "declared_profile", "enforced_by", "exists", "proof_fingerprint", "red_proof_status", "test_fingerprint")
	assertJSONKeys(t, raw["evaluation"], "diagnostics", "execution", "outcome", "reason", "static_integrity")
	evaluation := raw["evaluation"].(map[string]any)
	assertJSONKeys(t, evaluation["execution"], "performed", "requested", "status")
	rule := raw["rule"].(map[string]any)
	assertJSONKeys(t, rule["test_fingerprint"], "actual", "expected")
}

func TestValidateStaticOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*testing.T, string)
		ruleID     string
		wantCode   int
		want       ValidationOutcome
		wantReason string
	}{
		{
			name: "hash mismatch", mutate: func(t *testing.T, repo string) {
				replaceInFile(t, filepath.Join(repo, "rule_test.go"), "forced failure", "changed failure")
			}, ruleID: "JSON-VALIDATE-1", wantCode: 1, want: ValidationFail, wantReason: "hash_mismatch",
		},
		{
			name: "missing tag", mutate: func(t *testing.T, repo string) {
				replaceInFile(t, filepath.Join(repo, "rule_test.go"), "// RULE: JSON-VALIDATE-1", "// RULE: OTHER-RULE-1")
			}, ruleID: "JSON-VALIDATE-1", wantCode: 2, want: ValidationCannotEvaluate, wantReason: "tag_missing",
		},
		{
			name: "ambiguous tag", mutate: func(t *testing.T, repo string) {
				appendToFile(t, filepath.Join(repo, "rule_test.go"), "\n// RULE: JSON-VALIDATE-1\nfunc TestDuplicateRule(t *testing.T) {}\n")
			}, ruleID: "JSON-VALIDATE-1", wantCode: 2, want: ValidationCannotEvaluate, wantReason: "tag_ambiguous",
		},
		{
			name: "missing check file", mutate: func(t *testing.T, repo string) {
				if err := os.Remove(filepath.Join(repo, "rule_test.go")); err != nil {
					t.Fatal(err)
				}
			}, ruleID: "JSON-VALIDATE-1", wantCode: 2, want: ValidationCannotEvaluate, wantReason: "check_file_missing",
		},
		{
			name: "malformed ledger", mutate: func(t *testing.T, repo string) {
				writeFile(t, repo, ledgerFile, "not a ledger\n")
			}, ruleID: "JSON-VALIDATE-1", wantCode: 2, want: ValidationCannotEvaluate, wantReason: "ledger_invalid",
		},
		{
			name: "missing rule", mutate: func(*testing.T, string) {}, ruleID: "MISSING-RULE-1",
			wantCode: 2, want: ValidationCannotEvaluate, wantReason: "rule_not_found",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newValidationGoRepo(t, "unit")
			tc.mutate(t, repo)
			code, result, _, stderr := runValidationCommand(t, "validate", tc.ruleID, "--repo", repo, "--mode", "static", "--json")
			if code != tc.wantCode || result.Evaluation.Outcome != tc.want || result.Evaluation.Reason != tc.wantReason || stderr != "" {
				t.Fatalf("code=%d stderr=%q evaluation=%+v", code, stderr, result.Evaluation)
			}
		})
	}
}

// RULE: SINGLE-RULE-1
func TestValidateSelectedRuleIgnoresOtherBrokenRule(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")
	appendToFile(t, filepath.Join(repo, "rule_test.go"), "\n\n"+"// RULE: OTHER-VALIDATE-2\n"+`func TestOtherRule(t *testing.T) {
	t.Log("other invariant")
}
`)
	mustRun(t, "declare", "An unrelated invariant holds.", "--id", "OTHER-VALIDATE-2", "--repo", repo)
	mustRun(t, "arm", "OTHER-VALIDATE-2", "--check", "rule_test.go @ unit", "--red-proof", fixtureProof, "--repo", repo)
	replaceInFile(t, filepath.Join(repo, "rule_test.go"), "other invariant", "changed unrelated invariant")
	if code, _ := run2(t, "check", "--repo", repo); code != 1 {
		t.Fatalf("fixture check code = %d, want unrelated row failure", code)
	}
	code, result, _, stderr := runValidationCommand(t, "validate", "JSON-VALIDATE-1", "--repo", repo, "--mode", "static", "--json")
	if code != 0 || stderr != "" || result.Evaluation.Outcome != ValidationPass {
		t.Fatalf("selected result: code=%d stderr=%q result=%+v", code, stderr, result)
	}
}

func TestValidateUnarmedAndMissingRedProof(t *testing.T) {
	t.Run("unarmed", func(t *testing.T) {
		repo := t.TempDir()
		mustRun(t, "init", "--repo", repo)
		mustRun(t, "declare", "A declared rule.", "--id", "DECLARED-RULE-1", "--repo", repo)
		code, result, _, _ := runValidationCommand(t, "validate", "DECLARED-RULE-1", "--repo", repo, "--mode", "static", "--json")
		if code != 2 || result.Evaluation.Reason != "rule_unarmed" || result.Rule.RedProofStatus != RedProofNotApplicable {
			t.Fatalf("result = %+v", result)
		}
	})
	t.Run("armed proof missing", func(t *testing.T) {
		repo := newValidationGoRepo(t, "unit")
		replaceInFile(t, filepath.Join(repo, ledgerFile), fixtureProof, "-")
		code, result, _, _ := runValidationCommand(t, "validate", "JSON-VALIDATE-1", "--repo", repo, "--mode", "static", "--json")
		if code != 1 || result.Evaluation.Reason != "red_proof_missing" || result.Rule.ProofFingerprint != nil {
			t.Fatalf("result = %+v", result)
		}
	})
}

func TestValidateExecuteGoUnit(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")
	code, result, _, stderr := runValidationCommand(t, "validate", "JSON-VALIDATE-1", "--repo", repo, "--mode", "execute", "--json")
	if code != 0 || stderr != "" || result.Evaluation.Outcome != ValidationPass || !result.Evaluation.Execution.Performed || result.Evaluation.Execution.Status != ValidationStatusPass {
		t.Fatalf("pass result: code=%d stderr=%q result=%+v", code, stderr, result)
	}
	writeFile(t, repo, "FAIL", "fail without changing the test body")
	code, result, _, stderr = runValidationCommand(t, "validate", "JSON-VALIDATE-1", "--repo", repo, "--mode", "execute", "--json")
	if code != 1 || stderr != "" || result.Evaluation.Outcome != ValidationFail || result.Evaluation.Reason != "rule_failed" || result.Evaluation.Execution.Status != ValidationStatusFail {
		t.Fatalf("fail result: code=%d stderr=%q result=%+v", code, stderr, result)
	}
}

func TestValidateExecuteProfileSemantics(t *testing.T) {
	repo := newValidationProfileRepo(t)
	code, result, _, _ := runValidationCommand(t, "validate", "JSON-PROFILE-1", "--repo", repo, "--mode", "static", "--json")
	if code != 0 || result.Evaluation.Outcome != ValidationPass {
		t.Fatalf("static result = %+v", result)
	}
	for _, tc := range []struct {
		name    string
		profile string
	}{
		{name: "missing profile"},
		{name: "wrong profile", profile: "nightly"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"validate", "JSON-PROFILE-1", "--repo", repo, "--mode", "execute", "--json"}
			if tc.profile != "" {
				args = append(args, "--profile", tc.profile)
			}
			code, result, _, _ := runValidationCommand(t, args...)
			if code != 2 || result.Evaluation.Reason != "profile_mismatch" {
				t.Fatalf("result = %+v", result)
			}
		})
	}
	writeFile(t, repo, "SKIP", "runtime precondition absent")
	code, result, _, _ = runValidationCommand(t, "validate", "JSON-PROFILE-1", "--repo", repo, "--mode", "execute", "--profile", "integration", "--json")
	if code != 2 || result.Evaluation.Reason != "test_skipped" || result.Evaluation.Execution.Status != ValidationStatusCannotEvaluate {
		t.Fatalf("skip result = %+v", result)
	}
}

func TestValidateExecuteSupportsExplicitBuildTags(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module example.com/rulefloor-tagged\n\ngo 1.27.0\n")
	writeFile(t, repo, "tagged_test.go", taggedValidationProfileFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "The tagged invariant holds.", "--id", "JSON-TAGGED-1", "--repo", repo)
	mustRun(t, "arm", "JSON-TAGGED-1", "--check", "tagged_test.go @ integration", "--red-proof", fixtureProof, "--repo", repo)

	code, result, _, stderr := runValidationCommand(t, "validate", "JSON-TAGGED-1", "--repo", repo, "--mode", "execute", "--profile", "integration", "--tags", "integration", "--json")
	if code != 0 || stderr != "" || result.Evaluation.Outcome != ValidationPass || result.Request.Tags != "integration" {
		t.Fatalf("tagged execute result: code=%d stderr=%q result=%+v", code, stderr, result)
	}

	for _, args := range [][]string{
		{"validate", "JSON-TAGGED-1", "--repo", repo, "--mode", "static", "--tags", "integration", "--json"},
		{"validate", "JSON-TAGGED-1", "--repo", repo, "--mode", "execute", "--profile", "integration", "--tags", "integration;echo", "--json"},
	} {
		code, result, _, stderr = runValidationCommand(t, args...)
		if code != 2 || stderr != "" || result.Evaluation.Reason != "invalid_request" {
			t.Fatalf("invalid tags %v: code=%d stderr=%q result=%+v", args, code, stderr, result)
		}
	}
}

func TestValidatePlaywrightStaticAndExecuteUnsupported(t *testing.T) {
	repo := newValidationPWRepo(t)
	code, result, _, _ := runValidationCommand(t, "validate", "JSON-PW-1", "--repo", repo, "--mode", "static", "--json")
	if code != 0 || result.Evaluation.Outcome != ValidationPass {
		t.Fatalf("static result = %+v", result)
	}
	code, result, _, _ = runValidationCommand(t, "validate", "JSON-PW-1", "--repo", repo, "--mode", "execute", "--profile", "chromium", "--json")
	if code != 2 || result.Evaluation.Reason != "execution_unsupported" || result.Evaluation.Execution.Performed {
		t.Fatalf("execute result = %+v", result)
	}
	replaceInFile(t, filepath.Join(repo, "e2e", "rule.spec.ts"), "test(\"[JSON-PW-1]", "test.skip(\"[JSON-PW-1]")
	code, result, _, _ = runValidationCommand(t, "validate", "JSON-PW-1", "--repo", repo, "--mode", "static", "--json")
	if code != 1 || result.Evaluation.Reason != "test_skipped" {
		t.Fatalf("skip static result = %+v", result)
	}
	onlyRepo := newValidationPWRepo(t)
	replaceInFile(t, filepath.Join(onlyRepo, "e2e", "rule.spec.ts"), "test(\"[JSON-PW-1]", "test.only(\"[JSON-PW-1]")
	code, result, _, _ = runValidationCommand(t, "validate", "JSON-PW-1", "--repo", onlyRepo, "--mode", "static", "--json")
	if code != 1 || result.Evaluation.Reason != "test_restricted" {
		t.Fatalf("only static result = %+v", result)
	}
}

func TestValidateInvalidRequestsAreJSONOnly(t *testing.T) {
	tests := [][]string{
		{"validate", "--mode", "static", "--json"},
		{"validate", "BAD", "--mode", "static", "--json"},
		{"validate", "JSON-VALIDATE-1", "--mode", "unknown", "--json"},
		{"validate", "JSON-VALIDATE-1", "--mode", "static", "--profile", "unit", "--json"},
		{"validate", "JSON-VALIDATE-1", "--bogus", "--json"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, result, stdout, stderr := runValidationCommand(t, args...)
			if code != 2 || stderr != "" || result.Evaluation.Reason != "invalid_request" {
				t.Fatalf("code=%d stderr=%q stdout=%q result=%+v", code, stderr, stdout, result)
			}
		})
	}
}

func TestValidateRepositoryConfinement(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")
	outside := t.TempDir()
	writeFile(t, outside, "outside_test.go", validationGoFixture)
	if err := os.Remove(filepath.Join(repo, "rule_test.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside_test.go"), filepath.Join(repo, "rule_test.go")); err != nil {
		t.Fatal(err)
	}
	code, result, _, _ := runValidationCommand(t, "validate", "JSON-VALIDATE-1", "--repo", repo, "--mode", "static", "--json")
	if code != 2 || result.Evaluation.Reason != "check_file_missing" || !strings.Contains(result.Evaluation.Diagnostics[0].Message, "escapes repository root") {
		t.Fatalf("result = %+v", result)
	}
}

func TestValidationServiceOperationalFailures(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")
	fixed := time.Date(2026, 8, 20, 12, 34, 56, 0, time.UTC)
	service := validationService{
		version: "v0.test",
		now:     func() time.Time { return fixed },
		runner:  fakeValidationRunner{err: exec.ErrNotFound},
	}
	result := service.Validate(context.Background(), ValidationRequest{Repository: repo, RuleID: "JSON-VALIDATE-1", Mode: ValidationModeExecute})
	if result.Evaluation.Outcome != ValidationCannotEvaluate || result.Evaluation.Reason != "toolchain_unavailable" || result.GeneratedAt != fixed.Format(time.RFC3339Nano) {
		t.Fatalf("missing toolchain result = %+v", result)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	result = service.Validate(canceled, ValidationRequest{Repository: repo, RuleID: "JSON-VALIDATE-1", Mode: ValidationModeExecute})
	if result.Evaluation.Reason != "context_canceled" || result.Evaluation.Outcome != ValidationCannotEvaluate {
		t.Fatalf("canceled result = %+v", result)
	}

	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	result = service.Validate(deadline, ValidationRequest{Repository: repo, RuleID: "JSON-VALIDATE-1", Mode: ValidationModeExecute})
	if result.Evaluation.Reason != "deadline_exceeded" {
		t.Fatalf("deadline result = %+v", result)
	}
}

type fakeValidationRunner struct {
	out []byte
	err error
}

func (f fakeValidationRunner) Run(context.Context, string, string, ...string) ([]byte, error) {
	return f.out, f.err
}

func newValidationGoRepo(t *testing.T, profile string) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module example.com/rulefloor-validation\n\ngo 1.26\n")
	writeFile(t, repo, "rule_test.go", validationGoFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "The selected invariant holds.", "--id", "JSON-VALIDATE-1", "--repo", repo)
	mustRun(t, "arm", "JSON-VALIDATE-1", "--check", "rule_test.go @ "+profile, "--red-proof", fixtureProof, "--repo", repo)
	return repo
}

func newValidationProfileRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module example.com/rulefloor-profile\n\ngo 1.26\n")
	writeFile(t, repo, "profile_test.go", validationProfileFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "The profile invariant holds.", "--id", "JSON-PROFILE-1", "--repo", repo)
	mustRun(t, "arm", "JSON-PROFILE-1", "--check", "profile_test.go @ integration", "--red-proof", fixtureProof, "--repo", repo)
	return repo
}

func newValidationPWRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	writeFile(t, repo, filepath.Join("e2e", "rule.spec.ts"), validationPWFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "The browser invariant holds.", "--id", "JSON-PW-1", "--repo", repo)
	mustRun(t, "arm", "JSON-PW-1", "--check", "e2e/rule.spec.ts @ chromium", "--red-proof", fixtureProof, "--repo", repo)
	return repo
}

func runValidationCommand(t *testing.T, args ...string) (int, ValidationResult, string, string) {
	t.Helper()
	code, stdout, stderr := runSeparate(args...)
	var result ValidationResult
	decodeSingleJSON(t, stdout, &result)
	return code, result, stdout, stderr
}

func runSeparate(args ...string) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func decodeSingleJSON(t *testing.T, data string, target any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode JSON %q: %v", data, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contains more than one JSON document: %q (err=%v)", data, err)
	}
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertJSONKeys(t *testing.T, value any, keys ...string) {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("JSON value has type %T, want object", value)
	}
	sort.Strings(keys)
	if got := sortedKeys(object); !reflect.DeepEqual(got, keys) {
		t.Fatalf("JSON keys = %v, want %v", got, keys)
	}
}
