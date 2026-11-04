package check

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// fakeClock drives LoadGuard deterministically: Sleep advances Now, no real waiting.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	return nil
}

func scriptedGuard(loads []float64, max float64, wait, poll time.Duration) (LoadGuard, *fakeClock, *int) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	reads := 0
	source := func() (float64, error) {
		i := reads
		if i >= len(loads) {
			i = len(loads) - 1
		}
		reads++
		return loads[i], nil
	}
	return LoadGuard{MaxLoad: max, Budget: wait, Poll: poll, Source: source, Now: clock.Now, Sleep: clock.Sleep}, clock, &reads
}

func TestLoadGuardProceedsImmediatelyBelowLimit(t *testing.T) {
	guard, clock, reads := scriptedGuard([]float64{15.99}, 16, time.Minute, time.Second)
	start := clock.now
	if err := guard.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *reads != 1 || !clock.now.Equal(start) {
		t.Fatalf("reads=%d slept=%v", *reads, clock.now.Sub(start))
	}
}

func TestLoadGuardWaitsWhileAtOrAboveLimitThenProceeds(t *testing.T) {
	guard, clock, reads := scriptedGuard([]float64{16, 20, 16.0, 3}, 16, time.Minute, 5*time.Second)
	start := clock.now
	if err := guard.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *reads != 4 || clock.now.Sub(start) != 15*time.Second {
		t.Fatalf("reads=%d slept=%v, want 4 reads and 15s", *reads, clock.now.Sub(start))
	}
}

func TestLoadGuardTimesOutAtBoundary(t *testing.T) {
	guard, clock, _ := scriptedGuard([]float64{99}, 16, 10*time.Second, 5*time.Second)
	start := clock.now
	err := guard.Wait(context.Background())
	var timeout *LoadGuardTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("err = %v, want LoadGuardTimeoutError", err)
	}
	if timeout.Load != 99 || timeout.Max != 16 || clock.now.Sub(start) > 10*time.Second {
		t.Fatalf("timeout %+v after %v", timeout, clock.now.Sub(start))
	}
}

func TestLoadGuardSourceErrorFailsDeterministically(t *testing.T) {
	boom := errors.New("no load source")
	guard := LoadGuard{MaxLoad: 16, Budget: time.Second, Poll: time.Millisecond, Source: func() (float64, error) { return 0, boom }}
	if err := guard.Wait(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want source error", err)
	}
}

func TestLoadGuardHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	guard := LoadGuard{MaxLoad: 1, Budget: time.Hour, Poll: time.Hour, Source: func() (float64, error) { return 50, nil }}
	if err := guard.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestParseLoad1Formats(t *testing.T) {
	for in, want := range map[string]float64{
		"{ 7.26 5.26 6.56 }\n":         7.26,
		"0.52 0.58 0.59 1/467 12345\n": 0.52,
		"  12 3 4":                     12,
	} {
		got, err := ParseLoad1(in)
		if err != nil || got != want {
			t.Fatalf("ParseLoad1(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "{ }", "abc 1 2", "NaN 1 2", "-1 0 0", "Inf 1 1"} {
		if _, err := ParseLoad1(in); err == nil {
			t.Fatalf("ParseLoad1(%q) accepted", in)
		}
	}
}

type countingExecutor struct {
	runs   int
	closed bool
}

func (e *countingExecutor) Run(context.Context, string, string, string) Execution {
	e.runs++
	return Execution{Status: ExecutionPass, Performed: true, Reason: "rule_passed"}
}
func (e *countingExecutor) Close() error { e.closed = true; return nil }

func TestGuardedExecutorBlocksRunOnTimeoutAndDelegatesOtherwise(t *testing.T) {
	inner := &countingExecutor{}
	busy, _, _ := scriptedGuard([]float64{99}, 16, time.Second, time.Second)
	blocked := NewGuardedExecutor(inner, busy)
	got := blocked.Run(context.Background(), "/r/x_test.go", "TestX", "")
	if got.Status != ExecutionCannotEvaluate || got.Reason != LoadGuardReason || inner.runs != 0 {
		t.Fatalf("busy host: %+v runs=%d", got, inner.runs)
	}
	idle, _, _ := scriptedGuard([]float64{1}, 16, time.Second, time.Second)
	open := NewGuardedExecutor(inner, idle)
	if got := open.Run(context.Background(), "/r/x_test.go", "TestX", ""); got.Status != ExecutionPass || inner.runs != 1 {
		t.Fatalf("idle host: %+v runs=%d", got, inner.runs)
	}
	if err := open.Close(); err != nil || !inner.closed {
		t.Fatalf("close not delegated: %v closed=%v", err, inner.closed)
	}
}

func TestCompiledExecutorCompileJobsAddsParallelismFlagOnlyWhenSet(t *testing.T) {
	for _, tc := range []struct {
		jobs int
		want []string
	}{
		{0, []string{"test", "-c", "-o"}},
		{3, []string{"test", "-c", "-p", "3", "-o"}},
	} {
		runner := &compiledRunner{}
		executor := NewCompiledGoTestExecutor(runner)
		executor.SetCompileJobs(tc.jobs)
		executor.Run(context.Background(), "/repo/pkg/bound_test.go", "TestOne", "")
		_ = executor.Close()
		if got := runner.calls[0].args[:len(tc.want)]; !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("jobs=%d compile args prefix = %v, want %v", tc.jobs, got, tc.want)
		}
	}
}
