//go:build unix

package check

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The tests below start this test binary again as a helper process. The
// helper's role comes from the environment, so no test sleeps to order events:
// a helper reports that it is ready by writing a file.
const (
	helperModeEnv  = "RULEFLOOR_BOUNDS_HELPER"
	helperReadyEnv = "RULEFLOOR_BOUNDS_READY"
	helperFillEnv  = "RULEFLOOR_BOUNDS_FILL"
	helperTextEnv  = "RULEFLOOR_BOUNDS_TEXT"
	// helperLifetime ends a helper that nothing stopped, so a failed test
	// leaves no process behind for long.
	helperLifetime = 30 * time.Second
	helperPatience = 20 * time.Second
)

var helperArgs = []string{"-test.run=^TestHelperProcess$"}

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperModeEnv)
	if mode == "" {
		return
	}
	os.Exit(runHelper(mode))
}

func runHelper(mode string) int {
	switch mode {
	case "print":
		fill, _ := strconv.Atoi(os.Getenv(helperFillEnv))
		line := []byte(strings.Repeat("x", 63) + "\n")
		for written := 0; written < fill; written += len(line) {
			_, _ = os.Stdout.Write(line[:min(len(line), fill-written)])
		}
		printHelperText()
	case "sleep":
		time.Sleep(helperLifetime)
	case "leader":
		// Stays alive with a descendant that shares its output pipe.
		spawnHelperDescendant()
		time.Sleep(helperLifetime)
	case "leak":
		// Prints, leaves a descendant holding the output pipe, and exits 0.
		printHelperText()
		spawnHelperDescendant()
	case "host":
		// A stand-in for Rulefloor: it runs a leader through the real runner.
		_ = os.Setenv(helperModeEnv, "leader")
		_, _ = ExecRunner{}.RunBounded(context.Background(), "", os.Args[0], helperArgs, nil)
	default:
		return 2
	}
	return 0
}

func printHelperText() {
	text, err := strconv.Unquote(os.Getenv(helperTextEnv))
	if err == nil {
		_, _ = os.Stdout.WriteString(text)
	}
}

func spawnHelperDescendant() {
	descendant := exec.Command(os.Args[0], helperArgs...)
	descendant.Env = append(os.Environ(), helperModeEnv+"=sleep")
	descendant.Stdout = os.Stdout
	if err := descendant.Start(); err != nil {
		os.Exit(3)
	}
	ready := os.Getenv(helperReadyEnv)
	pids := fmt.Sprintf("%d %d", os.Getpid(), descendant.Process.Pid)
	if err := os.WriteFile(ready+".tmp", []byte(pids), 0o600); err != nil {
		os.Exit(3)
	}
	if err := os.Rename(ready+".tmp", ready); err != nil {
		os.Exit(3)
	}
}

