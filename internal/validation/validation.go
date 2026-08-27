package validation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
	"github.com/ozgurcd/rulefloor/internal/extract"
	"github.com/ozgurcd/rulefloor/internal/ledger"
	reachevidence "github.com/ozgurcd/rulefloor/internal/reach"
	"github.com/ozgurcd/rulefloor/internal/repository"
)

const (
	SchemaVersion        = "rulefloor.validation.v1"
	VersionSchemaVersion = "rulefloor.version.v1"
)

type Mode string

const (
	ModeStatic  Mode = "static"
	ModeExecute Mode = "execute"
)

type Outcome string

const (
	OutcomePass           Outcome = "pass"
	OutcomeFail           Outcome = "fail"
	OutcomeCannotEvaluate Outcome = "cannot_evaluate"
)

type Status string

const (
	StatusPass           Status = "pass"
	StatusFail           Status = "fail"
	StatusCannotEvaluate Status = "cannot_evaluate"
	StatusNotPerformed   Status = "not_performed"
	StatusNotRequested   Status = "not_requested"
)

type RedProofStatus string

const (
	RedProofPresent       RedProofStatus = "present"
	RedProofMissing       RedProofStatus = "missing"
	RedProofNotApplicable RedProofStatus = "not_applicable"
)

type Request struct {
	Repository string
	RuleID     string
	Mode       Mode
	Profile    string
	Tags       string
}

type VersionResult struct {
	SchemaVersion       string `json:"schema_version"`
	Version             string `json:"version"`
	ToolchainVersion    string `json:"toolchain_version"`
	VersionDisagreement bool   `json:"version_disagreement,omitzero"`
}

// RuntimeVersionResult reports the legacy linker stamp alongside the main
// module version embedded by the Go toolchain.
func RuntimeVersionResult(stamp string) VersionResult {
	toolchainVersion := "(unknown)"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		toolchainVersion = info.Main.Version
	}
	return versionResult(stamp, toolchainVersion)
}

func versionResult(stamp, toolchainVersion string) VersionResult {
	return VersionResult{
		SchemaVersion:       VersionSchemaVersion,
		Version:             stamp,
		ToolchainVersion:    toolchainVersion,
		VersionDisagreement: versionsDisagree(stamp, toolchainVersion),
	}
}

func versionsDisagree(stamp, toolchainVersion string) bool {
	if toolchainVersion == "(unknown)" {
		return false
	}
	if stamp == "dev" && toolchainVersion == "(devel)" {
		return false
	}
	return stamp != toolchainVersion
}

type Result struct {
	SchemaVersion    string      `json:"schema_version"`
	Command          string      `json:"command"`
	RulefloorVersion string      `json:"rulefloor_version"`
	GeneratedAt      string      `json:"generated_at"`
	Repository       Repository  `json:"repository"`
	Request          RequestView `json:"request"`
	Rule             Rule        `json:"rule"`
	Evaluation       Evaluation  `json:"evaluation"`
}

type Repository struct {
	Root              string `json:"root"`
	LedgerPath        string `json:"ledger_path"`
	LedgerFingerprint string `json:"ledger_fingerprint"`
}

type RequestView struct {
	RuleID  string `json:"rule_id"`
	Mode    string `json:"mode"`
	Profile string `json:"profile"`
	Tags    string `json:"tags,omitempty"`
}

