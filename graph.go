package main

import (
	"context"
	"errors"
	"strings"
	"time"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
	gographevidence "github.com/ozgurcd/rulefloor/internal/gograph"
	"github.com/ozgurcd/rulefloor/internal/model"
	"github.com/ozgurcd/rulefloor/internal/reach"
)

type graphClient interface {
	reach.Resolver
	reach.Verifier
}

var newGraphClient = func() graphClient { return gographevidence.New("gograph") }

func stableSymbolMode(symbols []string) (allStable, mixed bool) {
	if len(symbols) == 0 {
		return false, false
	}
	stable := 0
	for _, symbol := range symbols {
		if strings.Contains(symbol, "::") {
			stable++
		}
	}
	return stable == len(symbols), stable > 0 && stable < len(symbols)
}

func prepareCoveredSymbols(command, repo string, symbols []string) ([]string, model.ReachabilityPolicy, error) {
	stable, mixed := stableSymbolMode(symbols)
	if mixed {
		return nil, "", fatalf("%s: --covers must use either only Gograph stable identities or only legacy labels", command)
	}
	if !stable {
		return symbols, "", nil
	}
	resolved, err := resolveProtectedSymbols(repo, symbols, newGraphClient())
	if err != nil {
		return nil, "", err
	}
	return resolved, model.ReachabilityExact, nil
}

func resolveProtectedSymbols(repo string, symbols []string, client graphClient) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resolved, err := client.Resolve(ctx, repo, symbols)
	if err != nil {
		return nil, graphCannotEvaluate(err)
	}
	return resolved, nil
}

func verifyInitialProtectedReach(repo string, row *Row, testName string, client graphClient) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	testSymbol, err := client.ResolveTest(ctx, repo, testName)
	if err != nil {
		return graphCannotEvaluate(err)
	}
	row.TestSymbol = testSymbol
	evaluation, err := checkengine.EvaluateProtectedReach(ctx, client, repo, (*model.Row)(row))
	if err != nil {
		return graphCannotEvaluate(err)
	}
	if len(evaluation.Issues) > 0 {
		return failf("refusing exact protected-symbol binding: %s", evaluation.Issues[0].Message)
	}
	return nil
}

func graphCannotEvaluate(err error) error {
	var evidenceErr *reach.EvidenceError
	if errors.As(err, &evidenceErr) {
		if evidenceErr.Kind == reach.ErrorAmbiguous {
			return fatalf("CANNOT-EVALUATE: ambiguous symbol identity: %s", evidenceErr.Message)
		}
		return fatalf("CANNOT-EVALUATE: graph evidence unavailable or insufficient: %s", evidenceErr.Message)
	}
	return fatalf("CANNOT-EVALUATE: graph evidence unavailable or insufficient: %v", err)
}

type bindingSpec struct {
	Kind    string
	File    string
	Profile string
}

func parseExecutionPolicy(command, value string, set bool, binding bindingSpec) (model.ExecutionPolicy, error) {
	if set {
		policy := model.ExecutionPolicy(value)
		if policy != model.ExecutionStatic && policy != model.ExecutionExecute {
			return "", fatalf("%s: --execution must be static or execute", command)
		}
		if policy == model.ExecutionExecute && binding.Kind != "go-test" {
			return "", fatalf("%s: execution is unsupported for %s bindings", command, binding.Kind)
		}
		return policy, nil
	}
	legacy, err := model.InterpretBinding(&model.Row{EnforcedBy: binding.Kind, Check: binding.File + " @ " + binding.Profile})
	if err != nil {
		return "", fatalf("%s: %v", command, err)
	}
	return legacy.Execution, nil
}
