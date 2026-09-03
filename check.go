package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
	"github.com/ozgurcd/rulefloor/internal/extract"
	"github.com/ozgurcd/rulefloor/internal/ledger"
	rulemodel "github.com/ozgurcd/rulefloor/internal/model"
	"github.com/ozgurcd/rulefloor/internal/reach"
	"github.com/ozgurcd/rulefloor/internal/repository"
)

type checkOptions struct {
	ReportPath string
	AllSpec    string
	RunProfile string
	Tags       string
	OnlyID     string
}

func cmdCheck(repo string, options checkOptions, stdout io.Writer) error {
	if err := checkengine.ValidateBuildTags(options.Tags); err != nil {
		return fatalf("check: %v", err)
	}
	if options.OnlyID != "" {
		if options.AllSpec != "" || options.ReportPath != "" {
			return fatalf("check: --only cannot be combined with --all or --report")
		}
		return checkOnly(repo, options.OnlyID, options.RunProfile, options.Tags, stdout)
	}
	if options.AllSpec == "" {
		n, err := checkRepo(repo, options.ReportPath, options.RunProfile, options.Tags, stdout)
		if err != nil {
			return err
		}
		if n > 0 {
			return failf("check FAILED: %d problem(s) in %s", n, repo)
		}
		return nil
	}
	if options.ReportPath != "" {
		return fatalf("check: --report cannot be combined with --all")
	}
	if options.RunProfile != "" {
		return fatalf("check: --run-profile cannot be combined with --all (a profile run belongs to one repo)")
	}
	var targets []string
	for _, path := range strings.Split(options.AllSpec, ",") {
		if path = strings.TrimSpace(path); path != "" {
			targets = append(targets, path)
		}
	}
	if len(targets) == 0 {
		return fatalf("check: --all needs a comma-separated list of repo paths")
	}
	total := 0
	for _, target := range targets {
		fmt.Fprintf(stdout, "== %s ==\n", target)
		n, err := checkRepo(target, "", "", "", stdout)
		if err != nil {
			var exit exitErr
			if errors.As(err, &exit) {
				return exitErr{exit.code, target + ": " + exit.msg}
			}
			return err
		}
		total += n
	}
	if total > 0 {
		return failf("check FAILED: %d problem(s) across %d repos", total, len(targets))
	}
	return nil
}

func checkOnly(repo, id, runProfile, tags string, stdout io.Writer) error {
	model, err := loadLedger(repo)
	if err != nil {
		return err
	}
	row := model.find(id)
	if row == nil {
		return failf("no rule %s", id)
	}
	if !row.Armed() {
		return failf("refusing: rule %s is not armed", id)
	}
	binding, err := rulemodel.InterpretBinding((*rulemodel.Row)(row))
	if err != nil {
		return fatalf("row %s: %v", id, err)
	}
	if runProfile != "" && runProfile != binding.Profile {
		return fatalf("check: --only %s requested profile %q but the row declares %q", id, runProfile, binding.Profile)
	}
	problems, err := checkRow(repo, row, rowCheckOptions{
		runProfile: runProfile, tags: tags, reachVerifier: newGraphClient(),
	})
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		for _, problem := range problems {
			fmt.Fprintf(stdout, "FAIL %s: %s\n", id, problem)
		}
		return failf("check --only FAILED: %d problem(s) for %s", len(problems), id)
	}
	fmt.Fprintf(stdout, "PASS %s (%s)\nselected check OK (not a full repository gate)\n", id, row.Check)
	return nil
}

