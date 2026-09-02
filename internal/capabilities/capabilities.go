// Package capabilities describes features compiled into the Rulefloor binary.
// It does not inspect a repository, ledger, environment, or installed toolchain.
package capabilities

import (
	"slices"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
	"github.com/ozgurcd/rulefloor/internal/extract"
	"github.com/ozgurcd/rulefloor/internal/ledger"
	"github.com/ozgurcd/rulefloor/internal/ledgerdiff"
	machine "github.com/ozgurcd/rulefloor/internal/validation"
)

const SchemaVersion = "rulefloor.capabilities.v1"

type Result struct {
	SchemaVersion      string             `json:"schema_version"`
	RulefloorVersion   string             `json:"rulefloor_version"`
	MachineInterfaces  []string           `json:"machine_interfaces"`
	TestKinds          []TestKind         `json:"test_kinds"`
	ValidationModes    []string           `json:"validation_modes"`
	ProofKinds         []string           `json:"proof_kinds"`
	LedgerFeatures     []string           `json:"ledger_features"`
	Commands           []string           `json:"commands"`
	ExecutionSemantics ExecutionSemantics `json:"execution_semantics"`
	StructuralReach    StructuralReach    `json:"structural_reach"`
}

type TestKind struct {
	Kind             string `json:"kind"`
	StaticValidation bool   `json:"static_validation"`
	Execution        bool   `json:"execution"`
}

type ExecutionSemantics struct {
	SupportDependsOn         string `json:"support_depends_on"`
	StaticValidationExecutes bool   `json:"static_validation_executes"`
	ExecuteFallsBackToStatic bool   `json:"execute_falls_back_to_static"`
	PolicyPersisted          bool   `json:"policy_persisted"`
	LegacyProfileCompatible  bool   `json:"legacy_profile_compatible"`
}

type StructuralReach struct {
	Provider             string   `json:"provider"`
	TestKinds            []string `json:"test_kinds"`
	SymbolIdentity       string   `json:"symbol_identity"`
	RequiredResolution   string   `json:"required_resolution"`
	PossibleIsSufficient bool     `json:"possible_is_sufficient"`
	EvidenceRequirements []string `json:"evidence_requirements"`
}

func New(rulefloorVersion string, commands []string) Result {
	commands = slices.Clone(commands)
	slices.Sort(commands)
	kinds := []extract.Kind{extract.KindGoTest, extract.KindPlaywright, extract.KindVitest}
	testKinds := make([]TestKind, 0, len(kinds))
	for _, kind := range kinds {
		testKinds = append(testKinds, TestKind{
			Kind:             string(kind),
			StaticValidation: true,
			Execution:        checkengine.SupportsExecution(kind),
		})
	}
	return Result{
		SchemaVersion:    SchemaVersion,
		RulefloorVersion: rulefloorVersion,
		MachineInterfaces: []string{
			machine.VersionSchemaVersion,
			machine.SchemaVersion,
			SchemaVersion,
			ledger.CoversSchemaVersion,
			ledgerdiff.SchemaVersion,
		},
		TestKinds:       testKinds,
		ValidationModes: []string{string(machine.ModeStatic), string(machine.ModeExecute)},
		ProofKinds: []string{
			string(ledger.ProofKindLegacyManual),
			string(ledger.ProofKindManualObservation),
			string(ledger.ProofKindMutationObservation),
			string(ledger.ProofKindCIReference),
		},
		LedgerFeatures: []string{"FLOOR", "RED-PROOFS", "six-column-ledger", "proof-v1", "covers-v1", "binding-v1", "exact-symbol-reach", "ledger-diff-sentence-sha256", "REPAIRED-FIXTURES"},
		Commands:       commands,
		ExecutionSemantics: ExecutionSemantics{
			SupportDependsOn:         "test_kind",
			StaticValidationExecutes: false,
			ExecuteFallsBackToStatic: false,
			PolicyPersisted:          true,
			LegacyProfileCompatible:  true,
		},
		StructuralReach: StructuralReach{
			Provider: "gograph", TestKinds: []string{string(extract.KindGoTest)},
			SymbolIdentity: "stable_id", RequiredResolution: "exact", PossibleIsSufficient: false,
			EvidenceRequirements: []string{"persisted", "current", "complete", "precise", "typed_complete"},
		},
	}
}
