package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// DefaultGrace is how long an interrupted child gets to exit on its own before
// it is killed, when the caller names no other. It suits a development server,
// which is what the wrapper mostly runs.
const DefaultGrace = 10 * time.Second

// Cmd is a child process to run under Faultline: the command itself, the
// environment that sends its calls through the proxy, and where its streams
// go.
type Cmd struct {
	// Args is the command and its arguments. It is run directly, so there is
	// no shell between the caller and it.
	Args []string
	// Env is the whole child environment, usually from Env.
	Env []string
	// Dir is the working directory. Empty means the caller's own.
	Dir string

	Stdin          io.Reader
	Stdout, Stderr io.Writer

	// Grace is how long an interrupted child gets to exit on its own before it
	// is killed, and how long its output pipes get after that. Zero means
	// DefaultGrace.
	Grace time.Duration

	// OwnGroup starts the child in its own process group on Unix and sends
	// every signal, including the final kill, to that whole group, so a
	// shell's children stop with it when the context is cancelled. It is off
	// by default because a child in its own group is no longer in the
	// terminal's foreground group and cannot read the tty, which is what
	// `faultline run` needs for interactive commands. Turn it on for commands
	// that have no terminal, such as those started by the MCP server. It has
	// no effect on other operating systems.
	OwnGroup bool
}

// A StartError means the command never ran at all: the executable was not
// found, or could not be started. The caller can tell it from a child that
// ran and failed, which has an exit code and a story of its own.
type StartError struct {
	Name string
	Err  error
}

func (e *StartError) Error() string { return "start " + e.Name + ": " + e.Err.Error() }

func (e *StartError) Unwrap() error { return e.Err }

// Run starts the child, wires the streams straight through, and waits for it.
// The returned code is the child's own exit code, or 128 plus the signal
// number when a signal ended it, so a caller can exit with it and be
// indistinguishable from running the command directly.
//
// An interrupt aimed at Faultline is relayed to the child rather than acted
// on here: the child decides how to stop, and Faultline shuts down after it.
// Cancelling ctx interrupts the child too, and kills it if it does not go.
//
// An error means the command never ran, or could not be waited for. A child
// that ran and failed is not an error; it is a non-zero code.
func (c Cmd) Run(ctx context.Context) (int, error) {
	if len(c.Args) == 0 {
		return 1, errors.New("no command to run")
	}
	grace := c.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}

	// The command is the developer's own start command, typed on their own
	// command line; running it verbatim is the whole point of the wrapper.
	cmd := exec.Command(c.Args[0], c.Args[1:]...) //nolint:gosec // G204: running the caller's command is the feature
	cmd.Env = c.Env
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	cmd.Stdout = c.Stdout
	cmd.Stderr = c.Stderr
	// A shell does not pass a signal on to what it is running, so killing the
	// child can leave a grandchild holding the output pipes. Waiting on those
	// would make a cancelled command take as long as the one it was meant to
	// cut short, so they get a deadline of their own once the child is gone.
	cmd.WaitDelay = grace
	if c.OwnGroup {
		startInOwnGroup(cmd)
	}

	if err := cmd.Start(); err != nil {
		return 1, &StartError{Name: c.Args[0], Err: err}
	}

	stop := relay(ctx, target{proc: cmd.Process, group: c.OwnGroup}, grace)
	err := cmd.Wait()
	stop()

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exitErr):
		return exitCode(exitErr.ProcessState), nil
	default:
		return 1, fmt.Errorf("wait for %s: %w", c.Args[0], err)
	}
}

// relay forwards interrupts to the child until the returned function is
// called, which the caller does once the child is reaped.
// target is what relay signals: the child alone, or its whole process group.
type target struct {
	proc  *os.Process
	group bool
}

func (t target) signal(sig os.Signal) error {
	if t.group {
		return signalGroup(t.proc, sig)
	}
	return t.proc.Signal(sig)
}

func (t target) kill() error {
	if t.group {
		return killGroup(t.proc)
	}
	return t.proc.Kill()
}

// lingers reports whether anything the target covers is still running once the
// child itself has been reaped. Only a group can outlive its leader.
func (t target) lingers() bool {
	return t.group && groupLingers(t.proc)
}

// end interrupts the child, gives it grace to go of its own accord, and kills
// what is left. done closing means the child has been reaped.
func end(child target, grace time.Duration, done <-chan struct{}) {
	_ = child.signal(os.Interrupt)

	deadline := time.NewTimer(grace)
	defer deadline.Stop()

	select {
	case <-deadline.C:
	case <-done:
		// Reaping the child does not empty its group. A shell exits the
		// moment it is interrupted while the grandchild it backgrounded
		// ignores the signal and runs on, and ending that grandchild is what
		// OwnGroup is for. Whatever is left keeps the rest of its grace.
		if !child.lingers() {
			return
		}
		<-deadline.C
	}
	_ = child.kill()
}

func relay(ctx context.Context, child target, grace time.Duration) func() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		for {
			select {
			case sig := <-sigs:
				_ = child.signal(sig)
			case <-ctx.Done():
				end(child, grace, done)
				return
			case <-done:
				return
			}
		}
	}()

	var once bool
	return func() {
		if once {
			return
		}
		once = true
		signal.Stop(sigs)
		close(done)
		// Waiting here is what makes the guarantee real: when Run returns
		// after a cancellation, the group it was asked to end is gone.
		<-finished
	}
}
