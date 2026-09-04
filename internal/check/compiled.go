package check

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
}

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
	output, err := e.runner.Run(ctx, key.directory, build.binary, args...)
	return interpretGoTestResult(ctx, output, err, functionName, false)
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
	args := []string{"test", "-c", "-o", binary}
	if key.tags != "" {
		args = append(args, "-tags", key.tags)
	}
	args = append(args, ".")
	output, err := e.runner.Run(ctx, key.directory, "go", args...)
	if err == nil {
		succeeded = true
		return goTestBuild{binary: binary}
	}

	failure := interpretGoTestCompileFailure(ctx, output, err)
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

func interpretGoTestResult(ctx context.Context, output []byte, err error, functionName string, toolchainCommand bool) Execution {
	text := string(output)
	if contextErr := ctx.Err(); contextErr != nil {
		reason := "context_canceled"
		if errors.Is(contextErr, context.DeadlineExceeded) {
			reason = "deadline_exceeded"
		}
		return Execution{Status: ExecutionCannotEvaluate, Reason: reason, Message: BoundedDiagnostic(text, contextErr.Error())}
	}
	if strings.Contains(text, "--- SKIP: "+functionName) {
		return Execution{Status: ExecutionCannotEvaluate, Performed: true, Reason: "test_skipped", Message: BoundedDiagnostic(text, "test skipped at runtime")}
	}
	if strings.Contains(text, "--- FAIL: "+functionName) {
		return Execution{Status: ExecutionFail, Performed: true, Reason: "execution_failed", Message: BoundedDiagnostic(text, "bound test failed")}
	}
	if err != nil {
		if toolchainCommand && errors.Is(err, exec.ErrNotFound) {
			return Execution{Status: ExecutionCannotEvaluate, Reason: "toolchain_unavailable", Message: BoundedMessage(err.Error())}
		}
		return Execution{Status: ExecutionCannotEvaluate, Reason: "execution_failed", Message: BoundedDiagnostic(text, err.Error())}
	}
	if !strings.Contains(text, "--- PASS: "+functionName) {
		return Execution{Status: ExecutionCannotEvaluate, Performed: true, Reason: "execution_failed", Message: BoundedDiagnostic(text, "go test did not report the selected test as passed")}
	}
	return Execution{Status: ExecutionPass, Performed: true, Reason: "rule_passed"}
}
