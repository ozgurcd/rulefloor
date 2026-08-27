package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestLegacyProofCompatibilityDoesNotInventMetadata(t *testing.T) {
	cell := "watched the assertion fail and restored it"
	proof, err := ParseProof(cell)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Kind != ProofKindLegacyManual || proof.Structured || proof.Reference != "" || proof.RecordedAt != "" || proof.CanonicalText() != cell {
		t.Fatalf("proof = %+v", proof)
	}
	sum := sha256.Sum256([]byte(cell))
	if proof.Fingerprint() != hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint = %q", proof.Fingerprint())
	}
}

func TestStructuredProofRoundTripAndFingerprintDeterminism(t *testing.T) {
	proof, err := NewProof(
		"2026-08-21T14:15:16Z inverted the assertion and observed failure",
		ProofKindMutationObservation,
		"https://ci.example.test/runs/42",
	)
	if err != nil {
		t.Fatal(err)
	}
	cell := proof.CanonicalText()
	parsed, err := ParseProof(cell)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Kind != ProofKindMutationObservation || parsed.Reference != "https://ci.example.test/runs/42" || parsed.RecordedAt != "2026-08-21T14:15:16Z" || !parsed.GenuineRecord() {
		t.Fatalf("parsed proof = %+v", parsed)
	}
	if proof.Fingerprint() != parsed.Fingerprint() || len(parsed.Fingerprint()) != 64 {
		t.Fatalf("fingerprints = %q / %q", proof.Fingerprint(), parsed.Fingerprint())
	}

	model := &Ledger{Floor: 1, RedProofs: 1, HasRedProofs: true, Rows: []Row{{
		ID: "PROOF-1", Rule: "A structured proof record survives.", EnforcedBy: "go-test",
		Check: "rule_test.go @ unit", RedProof: cell, Hash: "abcdef012345",
	}}}
	roundTrip, err := Parse(Serialize(model))
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.Rows[0].RedProof != cell || roundTrip.MeasuredRedProofs() != 1 || roundTrip.RedProofs != 1 {
		t.Fatalf("round trip = %+v", roundTrip)
	}
}

func TestProofKindAndReferenceValidation(t *testing.T) {
	if _, err := NewProof("observed", ProofKind("arbitrary"), ""); err == nil || !strings.Contains(err.Error(), "invalid proof kind") {
		t.Fatalf("invalid kind error = %v", err)
	}
	if _, err := NewProof("observed", ProofKindManualObservation, "not a URL"); err == nil || !strings.Contains(err.Error(), "absolute HTTP") {
		t.Fatalf("invalid reference error = %v", err)
	}
	if _, err := NewProof("observed", ProofKindCIReference, ""); err == nil || !strings.Contains(err.Error(), "requires --proof-ref") {
		t.Fatalf("missing CI reference error = %v", err)
	}
	for _, reference := range []string{"https://user:secret@example.test/run/1", "https://ci.example.test/run/1#token"} {
		if _, err := NewProof("observed", ProofKindManualObservation, reference); err == nil || !strings.Contains(err.Error(), "credentials or a fragment") {
			t.Fatalf("unsafe reference %q error = %v", reference, err)
		}
	}
}

func TestStructuredProofWithoutDateKeepsRecordedTimeUnknown(t *testing.T) {
	proof, err := NewProof("watched the failure", ProofKindManualObservation, "")
	if err != nil {
		t.Fatal(err)
	}
	if proof.RecordedAt != "" || proof.Reference != "" {
		t.Fatalf("invented metadata: %+v", proof)
	}
	if !proof.GenuineRecord() {
		t.Fatal("a valid structured observation record should be protected from ordinary replacement")
	}
}
