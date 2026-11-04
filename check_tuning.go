package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	checkengine "github.com/ozgurcd/rulefloor/internal/check"
)

const (
	envMaxLoad  = "RULEFLOOR_MAX_LOAD"
	envLoadWait = "RULEFLOOR_LOAD_WAIT"
	envLoadPoll = "RULEFLOOR_LOAD_POLL"
	envJobs     = "RULEFLOOR_JOBS"

	// defaultLoadWait bounds how long the guard waits for the host to calm down
	// before the check stops with CANNOT-EVALUATE.
	defaultLoadWait = 5 * time.Minute
)

// checkTuning holds the optional host-protection settings of `check`. The zero
// value means no guard and the Go default build parallelism.
type checkTuning struct {
	MaxLoad float64
	Wait    time.Duration
	Poll    time.Duration
	Jobs    int
}

func (t checkTuning) guarded() bool { return t.MaxLoad > 0 }

func (t checkTuning) active() bool { return t.guarded() || t.Jobs > 0 }

func (t checkTuning) guard() checkengine.LoadGuard {
	return checkengine.LoadGuard{MaxLoad: t.MaxLoad, Budget: t.Wait, Poll: t.Poll}
}

// parseCheckTuning resolves the guard settings; a flag wins over its
// RULEFLOOR_* environment variable. Bad values are usage errors (exit 2).
func parseCheckTuning(flags map[string]string) (checkTuning, error) {
	var t checkTuning
	pick := func(flag, env string) (string, string) {
		if v, ok := flags[flag]; ok {
			return v, flag
		}
		return os.Getenv(env), env
	}
	if v, name := pick("--max-load", envMaxLoad); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
			return t, fatalf("check: %s must be a number > 0, got %q", name, v)
		}
		t.MaxLoad = f
	}
	if v, name := pick("--jobs", envJobs); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return t, fatalf("check: %s must be an integer >= 1, got %q", name, v)
		}
		t.Jobs = n
	}
	var err error
	if t.Wait, err = tuningDuration(pick("--load-wait", envLoadWait)); err != nil {
		return t, err
	}
	if t.Poll, err = tuningDuration(pick("--load-poll", envLoadPoll)); err != nil {
		return t, err
	}
	if t.Wait == 0 {
		t.Wait = defaultLoadWait
	}
	return t, nil
}

func tuningDuration(v, name string) (time.Duration, error) {
	if v == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fatalf("check: %s must be a positive duration such as 30s, got %q", name, v)
	}
	return d, nil
}

// newCheckExecutor builds the compiled executor for one check operation and,
// when a load guard is configured, the guarded wrapper to hand to rows. The
// compiled executor stays separate because timings read it.
func newCheckExecutor(t checkTuning) (*checkengine.CompiledGoTestExecutor, checkengine.GoTestExecutor) {
	compiled := checkengine.NewCompiledGoTestExecutor(checkengine.ExecRunner{})
	compiled.SetCompileJobs(t.Jobs)
	if t.guarded() {
		return compiled, checkengine.NewGuardedExecutor(compiled, t.guard())
	}
	return compiled, compiled
}

func lightSummary(skipped int) string {
	return fmt.Sprintf("LIGHT: %d test execution(s) skipped; ledger, hash, static and graph checks only — not a gate (run without --light)", skipped)
}
