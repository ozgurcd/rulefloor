package check

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// LoadGuardReason is the Execution.Reason reported when the host stays above
// the configured load limit for the whole wait budget.
const LoadGuardReason = "load_guard"

// DefaultLoadPoll is the polling interval used when LoadGuard.Poll is zero.
const DefaultLoadPoll = 5 * time.Second

// LoadGuard blocks until the one-minute load average is below MaxLoad. It is
// immutable after construction and holds no goroutines: Wait polls on the
// caller's goroutine, bounded by Wait and by the context.
type LoadGuard struct {
	MaxLoad float64
	Budget  time.Duration
	Poll    time.Duration
	// Source, Now and Sleep default to the host load, time.Now and a timer;
	// tests inject deterministic versions.
	Source func() (float64, error)
	Now    func() time.Time
	Sleep  func(context.Context, time.Duration) error
}

// LoadGuardTimeoutError reports that the load stayed at or above Max for Waited.
type LoadGuardTimeoutError struct {
	Load, Max float64
	Waited    time.Duration
}

func (e *LoadGuardTimeoutError) Error() string {
	return fmt.Sprintf("load guard: load1 %.2f stayed at or above --max-load %.2f for %s", e.Load, e.Max, e.Waited)
}

// Wait returns nil once load1 < MaxLoad. It returns a *LoadGuardTimeoutError
// when the budget is spent, the context error on cancellation, and the source
// error when the load cannot be read (a guard that cannot measure must not
// silently pass).
func (g LoadGuard) Wait(ctx context.Context) error {
	source, now, sleep, poll := g.Source, g.Now, g.Sleep, g.Poll
	if source == nil {
		source = SystemLoad1
	}
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = sleepContext
	}
	if poll <= 0 {
		poll = DefaultLoadPoll
	}
	started := now()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		load, err := source()
		if err != nil {
			return fmt.Errorf("load guard: cannot read load average: %w", err)
		}
		if load < g.MaxLoad {
			return nil
		}
		if waited := now().Sub(started); waited >= g.Budget {
			return &LoadGuardTimeoutError{Load: load, Max: g.MaxLoad, Waited: waited}
		}
		if err := sleep(ctx, poll); err != nil {
			return err
		}
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// SystemLoad1 reads the one-minute load average from /proc/loadavg (Linux) or
// `sysctl -n vm.loadavg` (macOS and BSD).
func SystemLoad1() (float64, error) {
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		return ParseLoad1(string(data))
	}
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return 0, fmt.Errorf("no /proc/loadavg and sysctl vm.loadavg failed: %w", err)
	}
	return ParseLoad1(string(out))
}

// ParseLoad1 extracts the first (one-minute) value from "{ 7.26 5.26 6.56 }"
// or "0.52 0.58 0.59 1/467 12345".
func ParseLoad1(text string) (float64, error) {
	fields := strings.Fields(strings.Trim(strings.TrimSpace(text), "{}"))
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty load average %q", text)
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(load) || math.IsInf(load, 0) || load < 0 {
		return 0, fmt.Errorf("invalid load average %q", text)
	}
	return load, nil
}

// GuardedExecutor waits on a LoadGuard before every test execution (which
// includes the lazy package compile) and reports CANNOT-EVALUATE, without
// running anything, when the guard fails.
type GuardedExecutor struct {
	inner GoTestExecutor
	guard LoadGuard
}

func NewGuardedExecutor(inner GoTestExecutor, guard LoadGuard) *GuardedExecutor {
	return &GuardedExecutor{inner: inner, guard: guard}
}

func (e *GuardedExecutor) Run(ctx context.Context, testFile, functionName, tags string) Execution {
	if err := e.guard.Wait(ctx); err != nil {
		return Execution{Status: ExecutionCannotEvaluate, Reason: LoadGuardReason, Message: BoundedMessage(err.Error())}
	}
	return e.inner.Run(ctx, testFile, functionName, tags)
}

func (e *GuardedExecutor) Close() error { return e.inner.Close() }
