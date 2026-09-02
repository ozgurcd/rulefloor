package ledgerdiff

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ozgurcd/rulefloor/internal/ledger"
)

func TestCompareLedgersClassifiesLogicalChangesDeterministically(t *testing.T) {
	redBefore := 1
	redAfter := 2
	before := &ledger.Ledger{
		Floor: 2, HasRedProofs: true, RedProofs: redBefore, RepairedFixtures: []string{"OLD-1"},
		Rows: []ledger.Row{
			{ID: "R-2", Rule: "Removed.", EnforcedBy: "go-test", Check: "removed_test.go @ unit", RedProof: "proof", Hash: "222222222222"},
			{ID: "R-1", Rule: "Before.", EnforcedBy: "go-test", Check: "rule_test.go @ unit", RedProof: "old proof", Hash: "111111111111", CoveredSymbols: []string{"pkg.Old"}},
		},
	}
	after := &ledger.Ledger{
		Floor: 3, HasRedProofs: true, RedProofs: redAfter, RepairedFixtures: []string{"OLD-1", "OLD-2"},
		Rows: []ledger.Row{
			{ID: "R-3", Rule: "Added."},
			{ID: "R-1", Rule: "After.", EnforcedBy: "playwright", Check: "rule.spec.ts @ web", RedProof: "new proof", Hash: "aaaaaaaaaaaa", CoveredSymbols: []string{"pkg.New"}},
		},
	}

	result := CompareLedgers("main", "0123456789012345678901234567890123456789", before, after)
	if !result.Different() || !result.HeadersChanged {
		t.Fatalf("comparison = %+v", result)
	}
	wantHeaders := []HeaderChangeKind{HeaderFloorChanged, HeaderRedProofsChanged, HeaderRepairedFixturesChanged}
	if !reflect.DeepEqual(result.HeaderChanges, wantHeaders) {
		t.Fatalf("header changes = %v, want %v", result.HeaderChanges, wantHeaders)
	}
	want := []RuleChange{
		{RuleID: "R-1", Changes: []ChangeKind{ChangeSentence, ChangeBinding, ChangeProof, ChangeCoveredSymbols, ChangeTestFingerprint}, BeforeSentenceExcerpt: "Before.", AfterSentenceExcerpt: "After."},
		{RuleID: "R-2", Changes: []ChangeKind{ChangeRuleRemoved}, BeforeSentenceExcerpt: "Removed."},
		{RuleID: "R-3", Changes: []ChangeKind{ChangeRuleAdded}, AfterSentenceExcerpt: "Added."},
	}
	if !reflect.DeepEqual(result.Rules, want) {
		t.Fatalf("rules = %+v, want %+v", result.Rules, want)
	}
}

func TestCompareLedgersTreatsIdenticalLegacyLedgerAsSame(t *testing.T) {
	model := &ledger.Ledger{Floor: 1, Rows: []ledger.Row{{ID: "R-1", Rule: "Legacy.", EnforcedBy: "-", Check: "NONE", RedProof: "-", Hash: "-"}}}
	result := CompareLedgers("HEAD", "0123456789012345678901234567890123456789", model, model)
	if result.Different() || result.Rules == nil || result.HeaderChanges == nil || result.Before.RedProofs != nil || result.After.RedProofs != nil || result.TotalRuleChanges != 0 {
		t.Fatalf("comparison = %+v", result)
	}
}

func TestCompareLedgersBoundsSentenceExcerpts(t *testing.T) {
	beforeSentence := strings.Repeat("a", maxSentenceExcerptRunes+10)
	afterSentence := strings.Repeat("b", maxSentenceExcerptRunes+10)
	before := &ledger.Ledger{Rows: []ledger.Row{{ID: "R-1", Rule: beforeSentence}}}
	after := &ledger.Ledger{Rows: []ledger.Row{{ID: "R-1", Rule: afterSentence}}}
	result := CompareLedgers("main", "0123456789012345678901234567890123456789", before, after)
	change := result.Rules[0]
	if len([]rune(change.BeforeSentenceExcerpt)) != maxSentenceExcerptRunes+1 || !strings.HasSuffix(change.BeforeSentenceExcerpt, "…") ||
		len([]rune(change.AfterSentenceExcerpt)) != maxSentenceExcerptRunes+1 || !strings.HasSuffix(change.AfterSentenceExcerpt, "…") {
		t.Fatalf("change = %+v", change)
	}
}

func TestCompareLedgersBoundsRuleChanges(t *testing.T) {
	after := &ledger.Ledger{Rows: make([]ledger.Row, maxRuleChanges+2)}
	for i := range after.Rows {
		after.Rows[i] = ledger.Row{ID: fmt.Sprintf("R-%04d", i), Rule: "Added."}
	}
	result := CompareLedgers("main", "0123456789012345678901234567890123456789", &ledger.Ledger{}, after)
	if !result.Truncated || result.TotalRuleChanges != maxRuleChanges+2 || len(result.Rules) != maxRuleChanges {
		t.Fatalf("comparison = total %d, returned %d, truncated %t", result.TotalRuleChanges, len(result.Rules), result.Truncated)
	}
}

func TestCompareUsesResolvedCommitAndArgumentVectors(t *testing.T) {
	current := t.TempDir()
	ledgerText := "FLOOR: 0\n\n| ID | one-sentence rule | enforced-by | check | red-proof | hash |\n|---|---|---|---|---|---|\n"
	if err := ledger.Save(current, &ledger.Ledger{}); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{responses: []response{
		{stdout: "0123456789012345678901234567890123456789\n"},
		{stdout: ledgerText},
	}}
	result, err := Compare(context.Background(), runner, current, "--hostile-looking")
	if err != nil {
		t.Fatal(err)
	}
	if result.Different() || len(runner.calls) != 2 {
		t.Fatalf("result=%+v calls=%+v", result, runner.calls)
	}
	wantResolve := []string{"rev-parse", "--verify", "--end-of-options", "--hostile-looking^{commit}"}
	if !reflect.DeepEqual(runner.calls[0], wantResolve) {
		t.Fatalf("resolve args = %v, want %v", runner.calls[0], wantResolve)
	}
	wantShow := []string{"show", "--no-textconv", "0123456789012345678901234567890123456789:./RULE-FLOOR.md"}
	if !reflect.DeepEqual(runner.calls[1], wantShow) {
		t.Fatalf("show args = %v, want %v", runner.calls[1], wantShow)
	}
}

func TestCompareRejectsInvalidEvidence(t *testing.T) {
	current := t.TempDir()
	if err := ledger.Save(current, &ledger.Ledger{}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		base   string
		runner Runner
	}{
		{name: "empty base", base: "", runner: &recordingRunner{}},
		{name: "control character", base: "main\nnext", runner: &recordingRunner{}},
		{name: "bad commit", base: "main", runner: &recordingRunner{responses: []response{{stdout: "not-a-commit\n"}}}},
		{name: "git failure", base: "main", runner: &recordingRunner{responses: []response{{stderr: "missing", err: errors.New("exit 1")}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Compare(context.Background(), tc.runner, current, tc.base); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

type response struct {
	stdout string
	stderr string
	err    error
}

type recordingRunner struct {
	responses []response
	calls     [][]string
}

func (r *recordingRunner) Run(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(r.responses) == 0 {
		return nil, nil, errors.New("unexpected call")
	}
	response := r.responses[0]
	r.responses = r.responses[1:]
	return []byte(response.stdout), []byte(response.stderr), response.err
}
