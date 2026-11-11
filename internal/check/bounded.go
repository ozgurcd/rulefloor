package check

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

// CaptureLimitBytes is the most output Rulefloor keeps from one child process.
// The kept bytes are the start of the child's combined stdout and stderr; they
// feed only the bounded diagnostic. Verdict markers are found by scanning the
// whole stream, so the verdict does not depend on bytes dropped past the limit.
const CaptureLimitBytes = 64 << 10

// OutputDrainWait bounds how long Rulefloor waits, after a child has exited,
// for the end of its output. A descendant that outlives the child can hold the
// pipe open; past this wait the read is abandoned and the capture is incomplete.
const OutputDrainWait = 5 * time.Second

// OutputIncompleteReason is the Execution.Reason reported when a child's output
// was not read to its end and no skip or failure was seen: a pass cannot be
// established from a stream that was cut.
const OutputIncompleteReason = "output_incomplete"

const captureReadBytes = 32 << 10

// Capture is the bounded record of one child's combined output.
type Capture struct {
	// Output holds the first bytes of the stream, at most CaptureLimitBytes
	// when a BoundedRunner produced it. It is raw bytes, not text.
	Output []byte
	// Total counts every byte read from the child, kept or not.
	Total int64
	// Incomplete reports that reading stopped before the end of the stream.
	Incomplete bool
	// Found reports, per requested marker, whether it occurred anywhere in the
	// bytes that were read.
	Found []bool
}

// Truncated reports that the child printed more than Output holds.
func (c Capture) Truncated() bool { return c.Total > int64(len(c.Output)) }

func (c Capture) found(index int) bool { return index < len(c.Found) && c.Found[index] }

// BoundedRunner runs one child with bounded output capture. Markers are
// searched across the whole stream as it is read.
type BoundedRunner interface {
	RunBounded(ctx context.Context, dir, name string, args []string, markers []string) (Capture, error)
}

var _ BoundedRunner = ExecRunner{}

// runBounded uses the runner's bounded path when it has one. Other runners
// (test doubles) return their whole output, which is searched as one piece.
func runBounded(ctx context.Context, runner CommandRunner, dir, name string, args []string, markers []string) (Capture, error) {
	if bounded, ok := runner.(BoundedRunner); ok {
		return bounded.RunBounded(ctx, dir, name, args, markers)
	}
	output, err := runner.Run(ctx, dir, name, args...)
	capture := Capture{Output: output, Total: int64(len(output)), Found: make([]bool, len(markers))}
	for index, marker := range markers {
		capture.Found[index] = bytes.Contains(output, []byte(marker))
	}
	return capture, err
}

// captureWriter keeps the head of a stream and scans all of it for markers.
// It is used by one goroutine at a time.
type captureWriter struct {
	limit   int
	head    []byte
	total   int64
	markers [][]byte
	found   []bool
	// window holds the unscanned tail of the stream: the last keep bytes seen,
	// so a marker split across two reads is still found.
	window []byte
	keep   int
}

func newCaptureWriter(limit int, markers []string) *captureWriter {
	writer := &captureWriter{limit: limit, found: make([]bool, len(markers))}
	for _, marker := range markers {
		writer.markers = append(writer.markers, []byte(marker))
		writer.keep = max(writer.keep, len(marker)-1)
	}
	return writer
}

func (w *captureWriter) add(chunk []byte) {
	w.total += int64(len(chunk))
	if room := w.limit - len(w.head); room > 0 {
		w.head = append(w.head, chunk[:min(room, len(chunk))]...)
	}
	if len(w.markers) == 0 {
		return
	}
	w.window = append(w.window, chunk...)
	for index, marker := range w.markers {
		if !w.found[index] && bytes.Contains(w.window, marker) {
			w.found[index] = true
		}
	}
	if excess := len(w.window) - w.keep; excess > 0 {
		w.window = append(w.window[:0], w.window[excess:]...)
	}
}

func (w *captureWriter) capture(incomplete bool) Capture {
	return Capture{Output: w.head, Total: w.total, Incomplete: incomplete, Found: w.found}
}

// drainInto reads the stream to its end. It returns nil at end of stream and
// the read error otherwise, including the error from a reader closed under it.
func drainInto(writer *captureWriter, reader io.Reader) error {
	buffer := make([]byte, captureReadBytes)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			writer.add(buffer[:count])
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (r ExecRunner) drainWait() time.Duration {
	if r.DrainWait > 0 {
		return r.DrainWait
	}
	return OutputDrainWait
}

// RunBounded starts the child with stdout and stderr on one pipe, keeps at most
// CaptureLimitBytes of it, and reads the rest only to scan it, so a child that
// keeps printing never blocks on a full pipe. Where ProcessGroupStop is true
// the child leads its own process group and the whole group is killed when the
// context ends or Rulefloor receives SIGINT or SIGTERM. No goroutine started
// here outlives the call.
func (r ExecRunner) RunBounded(ctx context.Context, dir, name string, args []string, markers []string) (Capture, error) {
	writer := newCaptureWriter(CaptureLimitBytes, markers)
	reader, pipeWriter, err := os.Pipe()
	if err != nil {
		return writer.capture(false), err
	}
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Stdout = pipeWriter
	command.Stderr = pipeWriter
	command.WaitDelay = r.drainWait()
	isolateProcessGroup(command)
	if err := command.Start(); err != nil {
		_ = pipeWriter.Close()
		_ = reader.Close()
		return writer.capture(false), err
	}
	_ = pipeWriter.Close()
	stopForwarding := forwardTermination(command)
	drained := make(chan error, 1)
	go func() { drained <- drainInto(writer, reader) }()

	waitErr := command.Wait()
	stopForwarding()
	timer := time.NewTimer(r.drainWait())
	defer timer.Stop()
	var readErr error
	select {
	case readErr = <-drained:
	case <-timer.C:
		// A surviving descendant still holds the pipe. Closing the read end
		// wakes the reader; the capture is then marked incomplete.
		_ = reader.Close()
		readErr = <-drained
	}
	_ = reader.Close()
	return writer.capture(readErr != nil), waitErr
}
