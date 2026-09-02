package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ozgurcd/rulefloor/internal/ledgerdiff"
)

func TestLedgerDiffReportsSentenceChangeWithoutMovingTestFingerprint(t *testing.T) {
	repo := newPWRepo(t)
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.name", "Rulefloor Test")
	gitRun(t, repo, "config", "user.email", "rulefloor@example.test")
	gitRun(t, repo, "add", ledgerFile, "e2e/login.spec.ts")
	gitRun(t, repo, "commit", "-m", "baseline")

	before, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	oldHash := before.find("R-1").Hash
	mustRun(t, "amend", "R-1", "Refresh tokens remain single use.", "--repo", repo)

	code, human, stderr := runSeparate("ledger-diff", "--base", "HEAD", "--repo", repo)
	if code != 1 || stderr != "" || !strings.Contains(human, "R-1: sentence_changed") || strings.Contains(human, "test_fingerprint_changed") {
		t.Fatalf("human ledger-diff: code=%d stdout=%q stderr=%q", code, human, stderr)
	}
	ledgerBeforeJSON, err := os.ReadFile(filepath.Join(repo, ledgerFile))
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runSeparate("ledger-diff", "--base", "HEAD", "--repo", repo, "--json")
	if code != 1 || stderr != "" {
		t.Fatalf("JSON ledger-diff: code=%d stderr=%q", code, stderr)
	}
	var result ledgerDiffResult
	decodeSingleJSON(t, stdout, &result)
	if result.SchemaVersion != ledgerdiff.SchemaVersion || result.Status != "different" || len(result.Rules) != 1 {
		t.Fatalf("result = %+v", result)
	}
	wantKinds := []ledgerdiff.ChangeKind{ledgerdiff.ChangeSentence}
	if !reflect.DeepEqual(result.Rules[0].Changes, wantKinds) || result.Rules[0].BeforeSentenceExcerpt == result.Rules[0].AfterSentenceExcerpt {
		t.Fatalf("rule change = %+v", result.Rules[0])
	}
	after, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	if after.find("R-1").Hash != oldHash {
		t.Fatalf("test fingerprint moved: %s -> %s", oldHash, after.find("R-1").Hash)
	}
	ledgerAfterJSON, err := os.ReadFile(filepath.Join(repo, ledgerFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ledgerBeforeJSON, ledgerAfterJSON) {
		t.Fatal("ledger-diff modified the current ledger")
	}
}

func TestLedgerDiffJSONCannotEvaluateIsSingleDocumentWithoutStderr(t *testing.T) {
	repo := newPWRepo(t)
	gitRun(t, repo, "init")
	code, stdout, stderr := runSeparate("ledger-diff", "--base", "missing", "--repo", repo, "--json")
	if code != 2 || stderr != "" {
		t.Fatalf("ledger-diff cannot-evaluate: code=%d stderr=%q", code, stderr)
	}
	var result ledgerDiffResult
	decodeSingleJSON(t, stdout, &result)
	if result.Status != "cannot_evaluate" || result.SchemaVersion != ledgerdiff.SchemaVersion || result.Error == "" || result.Rules == nil {
		t.Fatalf("result = %+v", result)
	}
}

func TestLedgerDiffJSONMatchesV1Fixture(t *testing.T) {
	redProofs := 1
	header := ledgerdiff.HeaderState{Floor: 1, RedProofs: &redProofs, RepairedFixtureCount: 0}
	comparison := ledgerdiff.Comparison{
		BaseRef: "main", BaseCommit: "0123456789012345678901234567890123456789",
		Before: header, After: header, Rules: []ledgerdiff.RuleChange{},
	}
	var output bytes.Buffer
	if err := writeJSON(&output, newLedgerDiffResult(comparison, "same")); err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join("testdata", "machine", "rulefloor.ledger-diff.v1.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(expected) {
		t.Fatalf("ledger-diff document changed\nactual:   %s\nexpected: %s", output.String(), expected)
	}
	var decoded ledgerDiffResult
	decodeSingleJSON(t, output.String(), &decoded)
}
