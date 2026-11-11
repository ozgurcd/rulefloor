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
	watch := &terminationWatch{
		received: make(chan os.Signal, 1),
		quit:     make(chan struct{}),
		finished: make(chan struct{}),
		kill:     func() { _ = killProcessGroup(command.Process) },
		raise: func(value os.Signal) {
			signal.Reset(value)
			_ = syscall.Kill(os.Getpid(), value.(syscall.Signal))
		},
	}
	signal.Notify(watch.received, watched...)
	go watch.run()
	return watch.stop
}

// terminationWatch forwards one termination signal: it kills the child's
// group, then raises the signal against Rulefloor itself. Each watch has its
// own channel, so watches of concurrent children do not share state; a signal
// reaches every one of them and each kills its own group.
type terminationWatch struct {
	received chan os.Signal
	quit     chan struct{}
	finished chan struct{}
	kill     func()
	raise    func(os.Signal)
}

func (w *terminationWatch) run() {
	defer close(w.finished)
	select {
	case value := <-w.received:
		w.forward(value)
	case <-w.quit:
	}
}

func (w *terminationWatch) forward(value os.Signal) {
	w.kill()
	w.raise(value)
}

// stop ends the watch and loses no signal. After signal.Stop returns nothing
// more is delivered, so a signal that the ended goroutine did not take is
// still in the channel; it is forwarded here. The group kill is a no-op once
// the child is collected.
func (w *terminationWatch) stop() {
	signal.Stop(w.received)
	close(w.quit)
	<-w.finished
	select {
	case value := <-w.received:
		w.forward(value)
	default:
	}
}
