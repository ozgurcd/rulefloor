//go:build unix

package check

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// ProcessGroupStop reports whether this build stops a child's whole process
// group on cancel, deadline, SIGINT and SIGTERM.
const ProcessGroupStop = true

// isolateProcessGroup makes the child the leader of a new process group and
// kills that whole group when the command's context ends. A descendant that
// moves itself to another group or session is out of reach.
func isolateProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return killProcessGroup(command.Process) }
}

// killProcessGroup kills the group led by process. It signals the group only
// while the leader is not yet collected: after that the group ID may belong to
// an unrelated process.
func killProcessGroup(process *os.Process) error {
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return err
	}
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err != nil {
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}

// forwardTermination kills the child's group when Rulefloor itself receives
// SIGINT or SIGTERM, then lets the signal end Rulefloor as it did before the
// child had its own group. A signal the process was started ignoring stays
// ignored. The returned function stops the watch and waits for its goroutine.
func forwardTermination(command *exec.Cmd) func() {
	var watched []os.Signal
	for _, candidate := range []os.Signal{syscall.SIGINT, syscall.SIGTERM} {
		if !signal.Ignored(candidate) {
			watched = append(watched, candidate)
		}
	}
	if len(watched) == 0 {
		return func() {}
	}
	received := make(chan os.Signal, 1)
	signal.Notify(received, watched...)
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case value := <-received:
			_ = killProcessGroup(command.Process)
			signal.Reset(value)
			_ = syscall.Kill(os.Getpid(), value.(syscall.Signal))
		case <-stop:
		}
	}()
	return func() {
		close(stop)
		<-finished
		signal.Stop(received)
	}
}
