//go:build !unix

package check

import "os/exec"

// ProcessGroupStop is false on a non-Unix build: only the direct child is
// killed on cancel or deadline, a descendant can outlive it, and SIGINT or
// SIGTERM to Rulefloor is not forwarded. The capture limit and the bounded
// wait still apply, but closing the pipe is not guaranteed to wake a blocked
// read on every such platform. No release target is non-Unix.
const ProcessGroupStop = false

func isolateProcessGroup(*exec.Cmd) {}

func forwardTermination(*exec.Cmd) func() { return func() {} }
