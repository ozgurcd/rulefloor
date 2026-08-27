package gograph

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/ozgurcd/rulefloor/internal/reach"
)

type runnerFunc func(context.Context, string, ...string) ([]byte, []byte, error)

func (f runnerFunc) Run(ctx context.Context, dir string, args ...string) ([]byte, []byte, error) {
	return f(ctx, dir, args...)
}

func graphEnvelope(command, results string) []byte {
	return []byte(fmt.Sprintf(`{"schema_version":"1","command":%q,"status":"ok","results":%s,"graph_state":{"schema_version":"gograph.graph-state.v1","source":"persisted","freshness":"current","completeness":"complete","precision":"precise"}}`, command, results))
}

func TestResolveReturnsCanonicalStableIdentities(t *testing.T) {
	client := NewWithRunner(runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		query := args[1]
		return graphEnvelope("identity", fmt.Sprintf(`{"schema_version":"gograph.identity.v1","status":"exact","matches":[{"stable_id":%q,"kind":"function"}]}`, query)), nil, nil
	}))
	got, err := client.Resolve(context.Background(), t.TempDir(), []string{"module/pkg::Beta", "module/pkg::Alpha"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"module/pkg::Alpha", "module/pkg::Beta"}
	if !slices.Equal(got, want) {
		t.Fatalf("Resolve() = %v, want %v", got, want)
	}
}

func TestResolveRejectsAmbiguityAndDuplicateAliases(t *testing.T) {
	t.Run("ambiguous", func(t *testing.T) {
		client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
			return graphEnvelope("identity", `{"schema_version":"gograph.identity.v1","status":"ambiguous","matches":[]}`), nil, nil
		}))
		_, err := client.Resolve(context.Background(), t.TempDir(), []string{"Guard"})
		assertEvidenceKind(t, err, reach.ErrorAmbiguous)
	})
	t.Run("same stable identity", func(t *testing.T) {
		client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
			return graphEnvelope("identity", `{"schema_version":"gograph.identity.v1","status":"exact","matches":[{"stable_id":"module/pkg::Guard","kind":"function"}]}`), nil, nil
		}))
		_, err := client.Resolve(context.Background(), t.TempDir(), []string{"Guard", "(*Service).Guard"})
		assertEvidenceKind(t, err, reach.ErrorInsufficient)
	})
}

func TestVerifyDistinguishesExactPossibleAndMissing(t *testing.T) {
	client := NewWithRunner(runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		switch args[0] {
		case "coverage":
			return graphEnvelope("coverage", `{"schema_version":"gograph.coverage.v1","status":"exact","analysis_precision":"precise","test_call_resolution":"typed_complete","matched_tests":[{"stable_id":"module/pkg::TestGuard","kind":"function"}],"symbols":[{"stable_id":"module/pkg::Exact","resolution":"exact"},{"stable_id":"module/pkg::Possible","resolution":"possible"}]}`), nil, nil
		case "identity":
			return graphEnvelope("identity", `{"schema_version":"gograph.identity.v1","status":"exact","matches":[{"stable_id":"module/pkg::Missing","kind":"function"}]}`), nil, nil
		default:
			return nil, nil, errors.New("unexpected command")
		}
	}))
	result, err := client.Verify(context.Background(), reach.Request{
		Repository: t.TempDir(), TestSymbol: "module/pkg::TestGuard",
		ProtectedSymbols: []string{"module/pkg::Exact", "module/pkg::Possible", "module/pkg::Missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []reach.Resolution{reach.ResolutionExact, reach.ResolutionPossible, reach.ResolutionMissing}
	for i, symbol := range result.Symbols {
		if symbol.Resolution != want[i] {
			t.Fatalf("symbol %d resolution = %q, want %q", i, symbol.Resolution, want[i])
		}
	}
}

func TestVerifyFailsClosedOnUntrustedGraphStateAndFallback(t *testing.T) {
	tests := []struct {
		name     string
		envelope []byte
	}{
		{name: "stale", envelope: []byte(`{"schema_version":"1","command":"coverage","status":"ok","results":{},"graph_state":{"schema_version":"gograph.graph-state.v1","source":"persisted","freshness":"stale","completeness":"complete","precision":"precise"}}`)},
		{name: "partial", envelope: []byte(`{"schema_version":"1","command":"coverage","status":"ok","results":{},"graph_state":{"schema_version":"gograph.graph-state.v1","source":"persisted","freshness":"current","completeness":"partial","precision":"precise"}}`)},
		{name: "fallback", envelope: []byte(`{"schema_version":"1","command":"coverage","status":"ok","results":{},"graph_state":{"schema_version":"gograph.graph-state.v1","source":"persisted","freshness":"current","completeness":"complete","precision":"precise_fallback"}}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
				return test.envelope, nil, nil
			}))
			_, err := client.Verify(context.Background(), reach.Request{Repository: t.TempDir(), TestSymbol: "module/pkg::TestGuard"})
			assertEvidenceKind(t, err, reach.ErrorInsufficient)
		})
	}
}

func TestVerifyRejectsMultipleJSONDocuments(t *testing.T) {
	client := NewWithRunner(runnerFunc(func(context.Context, string, ...string) ([]byte, []byte, error) {
		valid := graphEnvelope("coverage", `{"schema_version":"gograph.coverage.v1","status":"exact","analysis_precision":"precise","test_call_resolution":"typed_complete"}`)
		return append(valid, []byte(` {}`)...), nil, nil
	}))
	_, err := client.Verify(context.Background(), reach.Request{Repository: t.TempDir(), TestSymbol: "module/pkg::TestGuard"})
	assertEvidenceKind(t, err, reach.ErrorInsufficient)
}

func assertEvidenceKind(t *testing.T, err error, want reach.ErrorKind) {
	t.Helper()
	var evidenceErr *reach.EvidenceError
	if !errors.As(err, &evidenceErr) || evidenceErr.Kind != want {
		t.Fatalf("error = %v, want evidence kind %q", err, want)
	}
}
