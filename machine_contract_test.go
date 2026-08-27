package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	machine "github.com/ozgurcd/rulefloor/internal/validation"
)

func TestMachineV1GoldenDocuments(t *testing.T) {
	proofFingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name  string
		file  string
		value any
	}{
		{
			name: "version",
			file: "rulefloor.version.v1.golden.json",
			value: machine.VersionResult{
				SchemaVersion: machine.VersionSchemaVersion,
				Version:       "dev",
			},
		},
		{
			name: "validation",
			file: "rulefloor.validation.v1.golden.json",
			value: machine.Result{
				SchemaVersion:    machine.SchemaVersion,
				Command:          "validate",
				RulefloorVersion: "dev",
				GeneratedAt:      "2026-08-21T12:00:00Z",
				Repository: machine.Repository{
					Root:              "/repo",
					LedgerPath:        "/repo/RULE-FLOOR.md",
					LedgerFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				},
				Request: machine.RequestView{RuleID: "MACHINE-1", Mode: "static", Profile: ""},
				Rule: machine.Rule{
					Exists:           true,
					Armed:            true,
					EnforcedBy:       "go-test",
					CheckFile:        "machine_test.go",
					DeclaredProfile:  "unit",
					RedProofStatus:   machine.RedProofPresent,
					TestFingerprint:  machine.Fingerprint{Expected: "abcdef012345", Actual: "abcdef012345"},
					ProofFingerprint: &proofFingerprint,
				},
				Evaluation: machine.Evaluation{
					Outcome:         machine.OutcomePass,
					StaticIntegrity: machine.StatusPass,
					Execution:       machine.Execution{Status: machine.StatusNotRequested},
					Reason:          "rule_passed",
					Diagnostics:     []machine.Diagnostic{},
				},
			},
		},
		{
			name: "validation protected reach",
			file: "rulefloor.validation.v1.protected.golden.json",
			value: machine.Result{
				SchemaVersion:    machine.SchemaVersion,
				Command:          "validate",
				RulefloorVersion: "dev",
				GeneratedAt:      "2026-08-21T12:00:00Z",
				Repository: machine.Repository{
					Root: "/repo", LedgerPath: "/repo/RULE-FLOOR.md",
					LedgerFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				},
				Request: machine.RequestView{RuleID: "MACHINE-1", Mode: "static", Profile: ""},
				Rule: machine.Rule{
					Exists: true, Armed: true, EnforcedBy: "go-test", CheckFile: "machine_test.go", DeclaredProfile: "unit",
					RedProofStatus:   machine.RedProofPresent,
					TestFingerprint:  machine.Fingerprint{Expected: "abcdef012345", Actual: "abcdef012345"},
					ProofFingerprint: &proofFingerprint, ProtectedSymbols: []string{"example.com/product::Guard"},
				},
				Evaluation: machine.Evaluation{
					Outcome: machine.OutcomePass, StaticIntegrity: machine.StatusPass,
					Execution: machine.Execution{Status: machine.StatusNotRequested}, Reason: "rule_passed", Diagnostics: []machine.Diagnostic{},
					StructuralReach: &machine.StructuralReach{
						Required: true, Status: machine.StatusPass, TestSymbol: "example.com/product::TestGuard",
						Symbols: []machine.ProtectedSymbol{{StableID: "example.com/product::Guard", Resolution: "exact"}},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var actual bytes.Buffer
			if err := writeJSON(&actual, test.value); err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(filepath.Join("testdata", "machine", test.file))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual.Bytes(), expected) {
				t.Fatalf("machine document changed\nactual:   %s\nexpected: %s", actual.Bytes(), expected)
			}
		})
	}
}
