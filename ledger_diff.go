package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ozgurcd/rulefloor/internal/ledgerdiff"
)

type ledgerDiffResult struct {
	SchemaVersion    string                        `json:"schema_version"`
	Command          string                        `json:"command"`
	RulefloorVersion string                        `json:"rulefloor_version"`
	Status           string                        `json:"status"`
	BaseRef          string                        `json:"base_ref"`
	BaseCommit       string                        `json:"base_commit,omitempty"`
	Before           *ledgerdiff.HeaderState       `json:"before,omitempty"`
	After            *ledgerdiff.HeaderState       `json:"after,omitempty"`
	HeadersChanged   bool                          `json:"headers_changed"`
	HeaderChanges    []ledgerdiff.HeaderChangeKind `json:"header_changes"`
	Rules            []ledgerdiff.RuleChange       `json:"rules"`
	TotalRuleChanges int                           `json:"total_rule_changes"`
	Truncated        bool                          `json:"truncated"`
	Error            string                        `json:"error,omitempty"`
}

func cmdLedgerDiff(repo, baseRef string, jsonOutput bool, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	comparison, err := ledgerdiff.Compare(ctx, ledgerdiff.ExecRunner{}, repo, baseRef)
	if err != nil {
		if !jsonOutput {
			return fatalf("CANNOT-EVALUATE: ledger-diff: %v", err)
		}
		result := ledgerDiffResult{
			SchemaVersion: ledgerdiff.SchemaVersion, Command: "ledger-diff", RulefloorVersion: version,
			Status: "cannot_evaluate", BaseRef: baseRef, HeaderChanges: []ledgerdiff.HeaderChangeKind{}, Rules: []ledgerdiff.RuleChange{}, Error: err.Error(),
		}
		if writeErr := writeJSON(stdout, result); writeErr != nil {
			return fatalf("write ledger-diff JSON: %v", writeErr)
		}
		return silentExit{code: 2}
	}
	status := "same"
	exitCode := 0
	if comparison.Different() {
		status = "different"
		exitCode = 1
	}
	result := newLedgerDiffResult(comparison, status)
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			return fatalf("write ledger-diff JSON: %v", err)
		}
	} else {
		writeLedgerDiffHuman(stdout, comparison)
	}
	if exitCode != 0 {
		return silentExit{code: exitCode}
	}
	return nil
}

func newLedgerDiffResult(comparison ledgerdiff.Comparison, status string) ledgerDiffResult {
	before, after := comparison.Before, comparison.After
	headerChanges := comparison.HeaderChanges
	if headerChanges == nil {
		headerChanges = []ledgerdiff.HeaderChangeKind{}
	}
	return ledgerDiffResult{
		SchemaVersion: ledgerdiff.SchemaVersion, Command: "ledger-diff", RulefloorVersion: version,
		Status: status, BaseRef: comparison.BaseRef, BaseCommit: comparison.BaseCommit,
		Before: &before, After: &after, HeadersChanged: comparison.HeadersChanged, HeaderChanges: headerChanges, Rules: comparison.Rules,
		TotalRuleChanges: comparison.TotalRuleChanges, Truncated: comparison.Truncated,
	}
}

func writeLedgerDiffHuman(output io.Writer, comparison ledgerdiff.Comparison) {
	if !comparison.Different() {
		fmt.Fprintf(output, "no ledger changes from %s (%.12s)\n", comparison.BaseRef, comparison.BaseCommit)
		return
	}
	fmt.Fprintf(output, "ledger differs from %s (%.12s)\n", comparison.BaseRef, comparison.BaseCommit)
	if comparison.HeadersChanged {
		headerLabels := make([]string, len(comparison.HeaderChanges))
		for i, kind := range comparison.HeaderChanges {
			headerLabels[i] = string(kind)
		}
		fmt.Fprintf(output, "headers changed (%s): FLOOR %d -> %d, RED-PROOFS %s -> %s, REPAIRED-FIXTURES %d -> %d\n",
			strings.Join(headerLabels, ", "),
			comparison.Before.Floor, comparison.After.Floor,
			optionalInt(comparison.Before.RedProofs), optionalInt(comparison.After.RedProofs),
			comparison.Before.RepairedFixtureCount, comparison.After.RepairedFixtureCount)
	}
	for _, change := range comparison.Rules {
		labels := make([]string, len(change.Changes))
		for i, kind := range change.Changes {
			labels[i] = string(kind)
		}
		fmt.Fprintf(output, "%s: %s\n", change.RuleID, strings.Join(labels, ", "))
		if change.BeforeSentenceExcerpt != "" {
			fmt.Fprintf(output, "  before: %q\n", change.BeforeSentenceExcerpt)
		}
		if change.AfterSentenceExcerpt != "" {
			fmt.Fprintf(output, "  after:  %q\n", change.AfterSentenceExcerpt)
		}
	}
	if comparison.Truncated {
		fmt.Fprintf(output, "... %d additional rule changes omitted\n", comparison.TotalRuleChanges-len(comparison.Rules))
	}
}

func optionalInt(value *int) string {
	if value == nil {
		return "absent"
	}
	return fmt.Sprintf("%d", *value)
}
