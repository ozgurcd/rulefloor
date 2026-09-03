package check

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type compiledRunnerCall struct {
	dir  string
	name string
	args []string
}

type compiledRunner struct {
	calls []compiledRunnerCall
	err   error
}

type compiledResultRunner struct {
	output []byte
	err    error
}

func (r compiledResultRunner) Run(_ context.Context, _, name string, _ ...string) ([]byte, error) {
	if name == "go" {
		return nil, nil
	}
	return r.output, r.err
}

func (r *compiledRunner) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, compiledRunnerCall{dir: dir, name: name, args: append([]string(nil), args...)})
	if name == "go" {
		return nil, r.err
	}
	for i, arg := range args {
		if arg == "-test.run" && i+1 < len(args) {
			return []byte("--- PASS: " + strings.Trim(args[i+1], "^") + "\n"), nil
		}
	}
	return nil, errors.New("missing -test.run")
}

func TestCompiledGoTestExecutorCompilesOnceAndExecutesEachRuleInIsolation(t *testing.T) {
	runner := &compiledRunner{}
	executor := NewCompiledGoTestExecutor(runner)
	t.Cleanup(func() { _ = executor.Close() })
	tags := "safe; touch /tmp/not-executed"

	for _, functionName := range []string{"TestOne", "TestTwo"} {
		result := executor.Run(context.Background(), "/repo/pkg/bound_test.go", functionName, tags)
		if result.Status != ExecutionPass {
			t.Fatalf("%s execution = %+v", functionName, result)
		}
	}

	if len(runner.calls) != 3 {
		t.Fatalf("calls = %d, want one compile and two executions", len(runner.calls))
	}
	wantCompile := []string{"test", "-c", "-o", runner.calls[0].args[3], "-tags", tags, "."}
	if runner.calls[0].name != "go" || !reflect.DeepEqual(runner.calls[0].args, wantCompile) {
		t.Fatalf("compile call = %#v, want go %#v", runner.calls[0], wantCompile)
	}
	for i, functionName := range []string{"TestOne", "TestTwo"} {
		call := runner.calls[i+1]
		want := []string{"-test.count=1", "-test.v", "-test.paniconexit0", "-test.timeout=10m0s", "-test.run", "^" + functionName + "$"}
		if call.name != runner.calls[0].args[3] || !reflect.DeepEqual(call.args, want) {
			t.Fatalf("execution %d = %#v, want binary %q args %#v", i, call, runner.calls[0].args[3], want)
		}
	}
}

func TestCompiledGoTestExecutorSeparatesBuildTagsAndCachesFailures(t *testing.T) {
	runner := &compiledRunner{err: exec.ErrNotFound}
	executor := NewCompiledGoTestExecutor(runner)
	t.Cleanup(func() { _ = executor.Close() })

	for range 2 {
		result := executor.Run(context.Background(), "/repo/pkg/bound_test.go", "TestBound", "integration")
		if result.Reason != "toolchain_unavailable" {
			t.Fatalf("execution = %+v", result)
		}
	}
	if len(runner.calls) != 1 {
		t.Fatalf("failed build calls = %d, want 1", len(runner.calls))
	}

	runner.err = nil
	result := executor.Run(context.Background(), "/repo/pkg/bound_test.go", "TestBound", "other")
	if result.Status != ExecutionPass {
		t.Fatalf("other-tag execution = %+v", result)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls after distinct tag set = %d, want 3", len(runner.calls))
	}
}

func TestCompiledGoTestExecutorPreservesRuntimeClassification(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		err       error
		status    ExecutionStatus
		reason    string
		performed bool
	}{
		{name: "pass", output: "--- PASS: TestBound\n", status: ExecutionPass, reason: "rule_passed", performed: true},
		{name: "skip", output: "--- SKIP: TestBound\n", status: ExecutionCannotEvaluate, reason: "test_skipped", performed: true},
		{name: "fail", output: "--- FAIL: TestBound\n", err: errors.New("exit status 1"), status: ExecutionFail, reason: "execution_failed", performed: true},
		{name: "missing result", status: ExecutionCannotEvaluate, reason: "execution_failed", performed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := NewCompiledGoTestExecutor(compiledResultRunner{output: []byte(test.output), err: test.err})
			t.Cleanup(func() { _ = executor.Close() })
			result := executor.Run(context.Background(), "/repo/pkg/bound_test.go", "TestBound", "")
			if result.Status != test.status || result.Reason != test.reason || result.Performed != test.performed {
				t.Fatalf("execution = %+v, want status=%s reason=%s performed=%t", result, test.status, test.reason, test.performed)
			}
		})
	}
}

func TestCompiledGoTestExecutorUsesFreshProcessPerRule(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.test/isolated\n\ngo 1.27.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `package isolated

import "testing"

var state int

func TestOne(t *testing.T) { state = 1 }

func TestTwo(t *testing.T) {
	if state != 0 {
		t.Fatalf("state leaked between selected rule executions: %d", state)
	}
}
`
	testFile := filepath.Join(repo, "isolated_test.go")
	if err := os.WriteFile(testFile, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	executor := NewCompiledGoTestExecutor(ExecRunner{})
	t.Cleanup(func() { _ = executor.Close() })
	for _, functionName := range []string{"TestOne", "TestTwo"} {
		result := executor.Run(context.Background(), testFile, functionName, "")
		if result.Status != ExecutionPass {
			t.Fatalf("%s execution = %+v", functionName, result)
		}
	}
}

func TestCompiledGoTestExecutorRemovesItsTemporaryDirectory(t *testing.T) {
	runner := &compiledRunner{}
	executor := NewCompiledGoTestExecutor(runner)
	result := executor.Run(context.Background(), "/repo/pkg/bound_test.go", "TestBound", "")
	if result.Status != ExecutionPass {
		t.Fatalf("execution = %+v", result)
	}
	tempDir := filepath.Dir(runner.calls[0].args[3])
	if err := executor.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tempDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary directory still exists or cannot be checked: %v", err)
	}
}
