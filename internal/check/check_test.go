package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurcd/rulefloor/internal/extract"
	"github.com/ozgurcd/rulefloor/internal/ledger"
)

func TestOrphanDiscoveryIgnoresGoFixtureStringsAndFindsRealTags(t *testing.T) {
	repo := t.TempDir()
	source := "package sample\n\n" +
		"const raw = `package fixture\\n\\n// RULE: FIXTURE-1\\nfunc TestFixture(t *testing.T) {}`\n" +
		"const interpreted = \"// RULE: STRING-1\"\n\n" +
		"// RULE: ORPHAN-1\nfunc TestOrphan(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(repo, "sample_test.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, err := ScanOrphans(extract.DefaultRegistry(), repo, &ledger.Ledger{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "orphan tag ORPHAN-1") {
		t.Fatalf("problems = %v", problems)
	}
	if strings.Contains(joined, "FIXTURE-1") || strings.Contains(joined, "STRING-1") {
		t.Fatalf("fixture string became an orphan: %v", problems)
	}
}

func TestStaticEvaluationUsesExtractorFingerprintAndSemantics(t *testing.T) {
	source := "package sample\n\n// RULE: SHARED-1\nfunc TestShared(t *testing.T) {}\n"
	ref, err := extract.NewGo().Extract(source, "SHARED-1")
	if err != nil {
		t.Fatal(err)
	}
	row := &ledger.Row{ID: "SHARED-1", EnforcedBy: "go-test", Check: "shared_test.go @ unit", RedProof: "proof", Hash: ref.Fingerprint()}
	evaluation, err := EvaluateSource(extract.DefaultRegistry(), row, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(evaluation.Issues) != 0 || evaluation.Ref.Fingerprint() != row.Hash || evaluation.Ref.FuncName != "TestShared" || evaluation.Binding.Execution != ledger.ExecutionExecute || evaluation.Binding.Profile != "unit" {
		t.Fatalf("evaluation = %+v", evaluation)
	}
}

func TestStaticLegacyPolicyAllowsGuardedGoTestWithoutGrantingExecutionByProfileName(t *testing.T) {
	source := "package sample\n\n// RULE: POLICY-1\nfunc TestPolicy(t *testing.T) { t.Skip(\"environment absent\") }\n"
	ref, err := extract.NewGo().Extract(source, "POLICY-1")
	if err != nil {
		t.Fatal(err)
	}
	row := &ledger.Row{ID: "POLICY-1", EnforcedBy: "go-test", Check: "policy_test.go @ unit-like-name", RedProof: "proof", Hash: ref.Fingerprint()}
	evaluation, err := EvaluateSource(extract.DefaultRegistry(), row, source)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Binding.Execution != ledger.ExecutionStatic || evaluation.Binding.Profile != "unit-like-name" || len(evaluation.Issues) != 0 {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	if !SupportsExecution(extract.KindGoTest) || SupportsExecution(extract.KindPlaywright) || SupportsExecution(extract.KindVitest) {
		t.Fatal("execution support must be determined by test kind")
	}
}