func checkRepo(repo, reportPath, runProfile, tags string, stdout io.Writer) (int, error) {
	model, err := loadLedger(repo)
	if err != nil {
		return 0, err
	}
	problems := 0
	problem := func(format string, args ...any) {
		fmt.Fprintf(stdout, "FAIL "+format+"\n", args...)
		problems++
	}
	if model.effectiveCount() < model.Floor {
		if len(model.RepairedFixtures) == 0 {
			problem("ledger: %d rows is below FLOOR %d (rows deleted or FLOOR tampered)", len(model.Rows), model.Floor)
		} else {
			problem("ledger: %d rows plus %d repaired fixtures is below FLOOR %d (rows deleted or FLOOR tampered)", len(model.Rows), len(model.RepairedFixtures), model.Floor)
		}
	}
	if model.HasRedProofs {
		if measured := model.measuredRedProofs(); measured < model.RedProofs {
			problem("ledger: %d red-proved armed rows is below RED-PROOFS %d (a proof was emptied or a proven row deleted)", measured, model.RedProofs)
		}
	}
	var playwrightReport map[string][]string
	if reportPath != "" {
		playwrightReport, err = parsePWReport(reportPath)
		if err != nil {
			return 0, err
		}
	}
	armed := 0
	reachVerifier := newGraphClient()
	goExecutor := checkengine.NewCompiledGoTestExecutor(checkengine.ExecRunner{})
	defer goExecutor.Close()
	for i := range model.Rows {
		row := &model.Rows[i]
		if !row.Armed() {
			fmt.Fprintf(stdout, "DECLARED %s (no armed check)\n", row.ID)
			continue
		}
		armed++
		rowProblems, err := checkRow(repo, row, rowCheckOptions{
			report: playwrightReport, haveReport: reportPath != "", runProfile: runProfile, tags: tags,
			reachVerifier: reachVerifier, goExecutor: goExecutor,
		})
		if err != nil {
			return 0, err
		}
		if len(rowProblems) == 0 {
			fmt.Fprintf(stdout, "PASS %s (%s)\n", row.ID, row.Check)
			continue
		}
		for _, rowProblem := range rowProblems {
			problem("%s: %s", row.ID, rowProblem)
		}
	}
	orphans, err := scanOrphans(repo, model)
	if err != nil {
		return 0, err
	}
	for _, orphan := range orphans {
		problem("%s", orphan)
	}
	if problems == 0 {
		if model.HasRedProofs {
			if len(model.RepairedFixtures) > 0 {
				fmt.Fprintf(stdout, "check OK: %d rows + %d repaired fixtures (%d armed), FLOOR %d, RED-PROOFS %d (measured %d)\n",
					len(model.Rows), len(model.RepairedFixtures), armed, model.Floor, model.RedProofs, model.measuredRedProofs())
			} else {
				fmt.Fprintf(stdout, "check OK: %d rows (%d armed), FLOOR %d, RED-PROOFS %d (measured %d)\n",
					len(model.Rows), armed, model.Floor, model.RedProofs, model.measuredRedProofs())
			}
		} else {
			fmt.Fprintf(stdout, "check OK: %d rows (%d armed), FLOOR %d\n", len(model.Rows), armed, model.Floor)
		}
	}
	return problems, nil
}

type rowCheckOptions struct {
	report        map[string][]string
	haveReport    bool
	runProfile    string
	tags          string
	reachVerifier reach.Verifier
	goExecutor    checkengine.GoTestExecutor
}

func checkRow(repo string, row *Row, options rowCheckOptions) ([]string, error) {
	binding, err := rulemodel.InterpretBinding((*rulemodel.Row)(row))
	if err != nil {
		return nil, fatalf("row %s: %v", row.ID, err)
	}
	file := binding.File
	selectedExtractor, err := extractorRegistry.ForFile(file)
	if err != nil {
		return nil, fatalf("row %s: %v", row.ID, err)
	}
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return nil, fatalf("row %s: cannot resolve repository: %v", row.ID, err)
	}
	resolved, err := repository.ConfinedRegularFile(root, file)
	if err != nil {
		var problems []string
		if row.EnforcedBy != string(selectedExtractor.Kind()) {
			problems = append(problems, fmt.Sprintf("enforced-by is %q but check file implies %q", row.EnforcedBy, selectedExtractor.Kind()))
		}
		return append(problems, fmt.Sprintf("check file %s cannot be read: %v", file, err)), nil
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		var problems []string
		if row.EnforcedBy != string(selectedExtractor.Kind()) {
			problems = append(problems, fmt.Sprintf("enforced-by is %q but check file implies %q", row.EnforcedBy, selectedExtractor.Kind()))
		}
		return append(problems, fmt.Sprintf("check file %s cannot be read: %v", file, err)), nil
	}
	evaluation, err := checkengine.EvaluateSource(extractorRegistry, (*rulemodel.Row)(row), string(data))
	if err != nil {
		if extract.IsFatal(err) || strings.HasPrefix(err.Error(), "CANNOT-EVALUATE:") {
			return nil, fatalf("%v", err)
		}
		return []string{err.Error()}, nil
	}
	rowProblems := make([]string, 0, len(evaluation.Issues)+1)
	for _, issue := range evaluation.Issues {
		rowProblems = append(rowProblems, issue.Message)
	}
	if len(evaluation.Issues) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		reachEvaluation, reachErr := checkengine.EvaluateProtectedReach(ctx, options.reachVerifier, root, (*rulemodel.Row)(row))
		cancel()
		if reachErr != nil {
			var evidenceErr *reach.EvidenceError
			if errors.As(reachErr, &evidenceErr) {
				if evidenceErr.Kind == reach.ErrorAmbiguous {
					return nil, fatalf("CANNOT-EVALUATE: ambiguous symbol identity: %s", evidenceErr.Message)
				}
				return nil, fatalf("CANNOT-EVALUATE: graph evidence unavailable or insufficient: %s", evidenceErr.Message)
			}
			return nil, fatalf("CANNOT-EVALUATE: graph evidence unavailable or insufficient: %v", reachErr)
		}
		for _, issue := range reachEvaluation.Issues {
			rowProblems = append(rowProblems, issue.Message)
		}
	}
	executionProblem, err := executeRowBinding(resolved, binding, evaluation, options)
	if err != nil {
		return nil, err
	}
	if executionProblem != "" {
		rowProblems = append(rowProblems, executionProblem)
	}
	rowProblems = append(rowProblems, playwrightReportProblems(row.ID, evaluation.Kind, options)...)
	return rowProblems, nil
}

