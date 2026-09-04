package check

import (
	"context"
	"testing"
)

func TestCompiledGoTestExecutorRecordsOnlyCompileAttempts(t *testing.T) {
	runner := &compiledRunner{}
	executor := NewCompiledGoTestExecutor(runner)
	t.Cleanup(func() { _ = executor.Close() })

	for _, functionName := range []string{"TestOne", "TestTwo"} {
		result := executor.Run(context.Background(), "/repo/pkg/bound_test.go", functionName, "integration")
		if result.Status != ExecutionPass {
			t.Fatalf("%s execution = %+v", functionName, result)
		}
	}

	timings := executor.CompileTimings()
	if len(timings) != 1 {
		t.Fatalf("compile timings=%d, want one cached build attempt", len(timings))
	}
	if timings[0].Directory != "/repo/pkg" || timings[0].Tags != "integration" || !timings[0].Succeeded {
		t.Fatalf("compile timing=%+v", timings[0])
	}
	if timings[0].Duration < 0 {
		t.Fatalf("compile duration=%s", timings[0].Duration)
	}

	timings[0].Tags = "changed"
	if got := executor.CompileTimings()[0].Tags; got != "integration" {
		t.Fatalf("CompileTimings returned mutable internal state: %q", got)
	}
}

func TestCompiledGoTestExecutorRecordsFailedCompileAttempt(t *testing.T) {
	runner := &compiledRunner{err: context.DeadlineExceeded}
	executor := NewCompiledGoTestExecutor(runner)
	t.Cleanup(func() { _ = executor.Close() })

	_ = executor.Run(context.Background(), "/repo/pkg/bound_test.go", "TestOne", "")
	_ = executor.Run(context.Background(), "/repo/pkg/bound_test.go", "TestTwo", "")
	timings := executor.CompileTimings()
	if len(timings) != 1 || timings[0].Succeeded {
		t.Fatalf("failed compile timings=%+v", timings)
	}
}
