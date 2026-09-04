package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
	"github.com/ozgurcd/rulefloor/internal/repository"
)

const checkTimingDetailLimit = 10

type ruleTiming struct {
	ID       string
	Duration time.Duration
}

type checkTimingReport struct {
	Total        time.Duration
	RuleTimings  []ruleTiming
	CompileTimes []checkengine.CompileTiming
}

type checkTimer struct {
	enabled bool
	started time.Time
	report  checkTimingReport
}

func newCheckTimer(enabled bool) *checkTimer {
	timer := &checkTimer{enabled: enabled}
	if enabled {
		timer.started = time.Now()
	}
	return timer
}

func (t *checkTimer) startRule() time.Time {
	if !t.enabled {
		return time.Time{}
	}
	return time.Now()
}

func (t *checkTimer) finishRule(id string, started time.Time) {
	if !t.enabled {
		return
	}
	t.report.RuleTimings = append(t.report.RuleTimings, ruleTiming{ID: id, Duration: time.Since(started)})
}

func (t *checkTimer) write(stdout io.Writer, repo string, executor *checkengine.CompiledGoTestExecutor) {
	if !t.enabled {
		return
	}
	t.report.Total = time.Since(t.started)
	if executor != nil {
		t.report.CompileTimes = executor.CompileTimings()
	}
	writeCheckTimings(stdout, canonicalTimingRoot(repo), t.report)
}

func writeCheckTimings(stdout io.Writer, repoRoot string, report checkTimingReport) {
	compileTotal := time.Duration(0)
	for _, timing := range report.CompileTimes {
		compileTotal += timing.Duration
	}
	fmt.Fprintf(stdout, "TIMINGS total=%s armed_rows=%d compile_total=%s compile_groups=%d\n",
		formatCheckDuration(report.Total), len(report.RuleTimings), formatCheckDuration(compileTotal), len(report.CompileTimes))

	compileTimes := append([]checkengine.CompileTiming(nil), report.CompileTimes...)
	sort.Slice(compileTimes, func(i, j int) bool {
		if compileTimes[i].Duration != compileTimes[j].Duration {
			return compileTimes[i].Duration > compileTimes[j].Duration
		}
		if compileTimes[i].Directory != compileTimes[j].Directory {
			return compileTimes[i].Directory < compileTimes[j].Directory
		}
		return compileTimes[i].Tags < compileTimes[j].Tags
	})
	shownCompile := min(len(compileTimes), checkTimingDetailLimit)
	for _, timing := range compileTimes[:shownCompile] {
		status := "failed"
		if timing.Succeeded {
			status = "passed"
		}
		tags := timing.Tags
		if tags == "" {
			tags = "-"
		}
		fmt.Fprintf(stdout, "TIMING compile package=%q tags=%q duration=%s status=%s\n",
			relativeTimingPath(repoRoot, timing.Directory), tags, formatCheckDuration(timing.Duration), status)
	}

	ruleTimes := append([]ruleTiming(nil), report.RuleTimings...)
	sort.Slice(ruleTimes, func(i, j int) bool {
		if ruleTimes[i].Duration != ruleTimes[j].Duration {
			return ruleTimes[i].Duration > ruleTimes[j].Duration
		}
		return ruleTimes[i].ID < ruleTimes[j].ID
	})
	shownRules := min(len(ruleTimes), checkTimingDetailLimit)
	for _, timing := range ruleTimes[:shownRules] {
		fmt.Fprintf(stdout, "TIMING rule id=%s duration=%s\n", timing.ID, formatCheckDuration(timing.Duration))
	}
	fmt.Fprintf(stdout, "TIMINGS shown compile_groups=%d/%d slow_rows=%d/%d\n",
		shownCompile, len(compileTimes), shownRules, len(ruleTimes))
}

func formatCheckDuration(duration time.Duration) string {
	if duration > 0 && duration < time.Millisecond {
		return "<1ms"
	}
	return duration.Round(time.Millisecond).String()
}

func relativeTimingPath(repoRoot, directory string) string {
	relative, err := filepath.Rel(repoRoot, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "(outside-repository)"
	}
	if relative == "" {
		return "."
	}
	return filepath.ToSlash(relative)
}

func canonicalTimingRoot(repo string) string {
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return repo
	}
	return root
}
