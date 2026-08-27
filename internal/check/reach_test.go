package check

import (
	"context"
	"errors"
	"testing"

	"github.com/ozgurcd/rulefloor/internal/model"
	"github.com/ozgurcd/rulefloor/internal/reach"
)

type verifierFunc func(context.Context, reach.Request) (reach.Result, error)

func (f verifierFunc) Verify(ctx context.Context, request reach.Request) (reach.Result, error) {
	return f(ctx, request)
}

func TestEvaluateProtectedReach(t *testing.T) {
	row := &model.Row{
		EnforcedBy: "go-test", ReachabilityPolicy: model.ReachabilityExact,
		TestSymbol: "module/pkg::TestGuard", CoveredSymbols: []string{"module/pkg::Guard"},
	}
	for _, test := range []struct {
		name       string
		resolution reach.Resolution
		wantCode   string
	}{
		{name: "exact", resolution: reach.ResolutionExact},
		{name: "possible", resolution: reach.ResolutionPossible, wantCode: "protected_symbol_possible"},
		{name: "missing", resolution: reach.ResolutionMissing, wantCode: "protected_symbol_unreached"},
	} {
		t.Run(test.name, func(t *testing.T) {
			evaluation, err := EvaluateProtectedReach(context.Background(), verifierFunc(func(_ context.Context, request reach.Request) (reach.Result, error) {
				return reach.Result{TestSymbol: request.TestSymbol, Symbols: []reach.Symbol{{StableID: request.ProtectedSymbols[0], Resolution: test.resolution}}}, nil
			}), ".", row)
			if err != nil {
				t.Fatal(err)
			}
			if test.wantCode == "" && len(evaluation.Issues) != 0 {
				t.Fatalf("unexpected issues: %+v", evaluation.Issues)
			}
			if test.wantCode != "" && (len(evaluation.Issues) != 1 || evaluation.Issues[0].Code != test.wantCode) {
				t.Fatalf("issues = %+v, want code %q", evaluation.Issues, test.wantCode)
			}
		})
	}
}

func TestEvaluateProtectedReachPropagatesEvidenceFailure(t *testing.T) {
	row := &model.Row{EnforcedBy: "go-test", ReachabilityPolicy: model.ReachabilityExact, TestSymbol: "module/pkg::TestGuard", CoveredSymbols: []string{"module/pkg::Guard"}}
	want := &reach.EvidenceError{Kind: reach.ErrorUnavailable, Message: "missing graph"}
	_, err := EvaluateProtectedReach(context.Background(), verifierFunc(func(context.Context, reach.Request) (reach.Result, error) {
		return reach.Result{}, want
	}), ".", row)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestEvaluateProtectedReachLeavesLegacyCoversUnchanged(t *testing.T) {
	evaluation, err := EvaluateProtectedReach(context.Background(), nil, ".", &model.Row{EnforcedBy: "go-test", CoveredSymbols: []string{"Guard"}})
	if err != nil || evaluation.Required || len(evaluation.Issues) != 0 {
		t.Fatalf("legacy evaluation = %+v, err = %v", evaluation, err)
	}
}
