package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
)

func TestCheckTimingsAreOptInAndBounded(t *testing.T) {
	repo := newValidationGoRepo(t, "unit")

	ordinary := mustRun(t, "check", "--repo", repo)
	if strings.Contains(ordinary, "TIMING") {
		t.Fatalf("ordinary check unexpectedly emitted timings:\n%s", ordinary)
	}

	output := mustRun(t, "check", "--timings", "--repo", repo)
	for _, want := range []string{
		"TIMINGS total=",
		"armed_rows=1",
		"compile_groups=1",
		`TIMING compile package="." tags="-"`,
		"TIMING rule id=JSON-VALIDATE-1",
		"TIMINGS shown compile_groups=1/1 slow_rows=1/1",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("timed check output missing %q:\n%s", want, output)
		}
	}
}

func TestCheckTimingFormatterOrdersAndBoundsDetails(t *testing.T) {
	repoRoot := filepath.Join(string(filepath.Separator), "repo")
	report := checkTimingReport{Total: 15 * time.Second}
	for i := 1; i <= 12; i++ {
		duration := time.Duration(i) * time.Second
		report.RuleTimings = append(report.RuleTimings, ruleTiming{ID: fmt.Sprintf("R-%02d", i), Duration: duration})
		report.CompileTimes = append(report.CompileTimes, checkengine.CompileTiming{
			Directory: filepath.Join(repoRoot, fmt.Sprintf("pkg%02d", i)),
			Duration:  duration,
			Succeeded: true,
		})
	}

	var output bytes.Buffer
	writeCheckTimings(&output, repoRoot, report)
	text := output.String()
	if got := strings.Count(text, "TIMING compile "); got != checkTimingDetailLimit {
		t.Fatalf("compile detail count=%d, want %d:\n%s", got, checkTimingDetailLimit, text)
	}
	if got := strings.Count(text, "TIMING rule "); got != checkTimingDetailLimit {
		t.Fatalf("rule detail count=%d, want %d:\n%s", got, checkTimingDetailLimit, text)
	}
	for _, want := range []string{
		`TIMING compile package="pkg12"`,
		"TIMING rule id=R-12",
		"TIMINGS shown compile_groups=10/12 slow_rows=10/12",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("timing output missing %q:\n%s", want, text)
		}
	}
	for _, omitted := range []string{`package="pkg01"`, "id=R-01"} {
		if strings.Contains(text, omitted) {
			t.Fatalf("bounded timing output contains %q:\n%s", omitted, text)
		}
	}
}

func TestCheckHelpDocumentsTimingScope(t *testing.T) {
	code, stdout, stderr := runSeparate("check", "--help")
	if code != 0 || stderr != "" {
		t.Fatalf("check help: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, want := range []string{"--timings", "10 slowest row", "static, graph, and any executed-test work"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("check help missing %q:\n%s", want, stdout)
		}
	}
}

func TestCheckDurationFormatting(t *testing.T) {
	if got := formatCheckDuration(0); got != "0s" {
		t.Fatalf("zero duration=%q", got)
	}
	if got := formatCheckDuration(time.Microsecond); got != "<1ms" {
		t.Fatalf("sub-millisecond duration=%q", got)
	}
}
