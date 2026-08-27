package main

import (
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"
	"io"
	"time"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
	"github.com/ozgurcd/rulefloor/internal/reach"
	machine "github.com/ozgurcd/rulefloor/internal/validation"
)

const (
	validationSchemaVersion = machine.SchemaVersion
	versionSchemaVersion    = machine.VersionSchemaVersion
)

type ValidationMode = machine.Mode

const (
	ValidationModeStatic  = machine.ModeStatic
	ValidationModeExecute = machine.ModeExecute
)

type ValidationOutcome = machine.Outcome

const (
	ValidationPass           = machine.OutcomePass
	ValidationFail           = machine.OutcomeFail
	ValidationCannotEvaluate = machine.OutcomeCannotEvaluate
)

type ValidationStatus = machine.Status

const (
	ValidationStatusPass           = machine.StatusPass
	ValidationStatusFail           = machine.StatusFail
	ValidationStatusCannotEvaluate = machine.StatusCannotEvaluate
	ValidationStatusNotPerformed   = machine.StatusNotPerformed
	ValidationStatusNotRequested   = machine.StatusNotRequested
)

type RedProofStatus = machine.RedProofStatus

const (
	RedProofPresent       = machine.RedProofPresent
	RedProofMissing       = machine.RedProofMissing
	RedProofNotApplicable = machine.RedProofNotApplicable
)

type ValidationRequest = machine.Request
type VersionResult = machine.VersionResult
type ValidationResult = machine.Result
type ValidationRepository = machine.Repository
type ValidationRequestView = machine.RequestView
type ValidationFingerprint = machine.Fingerprint
type ValidationRule = machine.Rule
type ValidationExecution = machine.Execution
type ValidationDiagnostic = machine.Diagnostic
type ValidationEvaluation = machine.Evaluation

type validationCommandRunner = checkengine.CommandRunner

type execValidationCommandRunner = checkengine.ExecRunner

type validationService struct {
	version string
	now     func() time.Time
	runner  validationCommandRunner
	reach   reach.Verifier
}

func newValidationService() validationService {
	return validationService{version: version, now: time.Now, runner: execValidationCommandRunner{}, reach: newGraphClient()}
}

func (s validationService) Validate(ctx context.Context, request ValidationRequest) ValidationResult {
	return machine.NewServiceWithReach(s.version, s.now, s.runner, s.reach).Validate(ctx, request)
}

func writeJSON(writer io.Writer, value any) error {
	if err := json.MarshalWrite(writer, value,
		json.Deterministic(true),
		json.FormatNilMapAsNull(true),
		json.FormatNilSliceAsNull(true),
		jsontext.EscapeForHTML(false),
	); err != nil {
		return err
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func dispatchValidate(args []string, stdout io.Writer) error {
	if !hasFlag(args, "--json") {
		return fatalf("validate requires --json")
	}
	request := ValidationRequest{Repository: "."}
	flags, positionals, err := parseArgs(args)
	if err != nil {
		return writeValidationCommandResult(stdout, invalidValidationResult(request, err.Error()))
	}
	allowed := map[string]bool{"--repo": true, "--mode": true, "--profile": true, "--tags": true, "--json": true}
	for name := range flags {
		if !allowed[name] {
			return writeValidationCommandResult(stdout, invalidValidationResult(request, fmt.Sprintf("flag %s is not valid for validate", name)))
		}
	}
	request.Repository = flags["--repo"]
	if request.Repository == "" {
		request.Repository = "."
	}
	request.Mode = ValidationMode(flags["--mode"])
	request.Profile = flags["--profile"]
	request.Tags = flags["--tags"]
	if len(positionals) == 1 {
		request.RuleID = positionals[0]
	} else {
		return writeValidationCommandResult(stdout, invalidValidationResult(request, "validate requires exactly one RULE-ID"))
	}
	return writeValidationCommandResult(stdout, newValidationService().Validate(context.Background(), request))
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func invalidValidationResult(request ValidationRequest, message string) ValidationResult {
	return machine.NewService(version).InvalidResult(request, message)
}

func writeValidationCommandResult(stdout io.Writer, result ValidationResult) error {
	if err := writeJSON(stdout, result); err != nil {
		return fatalf("write validation JSON: %v", err)
	}
	switch result.Evaluation.Outcome {
	case ValidationPass:
		return nil
	case ValidationFail:
		return silentExit{code: 1}
	default:
		return silentExit{code: 2}
	}
}
