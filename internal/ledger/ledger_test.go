package ledger

import (
	"strings"
	"testing"
)

func TestParseSerializeWithRepairedFixtureAudit(t *testing.T) {
	model := &Ledger{
		Floor:            2,
		HasRedProofs:     true,
		RepairedFixtures: []string{"FIXTURE-1"},
		Rows:             []Row{{ID: "REAL-1", Rule: "A real invariant.", EnforcedBy: "-", Check: "NONE", RedProof: "-", Hash: "-"}},
	}
	parsed, err := Parse(Serialize(model))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.EffectiveCount() != 2 || !parsed.IsRepairedFixture("FIXTURE-1") || parsed.Find("FIXTURE-1") != nil {
		t.Fatalf("parsed model = %+v", parsed)
	}
}

func TestParseRejectsRepairedFixtureThatIsStillARow(t *testing.T) {
	data := "FLOOR: 1\nREPAIRED-FIXTURES: FIXTURE-1\n\n" +
		"| ID | one-sentence rule | enforced-by | check | red-proof | hash |\n" +
		"|---|---|---|---|---|---|\n" +
		"| FIXTURE-1 | Not real. | - | NONE | - | - |\n"
	_, err := Parse(data)
	if err == nil || !strings.Contains(err.Error(), "also recorded") {
		t.Fatalf("error = %v", err)
	}
}
