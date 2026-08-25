package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDeclareArmAndAmendCoveredSymbols(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "e2e/login.spec.ts", pwFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Refresh token is single use.", "--id", "R-1", "--covers", "cmdDeclare,cmdArm", "--repo", repo)

	declared, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	if declared.find("R-1").RedProof != "-" || declared.measuredRedProofs() != 0 {
		t.Fatalf("covers-only declaration changed proof semantics: %+v", declared)
	}
	if output := mustRun(t, "unproved", "--repo", repo); !strings.Contains(output, "unproved: 0 of 0 armed rows") {
		t.Fatalf("unproved changed for declared row: %s", output)
	}

	mustRun(t, "arm", "R-1", "--check", "e2e/login.spec.ts @ chromium", "--red-proof", fixtureProof, "--repo", repo)
	armed, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	row := armed.find("R-1")
	if !slices.Equal(row.CoveredSymbols, []string{"cmdArm", "cmdDeclare"}) {
		t.Fatalf("arm did not retain declared symbols: %v", row.CoveredSymbols)
	}
	proof, hash := row.RedProof, row.Hash
	floor, redProofs := armed.Floor, armed.RedProofs

	output := mustRun(t, "amend", "R-1", row.Rule, "--covers", "cmdCheck,cmdArm", "--repo", repo)
	if !strings.Contains(output, "2 covered symbols") {
		t.Fatalf("amend output: %s", output)
	}
	amended, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	row = amended.find("R-1")
	if !slices.Equal(row.CoveredSymbols, []string{"cmdArm", "cmdCheck"}) {
		t.Fatalf("amended symbols = %v", row.CoveredSymbols)
	}
	if row.RedProof != proof || row.Hash != hash || amended.Floor != floor || amended.RedProofs != redProofs {
		t.Fatalf("covered-symbol amendment changed proof, hash, or ratchets: %+v", amended)
	}
	mustRun(t, "check", "--repo", repo)
	mustRun(t, "prove", "R-1", "--red-proof", "2026-08-25 refreshed observation", "--force", "--repo", repo)
	proved, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(proved.find("R-1").CoveredSymbols, []string{"cmdArm", "cmdCheck"}) {
		t.Fatalf("proof replacement dropped covered symbols: %v", proved.find("R-1").CoveredSymbols)
	}
	mustRun(t, "amend", "R-1", "Refresh tokens remain single use.", "--repo", repo)
	proseOnly, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(proseOnly.find("R-1").CoveredSymbols, []string{"cmdArm", "cmdCheck"}) {
		t.Fatalf("prose-only amendment changed covered symbols: %v", proseOnly.find("R-1").CoveredSymbols)
	}

	mustRun(t, "amend", "R-1", proseOnly.find("R-1").Rule, "--covers=", "--repo", repo)
	cleared, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.find("R-1").CoveredSymbols) != 0 {
		t.Fatalf("covered symbols were not cleared: %v", cleared.find("R-1").CoveredSymbols)
	}
	if code, output := run2(t, "amend", "R-1", proseOnly.find("R-1").Rule, "--covers=", "--repo", repo); code != 1 || !strings.Contains(output, "no-op amendment") {
		t.Fatalf("clear no-op: code=%d output=%s", code, output)
	}
}

func TestUnmappedRuleBehaviorIsUnchanged(t *testing.T) {
	repo := newPWRepo(t)
	before, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	rowBefore := before.find("R-1")
	hashBefore, proofBefore := rowBefore.Hash, rowBefore.RedProof
	checkBefore := mustRun(t, "check", "--repo", repo)
	unprovedBefore := mustRun(t, "unproved", "--repo", repo)

	mustRun(t, "covers", "--json", "--repo", repo)
	after, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	rowAfter := after.find("R-1")
	if len(rowAfter.CoveredSymbols) != 0 || rowAfter.Hash != hashBefore || rowAfter.RedProof != proofBefore || after.Floor != before.Floor || after.RedProofs != before.RedProofs {
		t.Fatalf("unmapped rule behavior changed: before=%+v after=%+v", before, after)
	}
	if checkAfter := mustRun(t, "check", "--repo", repo); checkAfter != checkBefore {
		t.Fatalf("check output changed for unmapped rule\nbefore:\n%s\nafter:\n%s", checkBefore, checkAfter)
	}
	if unprovedAfter := mustRun(t, "unproved", "--repo", repo); unprovedAfter != unprovedBefore {
		t.Fatalf("unproved output changed for unmapped rule\nbefore:\n%s\nafter:\n%s", unprovedBefore, unprovedAfter)
	}
	ledgerData, err := os.ReadFile(filepath.Join(repo, ledgerFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ledgerData), "rulefloor-covers-v1") {
		t.Fatal("unmapped row gained covered-symbol metadata")
	}
}

