package ledger

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ozgurcd/rulefloor/internal/model"
)

func TestBindingMetadataRoundTripPreservesLogicalProofAndSixColumns(t *testing.T) {
	proof := "2026-08-27 watched exact reach fail"
	before, err := ParseProof(proof)
	if err != nil {
		t.Fatal(err)
	}
	ledger := &Ledger{Floor: 1, HasRedProofs: true, RedProofs: 1, Rows: []Row{{
		ID: "REACH-1", Rule: "Protected behavior remains reached.", EnforcedBy: "go-test",
		Check: "reach_test.go @ unit", RedProof: proof, Hash: "abcdef012345",
		CoveredSymbols: []string{"example.com/product::Guard"}, ExecutionPolicy: model.ExecutionStatic,
		ReachabilityPolicy: model.ReachabilityExact, TestSymbol: "example.com/product::TestGuard",
	}}}
	serialized := Serialize(ledger)
	if !strings.Contains(serialized, "rulefloor-binding-v1:") || !strings.Contains(serialized, "rulefloor-covers-v1:") {
		t.Fatalf("metadata tokens missing:\n%s", serialized)
	}
	if strings.Contains(serialized, "| execution-policy |") || strings.Contains(serialized, "| protected-symbols |") {
		t.Fatalf("metadata changed the six-column table:\n%s", serialized)
	}
	parsed, err := Parse(serialized)
	if err != nil {
		t.Fatal(err)
	}
	row := parsed.Rows[0]
	if row.RedProof != proof || row.ExecutionPolicy != model.ExecutionStatic || row.ReachabilityPolicy != model.ReachabilityExact || row.TestSymbol != "example.com/product::TestGuard" {
		t.Fatalf("round-trip row = %+v", row)
	}
	after, err := ParseProof(row.RedProof)
	if err != nil {
		t.Fatal(err)
	}
	if before.Fingerprint() != after.Fingerprint() {
		t.Fatalf("binding metadata changed proof fingerprint: %s -> %s", before.Fingerprint(), after.Fingerprint())
	}
}

func TestDeclaredExactReachMetadataDoesNotInventExecutionOrProof(t *testing.T) {
	ledger := &Ledger{Floor: 1, HasRedProofs: true, Rows: []Row{{
		ID: "REACH-1", Rule: "Protected behavior remains reached.", EnforcedBy: "-", Check: "NONE",
		RedProof: "-", Hash: "-", CoveredSymbols: []string{"example.com/product::Guard"},
		ReachabilityPolicy: model.ReachabilityExact,
	}}}
	parsed, err := Parse(Serialize(ledger))
	if err != nil {
		t.Fatal(err)
	}
	row := parsed.Rows[0]
	if row.ExecutionPolicy != "" || row.TestSymbol != "" || row.RedProof != "-" || parsed.MeasuredRedProofs() != 0 {
		t.Fatalf("declared metadata invented binding state: %+v", row)
	}
}

func TestBindingMetadataRejectsInvalidPolicyAndMissingTestIdentity(t *testing.T) {
	encode := func(document string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(document))
	}
	for _, cell := range []string{
		"watched <!-- rulefloor-binding-v1:" + encode(`{"execution_policy":"sometimes"}`) + " -->",
		"watched <!-- rulefloor-binding-v1:" + encode(`{"execution_policy":"execute","reachability_policy":"exact"}`) + " -->",
	} {
		proof, metadata, err := splitProofAndBindingMetadata(cell)
		if err != nil {
			continue
		}
		row := Row{ID: "REACH-1", Rule: "A rule.", EnforcedBy: "go-test", Check: "reach_test.go @ unit", RedProof: proof, Hash: "abcdef012345", CoveredSymbols: []string{"example.com/product::Guard"}}
		if err := applyAndValidateBindingMetadata(&row, metadata); err == nil {
			t.Fatalf("invalid metadata accepted: %s", cell)
		}
	}
}
