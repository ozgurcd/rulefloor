package validation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/ozgurcd/rulefloor/internal/extract"
	"github.com/ozgurcd/rulefloor/internal/ledger"
	"github.com/ozgurcd/rulefloor/internal/model"
	"github.com/ozgurcd/rulefloor/internal/reach"
)

func TestStaticServicePreservesValidationV1Shape(t *testing.T) {
	repo := t.TempDir()
	source := "package sample\n\n// RULE: MACHINE-1\nfunc TestMachine(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(repo, "machine_test.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := extract.NewGo().Extract(source, "MACHINE-1")
	if err != nil {
		t.Fatal(err)
	}
	model := &ledger.Ledger{Floor: 1, HasRedProofs: true, RedProofs: 1, Rows: []ledger.Row{{
		ID: "MACHINE-1", Rule: "The machine contract remains stable.", EnforcedBy: "go-test",
		Check: "machine_test.go @ unit", RedProof: "watched red", Hash: ref.Fingerprint(),
	}}}
	if err := ledger.Save(repo, model); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	result := NewServiceWith("v-test", func() time.Time { return fixed }, nilRunner{}).Validate(context.Background(), Request{
		Repository: repo, RuleID: "MACHINE-1", Mode: ModeStatic,
	})
	if result.SchemaVersion != "rulefloor.validation.v1" || result.Evaluation.Outcome != OutcomePass {
		t.Fatalf("result = %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"command", "evaluation", "generated_at", "repository", "request", "rule", "rulefloor_version", "schema_version"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("top-level keys = %v, want %v", keys, want)
	}
}

type nilRunner struct{}

func (nilRunner) Run(context.Context, string, string, ...string) ([]byte, error) {
	panic("static validation must not execute")
}

type reachVerifierFunc func(context.Context, reach.Request) (reach.Result, error)

func (f reachVerifierFunc) Verify(ctx context.Context, request reach.Request) (reach.Result, error) {
	return f(ctx, request)
}

func TestMachineValidationReportsStructuralReachWithoutChangingV1(t *testing.T) {
	repo := newReachValidationRepo(t)
	service := NewServiceWithReach("v-test", time.Now, nilRunner{}, reachVerifierFunc(func(_ context.Context, request reach.Request) (reach.Result, error) {
		return reach.Result{TestSymbol: request.TestSymbol, Symbols: []reach.Symbol{{StableID: request.ProtectedSymbols[0], Resolution: reach.ResolutionExact}}}, nil
	}))
	result := service.Validate(context.Background(), Request{Repository: repo, RuleID: "MACHINE-1", Mode: ModeStatic})
	if result.SchemaVersion != SchemaVersion || result.Evaluation.Outcome != OutcomePass || result.Evaluation.StaticIntegrity != StatusPass {
		t.Fatalf("result = %+v", result)
	}
	if result.Evaluation.StructuralReach == nil || result.Evaluation.StructuralReach.Status != StatusPass || len(result.Rule.ProtectedSymbols) != 1 {
		t.Fatalf("structural reach = %+v, rule = %+v", result.Evaluation.StructuralReach, result.Rule)
	}
}

func TestMachineValidationSeparatesProofDecayFromSourceIntegrity(t *testing.T) {
	repo := newReachValidationRepo(t)
	service := NewServiceWithReach("v-test", time.Now, nilRunner{}, reachVerifierFunc(func(_ context.Context, request reach.Request) (reach.Result, error) {
		return reach.Result{TestSymbol: request.TestSymbol, Symbols: []reach.Symbol{{StableID: request.ProtectedSymbols[0], Resolution: reach.ResolutionMissing}}}, nil
	}))
	result := service.Validate(context.Background(), Request{Repository: repo, RuleID: "MACHINE-1", Mode: ModeStatic})
	if result.Evaluation.Outcome != OutcomeFail || result.Evaluation.Reason != "protected_symbol_unreached" || result.Evaluation.StaticIntegrity != StatusPass {
		t.Fatalf("result = %+v", result)
	}
	if result.Evaluation.StructuralReach == nil || result.Evaluation.StructuralReach.Status != StatusFail {
		t.Fatalf("structural reach = %+v", result.Evaluation.StructuralReach)
	}
}

func TestMachineValidationFailsClosedWhenGraphEvidenceIsUnavailable(t *testing.T) {
	repo := newReachValidationRepo(t)
	service := NewServiceWithReach("v-test", time.Now, nilRunner{}, reachVerifierFunc(func(context.Context, reach.Request) (reach.Result, error) {
		return reach.Result{}, &reach.EvidenceError{Kind: reach.ErrorInsufficient, Message: "stale graph"}
	}))
	result := service.Validate(context.Background(), Request{Repository: repo, RuleID: "MACHINE-1", Mode: ModeStatic})
	if result.Evaluation.Outcome != OutcomeCannotEvaluate || result.Evaluation.Reason != string(reach.ErrorInsufficient) || result.Evaluation.StaticIntegrity != StatusPass {
		t.Fatalf("result = %+v", result)
	}
	if result.Evaluation.StructuralReach == nil || result.Evaluation.StructuralReach.Status != StatusCannotEvaluate {
		t.Fatalf("structural reach = %+v", result.Evaluation.StructuralReach)
	}
}

func newReachValidationRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	source := "package sample\n\nimport \"testing\"\n\n// RULE: MACHINE-1\nfunc TestMachine(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(repo, "machine_test.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, err := extract.NewGo().Extract(source, "MACHINE-1")
	if err != nil {
		t.Fatal(err)
	}
	l := &ledger.Ledger{Floor: 1, HasRedProofs: true, RedProofs: 1, Rows: []ledger.Row{{
		ID: "MACHINE-1", Rule: "The machine contract remains stable.", EnforcedBy: "go-test",
		Check: "machine_test.go @ unit", RedProof: "watched red", Hash: ref.Fingerprint(),
		ExecutionPolicy: model.ExecutionExecute, ReachabilityPolicy: model.ReachabilityExact,
		TestSymbol: "sample::TestMachine", CoveredSymbols: []string{"sample::Guard"},
	}}}
	if err := ledger.Save(repo, l); err != nil {
		t.Fatal(err)
	}
	return repo
}
