package check

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// GoTestExecutor runs one selected Go test in an isolated process.
type GoTestExecutor interface {
	Run(context.Context, string, string, string) Execution
	Close() error
}

// CompiledGoTestExecutor compiles one test binary per package and build-tag set,
// then starts a fresh process for every selected test. Its cache is intentionally
// scoped to the executor lifetime; callers should create one per check operation.
type CompiledGoTestExecutor struct {
	runner         CommandRunner
	tempDir        string
	builds         map[goTestBuildKey]goTestBuild
	nextName       int
	compileTimings []CompileTiming
	compileJobs    int
}

// SetCompileJobs caps `go test -c` build parallelism (-p). Zero keeps the Go
// default. Call before the first Run.
func (e *CompiledGoTestExecutor) SetCompileJobs(jobs int) { e.compileJobs = jobs }

// CompileTiming describes one package compilation attempted by a compiled
// executor. Cached test executions do not add another entry.
type CompileTiming struct {
	Directory string
	Tags      string
	Duration  time.Duration
	Succeeded bool
}

type goTestBuildKey struct {
	directory string
	tags      string
}

type goTestBuild struct {
	binary  string
	failure *Execution
}

// NewCompiledGoTestExecutor returns a lazy executor. No filesystem or toolchain
// access occurs until the first Run call.
func NewCompiledGoTestExecutor(runner CommandRunner) *CompiledGoTestExecutor {
	return &CompiledGoTestExecutor{runner: runner, builds: make(map[goTestBuildKey]goTestBuild)}
}

// Run executes exactly one selected test. Compilation is reused, execution is not.
func (e *CompiledGoTestExecutor) Run(ctx context.Context, testFile, functionName, tags string) Execution {
	key := goTestBuildKey{directory: filepath.Dir(testFile), tags: tags}
	build, ok := e.builds[key]
	if !ok {
		build = e.compile(ctx, key)
		e.builds[key] = build
	}
	if build.failure != nil {
		return *build.failure
	}

	args := []string{"-test.count=1", "-test.v", "-test.paniconexit0", "-test.timeout=10m0s", "-test.run", "^" + functionName + "$"}
	capture, err := runBounded(ctx, e.runner, key.directory, build.binary, args, goTestMarkers(functionName))
	return interpretGoTestResult(ctx, capture, err, false)
}

func (e *CompiledGoTestExecutor) compile(ctx context.Context, key goTestBuildKey) goTestBuild {
	started := time.Now()
	succeeded := false
	defer func() {
		e.compileTimings = append(e.compileTimings, CompileTiming{
			Directory: key.directory,
			Tags:      key.tags,
			Duration:  time.Since(started),
			Succeeded: succeeded,
		})
	}()

	if e.tempDir == "" {
		tempDir, err := os.MkdirTemp("", "rulefloor-go-test-")
		if err != nil {
			failure := Execution{Status: ExecutionCannotEvaluate, Reason: "execution_failed", Message: BoundedMessage(err.Error())}
			return goTestBuild{failure: &failure}
		}
		e.tempDir = tempDir
	}

	e.nextName++
	name := fmt.Sprintf("package-%d.test", e.nextName)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(e.tempDir, name)
	args := []string{"test", "-c"}
	if e.compileJobs > 0 {
		args = append(args, "-p", strconv.Itoa(e.compileJobs))
	}
	args = append(args, "-o", binary)
	if key.tags != "" {
		args = append(args, "-tags", key.tags)
	}
	args = append(args, ".")
	capture, err := runBounded(ctx, e.runner, key.directory, "go", args, nil)
	if err == nil {
		succeeded = true
		return goTestBuild{binary: binary}
	}

	failure := interpretGoTestCompileFailure(ctx, capture.Output, err)
	return goTestBuild{failure: &failure}
}

// CompileTimings returns a snapshot of package compilation timings in attempt
// order. It is diagnostic data only and does not affect execution semantics.
func (e *CompiledGoTestExecutor) CompileTimings() []CompileTiming {
	return append([]CompileTiming(nil), e.compileTimings...)
}

// Close removes test binaries created by this executor.
func (e *CompiledGoTestExecutor) Close() error {
	if e.tempDir == "" {
		return nil
	}
	tempDir := e.tempDir
	e.tempDir = ""
	e.builds = make(map[goTestBuildKey]goTestBuild)
	return os.RemoveAll(tempDir)
}

func interpretGoTestCompileFailure(ctx context.Context, output []byte, err error) Execution {
	text := string(output)
	if contextErr := ctx.Err(); contextErr != nil {
		reason := "context_canceled"
		if errors.Is(contextErr, context.DeadlineExceeded) {
			reason = "deadline_exceeded"
		}
		return Execution{Status: ExecutionCannotEvaluate, Reason: reason, Message: BoundedDiagnostic(text, contextErr.Error())}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return Execution{Status: ExecutionCannotEvaluate, Reason: "toolchain_unavailable", Message: BoundedMessage(err.Error())}
	}
	return Execution{Status: ExecutionCannotEvaluate, Reason: "execution_failed", Message: BoundedDiagnostic(text, err.Error())}
}

// The verdict markers of one selected test, in the order interpretGoTestResult
// reads them. Each is matched as a plain substring of the output, as before.
const (
	markerSkip = iota
	markerFail
	markerPass
)

func goTestMarkers(functionName string) []string {
	return []string{"--- SKIP: " + functionName, "--- FAIL: " + functionName, "--- PASS: " + functionName}
}

func interpretGoTestResult(ctx context.Context, capture Capture, err error, toolchainCommand bool) Execution {
	text := string(capture.Output)
	if contextErr := ctx.Err(); contextErr != nil {
		reason := "context_canceled"
		if errors.Is(contextErr, context.DeadlineExceeded) {
			reason = "deadline_exceeded"
		}
		return Execution{Status: ExecutionCannotEvaluate, Reason: reason, Message: BoundedDiagnostic(text, contextErr.Error())}
	}
	if capture.found(markerSkip) {
		return Execution{Status: ExecutionCannotEvaluate, Performed: true, Reason: "test_skipped", Message: BoundedDiagnostic(text, "test skipped at runtime")}
	}
	if capture.found(markerFail) {
		return Execution{Status: ExecutionFail, Performed: true, Reason: "execution_failed", Message: BoundedDiagnostic(text, "bound test failed")}
	}
	if err != nil {
		if toolchainCommand && errors.Is(err, exec.ErrNotFound) {
			return Execution{Status: ExecutionCannotEvaluate, Reason: "toolchain_unavailable", Message: BoundedMessage(err.Error())}
		}
		return Execution{Status: ExecutionCannotEvaluate, Reason: "execution_failed", Message: BoundedDiagnostic(text, err.Error())}
	}
	if capture.Incomplete {
		// The child exited 0 but its output was cut before the end. A skip or a
		// failure may lie in the unread part, so a pass line proves nothing.
		return Execution{Status: ExecutionCannotEvaluate, Performed: true, Reason: OutputIncompleteReason, Message: BoundedMessage("output was not read to its end, so a pass cannot be established: a process started by the test still held the output pipe\n" + text)}
	}
	if !capture.found(markerPass) {
		return Execution{Status: ExecutionCannotEvaluate, Performed: true, Reason: "execution_failed", Message: BoundedDiagnostic(text, "go test did not report the selected test as passed")}
	}
	return Execution{Status: ExecutionPass, Performed: true, Reason: "rule_passed"}
}