func TestArmAcceptsCoveredSymbols(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, repo, "e2e/login.spec.ts", pwFixture)
	mustRun(t, "init", "--repo", repo)
	mustRun(t, "declare", "Refresh token is single use.", "--id", "R-1", "--repo", repo)
	mustRun(t, "arm", "R-1", "--check", "e2e/login.spec.ts @ chromium", "--red-proof", fixtureProof, "--covers", "cmdArm,internal/ledger.Parse", "--repo", repo)
	model, err := loadLedger(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(model.find("R-1").CoveredSymbols, []string{"cmdArm", "internal/ledger.Parse"}) {
		t.Fatalf("armed symbols = %v", model.find("R-1").CoveredSymbols)
	}
}

// RULE: COVERS-MAP-1
func TestCoversJSONMatchesV1Fixture(t *testing.T) {
	repo := t.TempDir()
	model := &Ledger{Floor: 2, HasRedProofs: true, Rows: []Row{
		{ID: "R-1", Rule: "First rule.", EnforcedBy: "-", Check: "NONE", RedProof: "-", Hash: "-", CoveredSymbols: []string{"pkg.Auth", "pkg.Refresh"}},
		{ID: "R-2", Rule: "Second rule.", EnforcedBy: "-", Check: "NONE", RedProof: "-", Hash: "-"},
	}}
	if err := saveLedger(repo, model); err != nil {
		t.Fatal(err)
	}
	ledgerData, err := os.ReadFile(filepath.Join(repo, ledgerFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ledgerData), "| ID | one-sentence rule | enforced-by | check | red-proof | hash |") || strings.Contains(string(ledgerData), "| covered-symbols |") {
		t.Fatalf("covered symbols changed the six-column ledger:\n%s", ledgerData)
	}
	code, stdout, stderr := runSeparate("covers", "--json", "--repo", repo)
	if code != 0 || stderr != "" {
		t.Fatalf("covers JSON: code=%d stderr=%q", code, stderr)
	}
	expected, err := os.ReadFile(filepath.Join("testdata", "machine", "rulefloor.covers.v1.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if stdout != string(expected) {
		t.Fatalf("covers document changed\nactual:   %s\nexpected: %s", stdout, expected)
	}
	var result coversResult
	decodeSingleJSON(t, stdout, &result)
	if result.SchemaVersion != coversSchemaVersion || !slices.Equal(result.Rules["R-1"], []string{"pkg.Auth", "pkg.Refresh"}) || len(result.Rules["R-2"]) != 0 {
		t.Fatalf("covers result = %+v", result)
	}

	human := mustRun(t, "covers", "--repo", repo)
	if human != "R-1: pkg.Auth, pkg.Refresh\nR-2: -\n" {
		t.Fatalf("human covers output = %q", human)
	}
}

func TestCoveredSymbolInputAndLedgerValidation(t *testing.T) {
	repo := t.TempDir()
	mustRun(t, "init", "--repo", repo)
	for _, tc := range []struct {
		value string
		want  string
	}{
		{"", "requires at least one symbol"},
		{"pkg.Func,pkg.Func", "duplicated"},
		{"pkg.Bad Name", "whitespace"},
	} {
		if code, output := run2(t, "declare", "A rule.", "--id", "R-1", "--covers", tc.value, "--repo", repo); code != 2 || !strings.Contains(output, tc.want) {
			t.Fatalf("declare --covers %q: code=%d output=%s", tc.value, code, output)
		}
	}
	mustRun(t, "declare", "A rule.", "--id", "R-1", "--covers", "pkg.Func", "--repo", repo)
	ledgerPath := filepath.Join(repo, ledgerFile)
	replaceInFile(t, ledgerPath, "<!-- rulefloor-covers-v1:", "<!-- rulefloor-covers-v1:not-base64!")
	if code, output := run2(t, "check", "--repo", repo); code != 2 || !strings.Contains(output, "invalid covered-symbol metadata") {
		t.Fatalf("malformed metadata check: code=%d output=%s", code, output)
	}
}
