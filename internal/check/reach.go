package check

import (
	"context"
	"fmt"

	"github.com/ozgurcd/rulefloor/internal/model"
	"github.com/ozgurcd/rulefloor/internal/reach"
)

type ReachEvaluation struct {
	Required bool
	Symbols  []reach.Symbol
	Issues   []Issue
}

func EvaluateProtectedReach(ctx context.Context, verifier reach.Verifier, repo string, row *model.Row) (ReachEvaluation, error) {
	evaluation := ReachEvaluation{Required: row.ReachabilityPolicy == model.ReachabilityExact}
	if !evaluation.Required {
		return evaluation, nil
	}
	if row.EnforcedBy != "go-test" {
		return evaluation, &reach.EvidenceError{
			Kind: reach.ErrorInsufficient, Message: fmt.Sprintf("exact protected-symbol reach is unsupported for %s bindings", row.EnforcedBy),
		}
	}
	if verifier == nil {
		return evaluation, &reach.EvidenceError{Kind: reach.ErrorUnavailable, Message: "Gograph verifier is unavailable"}
	}
	result, err := verifier.Verify(ctx, reach.Request{
		Repository: repo, TestSymbol: row.TestSymbol, ProtectedSymbols: row.CoveredSymbols,
	})
	if err != nil {
		return evaluation, err
	}
	if result.TestSymbol != row.TestSymbol || len(result.Symbols) != len(row.CoveredSymbols) {
		return evaluation, &reach.EvidenceError{Kind: reach.ErrorInsufficient, Message: "Gograph returned incomplete protected-symbol evidence"}
	}
	evaluation.Symbols = result.Symbols
	for i, symbol := range result.Symbols {
		if symbol.StableID != row.CoveredSymbols[i] {
			return evaluation, &reach.EvidenceError{Kind: reach.ErrorInsufficient, Message: "Gograph returned out-of-order protected-symbol evidence"}
		}
		switch symbol.Resolution {
		case reach.ResolutionExact:
		case reach.ResolutionPossible:
			evaluation.Issues = append(evaluation.Issues, Issue{
				Code:    "protected_symbol_possible",
				Message: fmt.Sprintf("protected symbol has only possible/uncertain reachability: %s", symbol.StableID),
			})
		case reach.ResolutionMissing:
			message := fmt.Sprintf("protected symbol no longer reached: %s", symbol.StableID)
			if symbol.Detail != "" {
				message += " (" + symbol.Detail + ")"
			}
			evaluation.Issues = append(evaluation.Issues, Issue{Code: "protected_symbol_unreached", Message: message})
		default:
			return evaluation, &reach.EvidenceError{Kind: reach.ErrorInsufficient, Message: fmt.Sprintf("Gograph returned unsupported resolution %q", symbol.Resolution)}
		}
	}
	return evaluation, nil
}
