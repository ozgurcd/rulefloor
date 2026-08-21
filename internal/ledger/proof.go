package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type ProofKind string

const (
	ProofKindLegacyManual        ProofKind = "legacy_manual"
	ProofKindManualObservation   ProofKind = "manual_observation"
	ProofKindMutationObservation ProofKind = "mutation_observation"
	ProofKindCIReference         ProofKind = "ci_reference"
)

type Proof struct {
	Text       string
	RecordedAt string
	Kind       ProofKind
	Reference  string
	Structured bool
	raw        string
}

var (
	structuredProofRe = regexp.MustCompile(`^\[proof-v1 kind=([a-z_]+)(?: ref=([^\]\s]+))?\] (.+)$`)
	rfc3339ProofRe    = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})\b`)
	dateProofRe       = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
)

func ValidProofKind(kind ProofKind) bool {
	switch kind {
	case ProofKindManualObservation, ProofKindMutationObservation, ProofKindCIReference:
		return true
	default:
		return false
	}
}

func ValidateProofReference(reference string) error {
	if reference == "" {
		return nil
	}
	if len(reference) > 2048 || strings.ContainsAny(reference, " \t\r\n|[]") {
		return fmt.Errorf("proof reference must be one absolute HTTP(S) URL without whitespace, brackets, or '|'")
	}
	if strings.Contains(reference, "#") {
		return fmt.Errorf("proof reference must not contain credentials or a fragment")
	}
	parsed, err := url.ParseRequestURI(reference)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return fmt.Errorf("proof reference must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("proof reference must not contain credentials or a fragment")
	}
	return nil
}

func NewProof(text string, kind ProofKind, reference string) (Proof, error) {
	text = strings.TrimSpace(text)
	if text == "" || text == "-" {
		return Proof{}, fmt.Errorf("proof text must be a real observation record, not empty or '-'")
	}
	if !ValidProofKind(kind) {
		return Proof{}, fmt.Errorf("invalid proof kind %q (want manual_observation, mutation_observation, or ci_reference)", kind)
	}
	if err := ValidateProofReference(reference); err != nil {
		return Proof{}, err
	}
	if kind == ProofKindCIReference && reference == "" {
		return Proof{}, fmt.Errorf("proof kind ci_reference requires --proof-ref")
	}
	proof := Proof{Text: text, Kind: kind, Reference: reference, Structured: true}
	proof.RecordedAt = recordedProofTime(text)
	proof.raw = proof.CanonicalText()
	return proof, nil
}

func ParseProof(cell string) (Proof, error) {
	if cell == "-" {
		return Proof{raw: cell}, nil
	}
	if strings.HasPrefix(cell, "[proof-v1 ") {
		match := structuredProofRe.FindStringSubmatch(cell)
		if match == nil {
			return Proof{}, fmt.Errorf("malformed proof-v1 metadata")
		}
		proof, err := NewProof(match[3], ProofKind(match[1]), match[2])
		if err != nil {
			return Proof{}, err
		}
		proof.raw = cell
		return proof, nil
	}
	return Proof{
		Text:       cell,
		RecordedAt: recordedProofTime(cell),
		Kind:       ProofKindLegacyManual,
		raw:        cell,
	}, nil
}

func (p Proof) Present() bool { return p.raw != "" && p.raw != "-" }

func (p Proof) GenuineRecord() bool {
	if !p.Present() || strings.HasPrefix(strings.TrimSpace(p.Text), "blocked:") {
		return false
	}
	return p.Structured || p.RecordedAt != ""
}

func (p Proof) CanonicalText() string {
	if !p.Structured {
		return p.Text
	}
	prefix := "[proof-v1 kind=" + string(p.Kind)
	if p.Reference != "" {
		prefix += " ref=" + p.Reference
	}
	return prefix + "] " + p.Text
}

func (p Proof) Fingerprint() string {
	text := p.raw
	if text == "" {
		text = p.CanonicalText()
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func recordedProofTime(text string) string {
	if candidate := rfc3339ProofRe.FindString(text); candidate != "" {
		if _, err := time.Parse(time.RFC3339Nano, candidate); err == nil {
			return candidate
		}
	}
	if candidate := dateProofRe.FindString(text); candidate != "" {
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return ""
}
