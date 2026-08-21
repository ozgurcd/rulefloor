package check

import (
	"context"
	"reflect"
	"testing"
)

type recordingRunner struct {
	dir  string
	name string
	args []string
}

func (r *recordingRunner) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	r.dir = dir
	r.name = name
	r.args = append([]string(nil), args...)
	return []byte("--- PASS: TestBound\n"), nil
}

func TestGoExecutionUsesArgumentVectorWithoutShellExpansion(t *testing.T) {
	runner := &recordingRunner{}
	tags := "safe; touch /tmp/not-executed"
	result := RunGoTest(context.Background(), runner, "/repo/pkg/bound_test.go", "TestBound", tags)
	if result.Status != ExecutionPass {
		t.Fatalf("execution = %+v", result)
	}
	if runner.name != "go" {
		t.Fatalf("command = %q", runner.name)
	}
	want := []string{"test", "-count=1", "-v", "-run", "^TestBound$", "-tags", tags, "."}
	if !reflect.DeepEqual(runner.args, want) {
		t.Fatalf("arguments = %#v, want %#v", runner.args, want)
	}
}
