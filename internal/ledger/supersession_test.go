package ledger

import "testing"

func TestProofSupersessionRoundTripAndFingerprintLink(t *testing.T) {
	previous, err := NewProof("2026-08-24 original observation", ProofKindManualObservation, "")
	if err != nil {
		t.Fatal(err)
	}
	next, err := NewProof("2026-08-25 re-watched extension", ProofKindMutationObservation, "")
	if err != nil {
		t.Fatal(err)
	}
	next, err = next.Superseding(previous)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseProof(next.CanonicalText())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.SupersedesFingerprint != previous.Fingerprint() || parsed.Fingerprint() != next.Fingerprint() {
		t.Fatalf("parsed=%+v previous=%s next=%s", parsed, previous.Fingerprint(), next.Fingerprint())
	}
	if _, err := ParseProof("supersedes-sha256:bad observation"); err == nil {
		t.Fatal("malformed supersession metadata was accepted")
	}
}

func TestProofSupersessionDoesNotInventRecordedTime(t *testing.T) {
	previous, err := NewProof("original observation without a date", ProofKindManualObservation, "")
	if err != nil {
		t.Fatal(err)
	}
	next, err := NewProof("re-watched observation without a date", ProofKindMutationObservation, "")
	if err != nil {
		t.Fatal(err)
	}
	next, err = next.Superseding(previous)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseProof(next.CanonicalText())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.RecordedAt != "" {
		t.Fatalf("supersession invented recorded time %q", parsed.RecordedAt)
	}
}

func TestProofSupersessionRequiresGenuineNewRecord(t *testing.T) {
	previous, err := NewProof("original observation", ProofKindManualObservation, "")
	if err != nil {
		t.Fatal(err)
	}
	next, err := ParseProof("dateless legacy replacement")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Superseding(previous); err == nil {
		t.Fatal("non-genuine superseding proof was accepted")
	}
}
