package check

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func scanInChunks(text string, markers []string, size int) Capture {
	writer := newCaptureWriter(CaptureLimitBytes, markers)
	for start := 0; start < len(text); start += size {
		writer.add([]byte(text[start:min(start+size, len(text))]))
	}
	return writer.capture(false)
}

func TestMarkerScanAgreesWithWholeTextSearchAtEveryChunkSize(t *testing.T) {
	texts := []string{
		"",
		"=== RUN   TestA\n--- PASS: TestA (0.00s)\nPASS\n",
		"=== RUN   TestAB\n--- PASS: TestAB (0.00s)\nPASS\n",
		"=== RUN   TestA\n    --- SKIP: TestA/sub (0.00s)\n--- FAIL: TestA (0.00s)\nFAIL\n",
		"--- PASS: Test--- PASS: TestA",
		"--- FAIL: TestA\x00\xff\xfe--- SKIP: Tes",
		strings.Repeat("-", 40) + " PASS: TestA",
	}
	for _, name := range []string{"TestA", "TestAB"} {
		markers := goTestMarkers(name)
		for _, text := range texts {
			for size := 1; size <= len(text)+1; size++ {
				capture := scanInChunks(text, markers, size)
				for index, marker := range markers {
					if want := strings.Contains(text, marker); capture.found(index) != want {
						t.Fatalf("name %s chunk %d marker %q in %q: found %v, want %v", name, size, marker, text, capture.found(index), want)
					}
				}
				if capture.Total != int64(len(text)) || string(capture.Output) != text {
					t.Fatalf("chunk %d: capture total %d output %q, want %q", size, capture.Total, capture.Output, text)
				}
			}
		}
	}
}

// The selected name is matched as a substring, so TestA also matches a line
// for TestAB. This pins the rule that existed before bounded capture.
func TestVerdictMatchingKeepsTheSubstringRule(t *testing.T) {
	cases := []struct {
		output string
		status ExecutionStatus
		reason string
	}{
		{"--- PASS: TestAB (0.00s)\n", ExecutionPass, "rule_passed"},
		{"--- FAIL: TestAB (0.00s)\n", ExecutionFail, "execution_failed"},
		{"--- SKIP: TestAB (0.00s)\n", ExecutionCannotEvaluate, "test_skipped"},
		{"--- PASS: TestA/sub (0.00s)\n--- FAIL: TestA (0.00s)\n", ExecutionFail, "execution_failed"},
		{"--- PASS: TestB (0.00s)\n", ExecutionCannotEvaluate, "execution_failed"},
		{"", ExecutionCannotEvaluate, "execution_failed"},
	}
	for _, testCase := range cases {
		result := RunGoTest(context.Background(), goOutputRunner{output: testCase.output}, "/repo/pkg/bound_test.go", "TestA", "")
		if result.Status != testCase.status || result.Reason != testCase.reason {
			t.Fatalf("output %q: result = %+v, want %s %s", testCase.output, result, testCase.status, testCase.reason)
		}
	}
}

type goOutputRunner struct{ output string }

func (r goOutputRunner) Run(context.Context, string, string, ...string) ([]byte, error) {
	return []byte(r.output), nil
}

func TestPassNeedsACompleteStreamAndNoError(t *testing.T) {
	pass := []bool{false, false, true}
	background := context.Background()
	canceled, cancel := context.WithCancel(background)
	cancel()
	cases := []struct {
		name    string
		ctx     context.Context
		capture Capture
		err     error
		status  ExecutionStatus
		reason  string
	}{
		{"complete pass", background, Capture{Found: pass}, nil, ExecutionPass, "rule_passed"},
		{"kept bytes cut, stream complete", background, Capture{Output: []byte("x"), Total: 2, Found: pass}, nil, ExecutionPass, "rule_passed"},
		{"no verdict line", background, Capture{Found: make([]bool, 3)}, nil, ExecutionCannotEvaluate, "execution_failed"},
		{"killed child", background, Capture{Found: pass}, errors.New("signal: killed"), ExecutionCannotEvaluate, "execution_failed"},
		{"start failure", background, Capture{}, exec.ErrNotFound, ExecutionCannotEvaluate, "toolchain_unavailable"},
		{"canceled context", canceled, Capture{Found: pass}, nil, ExecutionCannotEvaluate, "context_canceled"},
	}
	for _, testCase := range cases {
		result := interpretGoTestResult(testCase.ctx, testCase.capture, testCase.err, true)
		if result.Status != testCase.status || result.Reason != testCase.reason {
			t.Fatalf("%s: result = %+v, want %s %s", testCase.name, result, testCase.status, testCase.reason)
		}
	}
}
