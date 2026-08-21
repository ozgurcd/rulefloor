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
