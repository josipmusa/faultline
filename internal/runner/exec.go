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
}

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

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start %s: %w", c.Args[0], err)
	}

	stop := relay(ctx, cmd.Process, grace)
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
func relay(ctx context.Context, child *os.Process, grace time.Duration) func() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case sig := <-sigs:
				_ = child.Signal(sig)
			case <-ctx.Done():
				_ = child.Signal(os.Interrupt)
				select {
				case <-time.After(grace):
					_ = child.Kill()
				case <-done:
				}
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
	}
}