func executeRowBinding(testFile string, binding rulemodel.Binding, evaluation checkengine.StaticEvaluation, options rowCheckOptions) (string, error) {
	if !checkengine.SupportsExecution(evaluation.Kind) {
		return "", nil
	}
	if binding.Execution == rulemodel.ExecutionExecute {
		return runGoTestWith(options.goExecutor, testFile, evaluation.Ref.FuncName, "", false)
	}
	if options.runProfile != "" && binding.Profile == options.runProfile {
		return runGoTestWith(options.goExecutor, testFile, evaluation.Ref.FuncName, options.tags, true)
	}
	return "", nil
}

func playwrightReportProblems(id string, kind extract.Kind, options rowCheckOptions) []string {
	if kind != extract.KindPlaywright || !options.haveReport {
		return nil
	}
	statuses, ok := options.report[id]
	if !ok {
		return []string{"not present in the Playwright report"}
	}
	var problems []string
	for _, status := range statuses {
		if status != "passed" {
			problems = append(problems, fmt.Sprintf("Playwright report status is %q, want \"passed\"", status))
		}
	}
	return problems
}

func runGoTest(testFile, functionName, tags string, skipIsFatal bool) (string, error) {
	execution := checkengine.RunGoTest(context.Background(), checkengine.ExecRunner{}, testFile, functionName, tags)
	return reportGoTestExecution(execution, functionName, skipIsFatal)
}

func runGoTestWith(executor checkengine.GoTestExecutor, testFile, functionName, tags string, skipIsFatal bool) (string, error) {
	if executor == nil {
		return runGoTest(testFile, functionName, tags, skipIsFatal)
	}
	execution := executor.Run(context.Background(), testFile, functionName, tags)
	return reportGoTestExecution(execution, functionName, skipIsFatal)
}

func reportGoTestExecution(execution checkengine.Execution, functionName string, skipIsFatal bool) (string, error) {
	if execution.Status == checkengine.ExecutionPass {
		return "", nil
	}
	if execution.Reason == "toolchain_unavailable" {
		return "", fatalf("CANNOT-EVALUATE: go toolchain not found: %s", execution.Message)
	}
	if execution.Reason == "test_skipped" && skipIsFatal {
		return "", fatalf("CANNOT-EVALUATE: %s SKIPPED under --run-profile — its precondition is absent and a skip is not a proof:\n%s", functionName, execution.Message)
	}
	if execution.Reason == "test_skipped" {
		return fmt.Sprintf("go test ran but did not report \"--- PASS: %s\"", functionName), nil
	}
	return fmt.Sprintf("go test -run ^%s$ failed:\n%s", functionName, execution.Message), nil
}

func scanOrphans(repo string, model *Ledger) ([]string, error) {
	problems, err := checkengine.ScanOrphans(extractorRegistry, repo, (*ledger.Ledger)(model))
	if err != nil {
		return nil, fatalf("%v", err)
	}
	return problems, nil
}

func parsePWReport(path string) (map[string][]string, error) {
	report, err := checkengine.ParsePlaywrightReport(path)
	if err != nil {
		return nil, fatalf("%v", err)
	}
	return report, nil
}
