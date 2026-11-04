package main

import (
	"strings"
	"testing"
)

// newFailingExecutionRepo returns a repo whose single armed unit row passes the
// static ledger/hash validation but whose bound test fails when executed.
func newFailingExecutionRepo(t *testing.T) string {
	t.Helper()
	repo := newValidationGoRepo(t, "unit")
	writeFile(t, repo, "FAIL", "fail without changing the test body")
	return repo
}

func TestCheckFullStillExecutesArmedSuites(t *testing.T) {
	repo := newFailingExecutionRepo(t)
	code, out := run2(t, "check", "--repo", repo)
	if code != 1 || !strings.Contains(out, "FAIL JSON-VALIDATE-1") {
		t.Fatalf("full check must execute and fail: code=%d\n%s", code, out)
	}
}

func TestCheckLightSkipsExecutionAndSaysSo(t *testing.T) {
	repo := newFailingExecutionRepo(t)
	code, out := run2(t, "check", "--light", "--repo", repo)
	if code != 0 {
		t.Fatalf("light check must not execute the suite: code=%d\n%s", code, out)
	}
	for _, want := range []string{"PASS JSON-VALIDATE-1", "check OK", "LIGHT", "1 test execution(s) skipped", "not a gate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("light output missing %q:\n%s", want, out)
		}
	}
}

func TestCheckLightKeepsStaticAndHashProtections(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")
	replaceInFile(t, repo+"/rule_test.go", `"forced failure"`, `"forced failure, edited"`)
	code, out := run2(t, "check", "--light", "--repo", repo)
	if code == 0 {
		t.Fatalf("light check must still detect a changed test span:\n%s", out)
	}
	repo = newValidationGoRepo(t, "unit")
	replaceInFile(t, repo+"/RULE-FLOOR.md", "FLOOR", "FLOOR-X")
	if code, out = run2(t, "check", "--light", "--repo", repo); code == 0 {
		t.Fatalf("light check must still validate the ledger:\n%s", out)
	}
}

func TestCheckLightRejectsRunProfile(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")
	code, out := run2(t, "check", "--light", "--run-profile", "unit", "--repo", repo)
	if code != 2 || !strings.Contains(out, "--light cannot be combined with --run-profile") {
		t.Fatalf("code=%d\n%s", code, out)
	}
}

func TestCheckLoadGuardTimesOutBeforeExecuting(t *testing.T) {
	repo := newFailingExecutionRepo(t)
	code, out := run2(t, "check", "--repo", repo, "--max-load", "0.0001", "--load-wait", "50ms", "--load-poll", "10ms")
	if code != 2 || !strings.Contains(out, "CANNOT-EVALUATE") || !strings.Contains(out, "load guard") {
		t.Fatalf("guard must stop the run deterministically: code=%d\n%s", code, out)
	}
	if strings.Contains(out, "FAIL JSON-VALIDATE-1") {
		t.Fatalf("test executed despite the guard:\n%s", out)
	}
}

func TestCheckLoadGuardPassesWhenLoadBelowLimit(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")
	out := mustRun(t, "check", "--repo", repo, "--max-load", "1000000", "--jobs", "2")
	if !strings.Contains(out, "check OK") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestCheckLoadGuardEnvIsHonouredAndFlagWins(t *testing.T) {
	repo := newFailingExecutionRepo(t)
	t.Setenv("RULEFLOOR_MAX_LOAD", "0.0001")
	t.Setenv("RULEFLOOR_LOAD_WAIT", "50ms")
	t.Setenv("RULEFLOOR_LOAD_POLL", "10ms")
	if code, out := run2(t, "check", "--repo", repo); code != 2 || !strings.Contains(out, "load guard") {
		t.Fatalf("env guard ignored: code=%d\n%s", code, out)
	}
	if code, out := run2(t, "check", "--repo", repo, "--max-load", "1000000"); code != 1 || !strings.Contains(out, "FAIL JSON-VALIDATE-1") {
		t.Fatalf("flag must override env: code=%d\n%s", code, out)
	}
}

func TestCheckLoadGuardRejectsBadValues(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")
	for _, args := range [][]string{
		{"--max-load", "abc"}, {"--max-load", "0"}, {"--max-load", "-3"}, {"--max-load", "NaN"},
		{"--jobs", "0"}, {"--jobs", "x"}, {"--load-wait", "0s"}, {"--load-wait", "soon"}, {"--load-poll", "-1s"},
	} {
		code, out := run2(t, append([]string{"check", "--repo", repo}, args...)...)
		if code != 2 {
			t.Fatalf("%v: code=%d, want 2\n%s", args, code, out)
		}
	}
}

func TestCheckHelpDocumentsLightAndGuard(t *testing.T) {
	code, stdout, stderr := runSeparate("check", "--help")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{"--light", "not a gate", "--max-load", "RULEFLOOR_MAX_LOAD", "--load-wait", "--jobs"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("check help missing %q:\n%s", want, stdout)
		}
	}
}
