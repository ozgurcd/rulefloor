package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
	"github.com/ozgurcd/rulefloor/internal/extract"
	"github.com/ozgurcd/rulefloor/internal/ledger"
	"github.com/ozgurcd/rulefloor/internal/repository"
)

func cmdCheck(repo, reportPath, allSpec, runProfile, tags string, stdout io.Writer) error {
	if allSpec == "" {
		n, err := checkRepo(repo, reportPath, runProfile, tags, stdout)
		if err != nil {
			return err
		}
		if n > 0 {
			return failf("check FAILED: %d problem(s) in %s", n, repo)
		}
		return nil
	}
	if reportPath != "" {
		return fatalf("check: --report cannot be combined with --all")
	}
	if runProfile != "" {
		return fatalf("check: --run-profile cannot be combined with --all (a profile run belongs to one repo)")
	}
	var targets []string
	for _, path := range strings.Split(allSpec, ",") {
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
	for i := range model.Rows {
		row := &model.Rows[i]
		if !row.Armed() {
			fmt.Fprintf(stdout, "DECLARED %s (no armed check)\n", row.ID)
			continue
		}
		armed++
		rowProblems, err := checkRow(repo, row, rowCheckOptions{
			report: playwrightReport, haveReport: reportPath != "", runProfile: runProfile, tags: tags,
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
	report     map[string][]string
	haveReport bool
	runProfile string
	tags       string
}

func checkRow(repo string, row *Row, options rowCheckOptions) ([]string, error) {
	binding, err := ledger.InterpretBinding((*ledger.Row)(row))
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
	evaluation, err := checkengine.EvaluateSource(extractorRegistry, (*ledger.Row)(row), string(data))
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

func executeRowBinding(testFile string, binding ledger.Binding, evaluation checkengine.StaticEvaluation, options rowCheckOptions) (string, error) {
	if !checkengine.SupportsExecution(evaluation.Kind) {
		return "", nil
	}
	if binding.Execution == ledger.ExecutionExecute {
		return runGoTest(testFile, evaluation.Ref.FuncName, "", false)
	}
	if options.runProfile != "" && binding.Profile == options.runProfile {
		return runGoTest(testFile, evaluation.Ref.FuncName, options.tags, true)
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