// helperPIDs waits for a helper to report itself and its descendant, and
// makes sure neither outlives the test.
func helperPIDs(t *testing.T, ready string) (int, int) {
	t.Helper()
	limit := time.Now().Add(helperPatience)
	for {
		data, err := os.ReadFile(ready)
		if err == nil {
			var parent, descendant int
			if _, err := fmt.Sscanf(string(data), "%d %d", &parent, &descendant); err != nil {
				t.Fatalf("helper ready file %q: %v", data, err)
			}
			t.Cleanup(func() {
				for _, pid := range []int{parent, descendant} {
					if !processGone(pid) {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			})
			return parent, descendant
		}
		if time.Now().After(limit) {
			t.Fatalf("helper was not ready within %s: %v", helperPatience, err)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// processGone reports that pid no longer runs: it does not exist, or (Linux)
// it is a killed process that its new parent has not collected yet.
func processGone(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	state := stat[bytes.LastIndexByte(stat, ')')+1:]
	return strings.HasPrefix(strings.TrimSpace(string(state)), "Z")
}

func waitProcessGone(pid int) bool {
	limit := time.Now().Add(helperPatience)
	for !processGone(pid) {
		if time.Now().After(limit) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
	return true
}

func setHelper(t *testing.T, mode string, fill int, text string) string {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv(helperModeEnv, mode)
	t.Setenv(helperReadyEnv, ready)
	t.Setenv(helperFillEnv, strconv.Itoa(fill))
	t.Setenv(helperTextEnv, strconv.Quote(text))
	// A race-enabled helper would otherwise sleep one second when it exits.
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	return ready
}

// expiringContext reaches its deadline when the test says so.
type expiringContext struct {
	context.Context
	expired chan struct{}
}

func newExpiringContext() expiringContext {
	return expiringContext{Context: context.Background(), expired: make(chan struct{})}
}

func (c expiringContext) Done() <-chan struct{} { return c.expired }

func (c expiringContext) Err() error {
	select {
	case <-c.expired:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

type boundedOutcome struct {
	capture Capture
	err     error
}

// runUntilExpired starts a leader helper, waits until it and its descendant
// run, then expires the context and returns what RunBounded reported.
func runUntilExpired(t *testing.T) (expiringContext, boundedOutcome, int, int) {
	t.Helper()
	ready := setHelper(t, "leader", 0, "")
	ctx := newExpiringContext()
	finished := make(chan boundedOutcome, 1)
	go func() {
		capture, err := ExecRunner{}.RunBounded(ctx, "", os.Args[0], helperArgs, goTestMarkers("TestBound"))
		finished <- boundedOutcome{capture, err}
	}()
	leader, descendant := helperPIDs(t, ready)
	close(ctx.expired)
	select {
	case outcome := <-finished:
		return ctx, outcome, leader, descendant
	case <-time.After(helperPatience):
		t.Fatalf("RunBounded did not return within %s of the deadline", helperPatience)
		return ctx, boundedOutcome{}, leader, descendant
	}
}

// RULE: CAPTURE-LIMIT-1
func TestCapturedOutputNeverExceedsTheCaptureLimit(t *testing.T) {
	const fill = 4 * CaptureLimitBytes
	verdict := "--- FAIL: TestBound (0.00s)\n--- PASS: TestBound (0.00s)\n"
	setHelper(t, "print", fill, verdict)
	capture, err := ExecRunner{}.RunBounded(context.Background(), "", os.Args[0], helperArgs, goTestMarkers("TestBound"))
	if err != nil {
		t.Fatalf("helper failed: %v", err)
	}
	if len(capture.Output) > CaptureLimitBytes {
		t.Fatalf("kept %d bytes of output, limit is %d", len(capture.Output), CaptureLimitBytes)
	}
	if want := int64(fill + len(verdict)); capture.Total != want || !capture.Truncated() || capture.Incomplete {
		t.Fatalf("capture total %d truncated %v incomplete %v, want total %d, truncated, complete", capture.Total, capture.Truncated(), capture.Incomplete, want)
	}
	// The failure line lies in the dropped part; the verdict still sees it.
	if execution := interpretGoTestResult(context.Background(), capture, err, false); execution.Status != ExecutionFail {
		t.Fatalf("a failure past the limit was lost: %+v", execution)
	}
}

func TestPassAndRawBytesSurviveTheCaptureLimit(t *testing.T) {
	raw := "\x00\xff\xfe not text\n"
	setHelper(t, "print", 2*CaptureLimitBytes, raw+"--- PASS: TestBound (0.00s)\n")
	capture, err := ExecRunner{}.RunBounded(context.Background(), "", os.Args[0], helperArgs, append(goTestMarkers("TestBound"), raw))
	if err != nil || !capture.Truncated() || !capture.found(3) {
		t.Fatalf("capture = %d of %d bytes, raw bytes found %v, %v", len(capture.Output), capture.Total, capture.found(3), err)
	}
	if execution := interpretGoTestResult(context.Background(), capture, err, false); execution.Status != ExecutionPass {
		t.Fatalf("a pass read to the end of the stream was lost: %+v", execution)
	}
}

// RULE: CAPTURE-PASS-1
func TestCutCaptureNeverYieldsPass(t *testing.T) {
	background := context.Background()
	cut := interpretGoTestResult(background, Capture{Incomplete: true, Found: []bool{false, false, true}}, nil, false)
	if cut.Status != ExecutionCannotEvaluate || cut.Reason != OutputIncompleteReason {
		t.Fatalf("pass line in a cut stream: %+v", cut)
	}
	failed := interpretGoTestResult(background, Capture{Incomplete: true, Found: []bool{false, true, true}}, nil, false)
	if failed.Status != ExecutionFail {
		t.Fatalf("failure seen before the cut: %+v", failed)
	}

	// A real child prints a pass line and exits 0 while its descendant keeps
	// the pipe open: the stream never ends, so the pass line proves nothing.
	ready := setHelper(t, "leak", 0, "--- PASS: TestBound (0.00s)\n")
	started := time.Now()
	capture, err := ExecRunner{DrainWait: 100 * time.Millisecond}.RunBounded(background, "", os.Args[0], helperArgs, goTestMarkers("TestBound"))
	helperPIDs(t, ready)
	if err != nil || !capture.Incomplete {
		t.Fatalf("capture incomplete %v, err %v; want an incomplete capture from a child that exited 0", capture.Incomplete, err)
	}
	if elapsed := time.Since(started); elapsed > helperPatience {
		t.Fatalf("RunBounded waited %s on a descendant that held the pipe", elapsed)
	}
	execution := interpretGoTestResult(background, capture, err, false)
	if execution.Status != ExecutionCannotEvaluate || execution.Reason != OutputIncompleteReason {
		t.Fatalf("cut capture from a real child: %+v", execution)
	}
}

// RULE: DEADLINE-GROUP-1
func TestDeadlineLeavesNoDescendantRunning(t *testing.T) {
	ctx, outcome, leader, descendant := runUntilExpired(t)
	if !waitProcessGone(leader) || !waitProcessGone(descendant) {
		t.Fatalf("after the deadline: leader %d gone %v, descendant %d gone %v", leader, processGone(leader), descendant, processGone(descendant))
	}
	if outcome.capture.Incomplete {
		t.Fatalf("the pipe was still held after the group stop: %+v", outcome)
	}
	execution := interpretGoTestResult(ctx, outcome.capture, outcome.err, false)
	if execution.Status != ExecutionCannotEvaluate || execution.Reason != "deadline_exceeded" {
		t.Fatalf("deadline result = %+v", execution)
	}
}

func TestRunBoundedLeavesNoGoroutineOnAnyPath(t *testing.T) {
	// The first signal.Notify starts a process-wide reader that never ends;
	// start it here so that it is not counted against a path.
	warm := make(chan os.Signal, 1)
	signal.Notify(warm, syscall.SIGUSR1)
	signal.Stop(warm)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	paths := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"child exit", func(t *testing.T) {
			setHelper(t, "print", 2*CaptureLimitBytes, "done\n")
			_, _ = ExecRunner{}.RunBounded(context.Background(), "", os.Args[0], helperArgs, goTestMarkers("TestBound"))
		}},
		{"start failure", func(t *testing.T) {
			if _, err := (ExecRunner{}).RunBounded(context.Background(), "", filepath.Join(t.TempDir(), "absent"), nil, nil); err == nil {
				t.Fatal("an absent binary started")
			}
		}},
		{"context ended before start", func(t *testing.T) {
			if _, err := (ExecRunner{}).RunBounded(canceled, "", os.Args[0], helperArgs, nil); err == nil {
				t.Fatal("a canceled context started a child")
			}
		}},
		{"deadline", func(t *testing.T) { runUntilExpired(t) }},
		{"descendant holds the pipe", func(t *testing.T) {
			ready := setHelper(t, "leak", 0, "")
			_, _ = ExecRunner{DrainWait: 50 * time.Millisecond}.RunBounded(context.Background(), "", os.Args[0], helperArgs, nil)
			helperPIDs(t, ready)
		}},
	}
	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			before := runtime.NumGoroutine()
			path.run(t)
			limit := time.Now().Add(2 * time.Second)
			for runtime.NumGoroutine() > before && time.Now().Before(limit) {
				time.Sleep(2 * time.Millisecond)
			}
			if after := runtime.NumGoroutine(); after > before {
				stacks := make([]byte, 1<<16)
				t.Fatalf("goroutines: %d before, %d after\n%s", before, after, stacks[:runtime.Stack(stacks, true)])
			}
		})
	}
}

func TestTerminationSignalStopsTheChildGroup(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	host := exec.Command(os.Args[0], helperArgs...)
	host.Env = append(os.Environ(), helperModeEnv+"=host", helperReadyEnv+"="+ready)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Process.Kill() })
	leader, descendant := helperPIDs(t, ready)
	if err := host.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err := host.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("host ended with %v, want death by SIGTERM", err)
	}
	if status := exit.Sys().(syscall.WaitStatus); !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("host ended with %v, want death by SIGTERM", err)
	}
	if !waitProcessGone(leader) || !waitProcessGone(descendant) {
		t.Fatalf("after SIGTERM: leader %d gone %v, descendant %d gone %v", leader, processGone(leader), descendant, processGone(descendant))
	}
}
