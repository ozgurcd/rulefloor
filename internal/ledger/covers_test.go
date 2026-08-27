package ledger

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestSixColumnLedgerWithoutCoveredSymbolsIsByteStable(t *testing.T) {
	data := "FLOOR: 1\nRED-PROOFS: 1\n\n" +
		"| ID | one-sentence rule | enforced-by | check | red-proof | hash |\n" +
		"|---|---|---|---|---|---|\n" +
		"| R-1 | A rule. | go-test | rule_test.go @ unit | 2026-08-25 watched FAIL | abcdef012345 |\n"
	model, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Rows[0].CoveredSymbols) != 0 {
		t.Fatalf("covered symbols = %v, want empty", model.Rows[0].CoveredSymbols)
	}
	if got := Serialize(model); got != data {
		t.Fatalf("legacy six-column ledger changed on round trip\ngot:\n%s\nwant:\n%s", got, data)
	}
}

func TestCoveredSymbolsRoundTripSeparatesProofMetadata(t *testing.T) {
	proofCell := "2026-08-25 watched FAIL"
	proofBefore, err := ParseProof(proofCell)
	if err != nil {
		t.Fatal(err)
	}
	model := &Ledger{
		Floor: 1, RedProofs: 1, HasRedProofs: true,
		Rows: []Row{{
			ID: "R-1", Rule: "A rule.", EnforcedBy: "go-test", Check: "rule_test.go @ unit",
			RedProof: proofCell, Hash: "abcdef012345", CoveredSymbols: []string{"pkg.Auth", "pkg.Refresh"},
		}},
	}
	serialized := Serialize(model)
	if strings.Count(serialized, "|") == 0 || !strings.Contains(serialized, "<!-- rulefloor-covers-v1:") {
		t.Fatalf("serialized ledger lacks covers token:\n%s", serialized)
	}
	parsed, err := Parse(serialized)
	if err != nil {
		t.Fatal(err)
	}
	row := parsed.Rows[0]
	if row.RedProof != proofCell || !slices.Equal(row.CoveredSymbols, []string{"pkg.Auth", "pkg.Refresh"}) {
		t.Fatalf("parsed row = %+v", row)
	}
	proofAfter, err := ParseProof(row.RedProof)
	if err != nil {
		t.Fatal(err)
	}
	if proofAfter.Fingerprint() != proofBefore.Fingerprint() {
		t.Fatalf("proof fingerprint changed: %s -> %s", proofBefore.Fingerprint(), proofAfter.Fingerprint())
	}
}

func TestCoveredSymbolsOnDeclaredRowRemainUnproved(t *testing.T) {
	model := &Ledger{
		Floor: 1, RedProofs: 0, HasRedProofs: true,
		Rows: []Row{{
			ID: "R-1", Rule: "A rule.", EnforcedBy: "-", Check: "NONE", RedProof: "-", Hash: "-",
			CoveredSymbols: []string{"pkg.Func"},
		}},
	}
	parsed, err := Parse(Serialize(model))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Rows[0].RedProof != "-" || parsed.MeasuredRedProofs() != 0 {
		t.Fatalf("declared covers-only row counted as proof: %+v", parsed)
	}
}

func TestParseCoveredSymbolsCanonicalizesAndValidates(t *testing.T) {
	symbols, err := ParseCoveredSymbols("pkg.Z, pkg.A")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(symbols, []string{"pkg.A", "pkg.Z"}) {
		t.Fatalf("symbols = %v", symbols)
	}
	for _, value := range []string{"pkg.A,pkg.A", "pkg.A,,pkg.B", "pkg.Bad Name", "pkg.Bad\tName"} {
		if _, err := ParseCoveredSymbols(value); err == nil {
			t.Fatalf("ParseCoveredSymbols(%q) succeeded", value)
		}
	}
}

func TestCoveredSymbolMetadataRejectsMalformedOrNonCanonicalTokens(t *testing.T) {
	encode := func(symbols []string) string {
		t.Helper()
		payload, err := json.Marshal(symbols)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(payload)
	}
	for _, cell := range []string{
		"- <!-- rulefloor-covers-v1:not-base64! -->",
		"- <!-- rulefloor-covers-v1:Wz== -->",
		"- <!-- rulefloor-covers-v1:" + encode([]string{}) + " -->",
		"- <!-- rulefloor-covers-v1:" + encode([]string{"pkg.Z", "pkg.A"}) + " -->",
		"- <!-- rulefloor-covers-v1:" + encode([]string{"pkg.A", "pkg.A"}) + " -->",
		"- <!-- rulefloor-covers-v1:" + encode([]string{"pkg.Bad Name"}) + " -->",
		"- <!-- rulefloor-covers-v1:" + encode([]string{"pkg.A"}) + " --> trailing",
		"- <!-- rulefloor-covers-v1:" + encode([]string{"pkg.A"}) + " --> <!-- rulefloor-covers-v1:" + encode([]string{"pkg.B"}) + " -->",
	} {
		if _, _, err := splitProofAndCoveredSymbols(cell); err == nil {
			t.Fatalf("malformed cell accepted: %q", cell)
		}
	}
}