type Fingerprint struct {
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type Rule struct {
	Exists           bool           `json:"exists"`
	Armed            bool           `json:"armed"`
	EnforcedBy       string         `json:"enforced_by"`
	CheckFile        string         `json:"check_file"`
	DeclaredProfile  string         `json:"declared_profile"`
	RedProofStatus   RedProofStatus `json:"red_proof_status"`
	TestFingerprint  Fingerprint    `json:"test_fingerprint"`
	ProofFingerprint *string        `json:"proof_fingerprint,omitempty"`
	ProtectedSymbols []string       `json:"protected_symbols,omitempty"`
}

type Execution struct {
	Requested bool   `json:"requested"`
	Performed bool   `json:"performed"`
	Status    Status `json:"status"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Test    string `json:"test,omitempty"`
}

type ProtectedSymbol struct {
	StableID   string `json:"stable_id"`
	Resolution string `json:"resolution"`
}

type StructuralReach struct {
	Required   bool              `json:"required"`
	Status     Status            `json:"status"`
	TestSymbol string            `json:"test_symbol"`
	Symbols    []ProtectedSymbol `json:"symbols"`
}

type Evaluation struct {
	Outcome         Outcome          `json:"outcome"`
	StaticIntegrity Status           `json:"static_integrity"`
	Execution       Execution        `json:"execution"`
	Reason          string           `json:"reason"`
	Diagnostics     []Diagnostic     `json:"diagnostics"`
	StructuralReach *StructuralReach `json:"structural_reach,omitempty"`
}

type Service struct {
	version  string
	now      func() time.Time
	runner   checkengine.CommandRunner
	registry *extract.Registry
	reach    reachevidence.Verifier
}

func NewService(version string) Service {
	return NewServiceWith(version, time.Now, checkengine.ExecRunner{})
}

func NewServiceWith(version string, now func() time.Time, runner checkengine.CommandRunner) Service {
	return Service{version: version, now: now, runner: runner, registry: extract.DefaultRegistry()}
}

func NewServiceWithReach(version string, now func() time.Time, runner checkengine.CommandRunner, verifier reachevidence.Verifier) Service {
	service := NewServiceWith(version, now, runner)
	service.reach = verifier
	return service
}

func (s Service) InvalidResult(request Request, message string) Result {
	return s.cannotEvaluate(s.newResult(request), "invalid_request", checkengine.BoundedMessage(message))
}

func (s Service) Validate(ctx context.Context, request Request) Result {
	result := s.newResult(request)
	if ctx == nil {
		return s.cannotEvaluate(result, "invalid_request", "validation context is nil")
	}
	if err := ctx.Err(); err != nil {
		return s.contextResult(result, err)
	}
	if request.RuleID == "" || !ledger.ValidID(request.RuleID) {
		return s.cannotEvaluate(result, "invalid_request", fmt.Sprintf("invalid rule ID %q", request.RuleID))
	}
	if request.Mode != ModeStatic && request.Mode != ModeExecute {
		return s.cannotEvaluate(result, "invalid_request", fmt.Sprintf("unsupported mode %q", request.Mode))
	}
	if request.Mode == ModeExecute {
		result.Evaluation.Execution.Requested = true
		result.Evaluation.Execution.Status = StatusNotPerformed
	}
	if err := validateRequestOptions(request); err != nil {
		return s.cannotEvaluate(result, "invalid_request", err.Error())
	}

	root, err := repository.CanonicalRoot(request.Repository)
	if err != nil {
		return s.cannotEvaluate(result, "invalid_request", checkengine.BoundedMessage(err.Error()))
	}
	result.Repository.Root = root
	result.Repository.LedgerPath = filepath.Join(root, ledger.Filename)
	ledgerResolved, err := repository.ConfinedRegularFile(root, ledger.Filename)
	if err != nil {
		return s.cannotEvaluateWith(result, "ledger_invalid", Diagnostic{Code: "ledger_unavailable", Message: checkengine.BoundedMessage(err.Error()), Path: ledger.Filename})
	}
	ledgerData, err := os.ReadFile(ledgerResolved)
	if err != nil {
		return s.cannotEvaluateWith(result, "ledger_invalid", Diagnostic{Code: "ledger_unavailable", Message: checkengine.BoundedMessage(err.Error()), Path: ledger.Filename})
	}
	result.Repository.LedgerFingerprint = fullFingerprint(ledgerData)
	model, err := ledger.Parse(string(ledgerData))
	if err != nil {
		return s.cannotEvaluateWith(result, "ledger_invalid", Diagnostic{Code: "ledger_invalid", Message: checkengine.BoundedMessage(err.Error()), Path: ledger.Filename})
	}
	row := model.Find(request.RuleID)
	if row == nil {
		return s.cannotEvaluate(result, "rule_not_found", fmt.Sprintf("rule %s does not exist", request.RuleID))
	}
	result.Rule.Exists = true
	result.Rule.Armed = row.Armed()
	result.Rule.EnforcedBy = row.EnforcedBy
	if !row.Armed() {
		return s.cannotEvaluate(result, "rule_unarmed", fmt.Sprintf("rule %s is declared but not armed", request.RuleID))
	}

	binding, err := ledger.InterpretBinding(row)
	if err != nil {
		return s.cannotEvaluate(result, "ledger_invalid", checkengine.BoundedMessage(err.Error()))
	}
	file := binding.File
	declaredProfile := binding.Profile
	result.Rule.CheckFile = file
	result.Rule.DeclaredProfile = declaredProfile
	if row.ReachabilityPolicy != "" {
		result.Rule.ProtectedSymbols = append([]string{}, row.CoveredSymbols...)
		result.Evaluation.StructuralReach = &StructuralReach{
			Required: true, Status: StatusNotPerformed, TestSymbol: row.TestSymbol,
			Symbols: make([]ProtectedSymbol, 0, len(row.CoveredSymbols)),
		}
	}
	result.Rule.TestFingerprint.Expected = row.Hash
	proof, err := ledger.ParseProof(row.RedProof)
	if err != nil {
		return s.cannotEvaluate(result, "ledger_invalid", checkengine.BoundedMessage(err.Error()))
	}
	if !proof.Present() {
		result.Rule.RedProofStatus = RedProofMissing
	} else {
		result.Rule.RedProofStatus = RedProofPresent
		fingerprint := proof.Fingerprint()
		result.Rule.ProofFingerprint = &fingerprint
	}
	if request.Mode == ModeExecute {
		if err := checkengine.ValidateExecutionProfile(binding, request.Profile); err != nil {
			return s.cannotEvaluate(result, "profile_mismatch", err.Error())
		}
	}
	selectedExtractor, err := s.registry.ForFile(file)
	if err != nil {
		return s.cannotEvaluate(result, "execution_unsupported", checkengine.BoundedMessage(err.Error()))
	}
	if row.EnforcedBy != string(selectedExtractor.Kind()) {
		return s.cannotEvaluate(result, "ledger_invalid", fmt.Sprintf("enforced-by is %q but check file implies %q", row.EnforcedBy, selectedExtractor.Kind()))
	}

	resolvedCheck, err := repository.ConfinedRegularFile(root, file)
	if err != nil {
		return s.cannotEvaluateWith(result, "check_file_missing", Diagnostic{Code: "check_file_missing", Message: checkengine.BoundedMessage(err.Error()), Path: file})
	}
	data, err := os.ReadFile(resolvedCheck)
	if err != nil {
		return s.cannotEvaluateWith(result, "check_file_missing", Diagnostic{Code: "check_file_missing", Message: checkengine.BoundedMessage(err.Error()), Path: file})
	}
	static, err := checkengine.EvaluateSource(s.registry, row, string(data))
	if err != nil {
		reason := extractionReason(err)
		return s.cannotEvaluateWith(result, reason, Diagnostic{Code: reason, Message: checkengine.BoundedMessage(err.Error()), Path: file})
	}
	result.Rule.TestFingerprint.Actual = static.Ref.Fingerprint()
	if len(static.Issues) > 0 {
		issue := static.Issues[0]
		if issue.Code == "enforced_by_mismatch" {
			return s.cannotEvaluate(result, "ledger_invalid", issue.Message)
		}
		message := issue.Message
		if issue.Code == "hash_mismatch" {
			message = fmt.Sprintf("ledger hash %s does not match extracted hash %s", row.Hash, result.Rule.TestFingerprint.Actual)
		}
		if issue.Code == "test_skipped" && static.Ref.GoSkips {
			message = "tagged unit Go test calls t.Skip"
		}
		return s.staticFailure(result, issue.Code, message, file, static.Ref.FuncName)
	}
	if result.Rule.RedProofStatus != RedProofPresent {
		return s.staticFailure(result, "red_proof_missing", "armed rule has no red-proof text", file, static.Ref.FuncName)
	}
	result.Evaluation.StaticIntegrity = StatusPass
	if result.Evaluation.StructuralReach != nil {
		reachResult, reachErr := checkengine.EvaluateProtectedReach(ctx, s.reach, root, row)
		if reachErr != nil {
			var evidenceErr *reachevidence.EvidenceError
			reason := "graph_evidence_unavailable"
			if errors.As(reachErr, &evidenceErr) {
				reason = string(evidenceErr.Kind)
			}
			result.Evaluation.StructuralReach.Status = StatusCannotEvaluate
			return s.cannotEvaluateWith(result, reason, Diagnostic{Code: reason, Message: checkengine.BoundedMessage(reachErr.Error()), Path: file, Test: static.Ref.FuncName})
		}
		for _, symbol := range reachResult.Symbols {
			result.Evaluation.StructuralReach.Symbols = append(result.Evaluation.StructuralReach.Symbols, ProtectedSymbol{
				StableID: symbol.StableID, Resolution: string(symbol.Resolution),
			})
		}
		if len(reachResult.Issues) > 0 {
			result.Evaluation.StructuralReach.Status = StatusFail
			issue := reachResult.Issues[0]
			return s.structuralFailure(result, issue.Code, issue.Message, file, static.Ref.FuncName)
		}
		result.Evaluation.StructuralReach.Status = StatusPass
	}

	if request.Mode == ModeStatic {
		result.Evaluation.Outcome = OutcomePass
		result.Evaluation.Reason = "rule_passed"
		return s.finish(result)
	}
	if !checkengine.SupportsExecution(static.Kind) {
		return s.cannotEvaluateWith(result, "execution_unsupported", Diagnostic{Code: "execution_unsupported", Message: fmt.Sprintf("Rulefloor cannot directly execute %s checks", static.Kind), Path: file})
	}
	execution := checkengine.RunGoTest(ctx, s.runner, resolvedCheck, static.Ref.FuncName, request.Tags)
	result.Evaluation.Execution.Performed = execution.Performed
	result.Evaluation.Execution.Status = validationExecutionStatus(execution.Status)
	if execution.Message != "" {
		result.Evaluation.Diagnostics = append(result.Evaluation.Diagnostics, Diagnostic{Code: execution.Reason, Message: execution.Message, Path: file, Test: static.Ref.FuncName})
	}
	switch execution.Status {
	case checkengine.ExecutionPass:
		result.Evaluation.Outcome = OutcomePass
		result.Evaluation.Reason = "rule_passed"
	case checkengine.ExecutionFail:
		result.Evaluation.Outcome = OutcomeFail
		result.Evaluation.Reason = "rule_failed"
	default:
		result.Evaluation.Outcome = OutcomeCannotEvaluate
		result.Evaluation.Reason = execution.Reason
	}
	return s.finish(result)
}

func validateRequestOptions(request Request) error {
	if request.Mode == ModeStatic && request.Profile != "" {
		return errors.New("--profile is not valid in static mode")
	}
	if request.Mode == ModeStatic && request.Tags != "" {
		return errors.New("--tags is not valid in static mode")
	}
	return checkengine.ValidateBuildTags(request.Tags)
}

func validationExecutionStatus(status checkengine.ExecutionStatus) Status {
	switch status {
	case checkengine.ExecutionPass:
		return StatusPass
	case checkengine.ExecutionFail:
		return StatusFail
	default:
		return StatusCannotEvaluate
	}
}

func extractionReason(err error) string {
	var extractionErr *extract.Error
	if errors.As(err, &extractionErr) {
		switch extractionErr.Kind {
		case extract.ErrorMissing, extract.ErrorMisplaced:
			return "tag_missing"
		case extract.ErrorAmbiguous:
			return "tag_ambiguous"
		}
	}
	return "cannot_parse_test"
}

func (s Service) newResult(request Request) Result {
	return Result{
		SchemaVersion:    SchemaVersion,
		Command:          "validate",
		RulefloorVersion: s.version,
		Request:          RequestView{RuleID: request.RuleID, Mode: string(request.Mode), Profile: request.Profile, Tags: request.Tags},
		Rule:             Rule{RedProofStatus: RedProofNotApplicable},
		Evaluation: Evaluation{
			StaticIntegrity: StatusNotPerformed,
			Execution:       Execution{Status: StatusNotRequested},
			Diagnostics:     make([]Diagnostic, 0),
		},
	}
}

func (s Service) finish(result Result) Result {
	result.GeneratedAt = s.now().UTC().Format(time.RFC3339Nano)
	return result
}

func (s Service) cannotEvaluate(result Result, reason, message string) Result {
	return s.cannotEvaluateWith(result, reason, Diagnostic{Code: reason, Message: message})
}

func (s Service) cannotEvaluateWith(result Result, reason string, diagnostic Diagnostic) Result {
	result.Evaluation.Outcome = OutcomeCannotEvaluate
	result.Evaluation.Reason = reason
	if result.Evaluation.Execution.Requested && !result.Evaluation.Execution.Performed {
		result.Evaluation.Execution.Status = StatusCannotEvaluate
	}
	result.Evaluation.Diagnostics = append(result.Evaluation.Diagnostics, diagnostic)
	return s.finish(result)
}

func (s Service) staticFailure(result Result, reason, message, path, test string) Result {
	result.Evaluation.Outcome = OutcomeFail
	result.Evaluation.StaticIntegrity = StatusFail
	result.Evaluation.Reason = reason
	result.Evaluation.Diagnostics = append(result.Evaluation.Diagnostics, Diagnostic{Code: reason, Message: message, Path: path, Test: test})
	return s.finish(result)
}

func (s Service) structuralFailure(result Result, reason, message, path, test string) Result {
	result.Evaluation.Outcome = OutcomeFail
	result.Evaluation.Reason = reason
	result.Evaluation.Diagnostics = append(result.Evaluation.Diagnostics, Diagnostic{Code: reason, Message: message, Path: path, Test: test})
	return s.finish(result)
}

func (s Service) contextResult(result Result, err error) Result {
	reason := "context_canceled"
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "deadline_exceeded"
	}
	return s.cannotEvaluate(result, reason, err.Error())
}

func fullFingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
