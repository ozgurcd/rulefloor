package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	binarycap "github.com/ozgurcd/rulefloor/internal/capabilities"
)

func TestCapabilitiesHumanOutput(t *testing.T) {
	code, stdout, stderr := runSeparate("capabilities")
	if code != 0 || stderr != "" {
		t.Fatalf("capabilities: code=%d stderr=%q", code, stderr)
	}
	expected := `rulefloor dev capabilities
machine schemas: rulefloor.version.v1, rulefloor.validation.v1, rulefloor.capabilities.v1, rulefloor.covers.v1, rulefloor.ledger-diff.v1
test kinds: go-test (static, execute); playwright (static); vitest (static)
validation modes: static, execute
proof kinds: legacy_manual, manual_observation, mutation_observation, ci_reference
ledger features: FLOOR, RED-PROOFS, six-column-ledger, proof-v1, covers-v1, binding-v1, exact-symbol-reach, REPAIRED-FIXTURES
structural reach: gograph exact stable IDs for go-test (possible is insufficient)
commands: amend, arm, capabilities, check, covers, declare, diff, help, init, ledger-diff, list, prove, redproofs, rehash, repair-fixture-row, show, unarmed, unproved, validate, version
execution: persisted policy; legacy profiles compatible; static never executes; execute never falls back to static
`
	if stdout != expected {
		t.Fatalf("human capabilities changed\nactual:\n%s\nexpected:\n%s", stdout, expected)
	}
}

func TestCapabilitiesJSONMatchesV1Fixture(t *testing.T) {
	code, stdout, stderr := runSeparate("capabilities", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("capabilities JSON: code=%d stderr=%q", code, stderr)
	}
	expected, err := os.ReadFile(filepath.Join("testdata", "machine", "rulefloor.capabilities.v1.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if stdout != string(expected) {
		t.Fatalf("capabilities document changed\nactual:   %s\nexpected: %s", stdout, expected)
	}

	var result binarycap.Result
	decodeSingleJSON(t, stdout, &result)
	want := binarycap.New("dev", []string{
		"amend", "arm", "capabilities", "check", "covers", "declare", "diff", "help", "init", "ledger-diff", "list", "prove",
		"redproofs", "rehash", "repair-fixture-row", "show", "unarmed", "unproved", "validate", "version",
	})
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("capabilities = %+v, want %+v", result, want)
	}
}

func TestCapabilitiesAreIndependentOfCurrentRepository(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	runJSON := func() string {
		t.Helper()
		code, stdout, stderr := runSeparate("capabilities", "--json")
		if code != 0 || stderr != "" {
			t.Fatalf("capabilities outside repository: code=%d stderr=%q", code, stderr)
		}
		return stdout
	}

	withoutLedger := runJSON()
	ledgerPath := filepath.Join(directory, ledgerFile)
	if err := os.WriteFile(ledgerPath, []byte("not a Rulefloor ledger\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withMalformedLedger := runJSON()
	if err := os.WriteFile(ledgerPath, []byte("FLOOR: 999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withChangedLedger := runJSON()
	if err := os.Remove(ledgerPath); err != nil {
		t.Fatal(err)
	}
	afterRemoval := runJSON()

	if withoutLedger != withMalformedLedger || withoutLedger != withChangedLedger || withoutLedger != afterRemoval {
		t.Fatal("capabilities output changed with current-directory ledger state")
	}
	if code, _, stderr := runSeparate("capabilities", "--repo", directory); code != 2 || !strings.Contains(stderr, "not valid for capabilities") {
		t.Fatalf("capabilities accepted --repo: code=%d stderr=%q", code, stderr)
	}
}

func TestCapabilitiesVersionMatchesVersionJSON(t *testing.T) {
	versionCode, versionJSON, versionStderr := runSeparate("version", "--json")
	capabilitiesCode, capabilitiesJSON, capabilitiesStderr := runSeparate("capabilities", "--json")
	if versionCode != 0 || capabilitiesCode != 0 || versionStderr != "" || capabilitiesStderr != "" {
		t.Fatalf("version/capabilities exits: version=%d/%q capabilities=%d/%q", versionCode, versionStderr, capabilitiesCode, capabilitiesStderr)
	}
	var versionResult VersionResult
	var capabilitiesResult binarycap.Result
	decodeSingleJSON(t, versionJSON, &versionResult)
	decodeSingleJSON(t, capabilitiesJSON, &capabilitiesResult)
	if capabilitiesResult.RulefloorVersion != versionResult.Version {
		t.Fatalf("capabilities version %q != version version %q", capabilitiesResult.RulefloorVersion, versionResult.Version)
	}
}
