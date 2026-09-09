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

// gracePeriod is how long an interrupted child gets to exit on its own before
// it is killed.
const gracePeriod = 10 * time.Second

// Run starts args as a child process with env, wires the streams straight
// through, and waits for it. The returned code is the child's own exit code,
// or 128 plus the signal number when a signal ended it, so a caller can exit
// with it and be indistinguishable from running the command directly.
//
// An interrupt aimed at Faultline is relayed to the child rather than acted
// on here: the child decides how to stop, and Faultline shuts down after it.
// Cancelling ctx interrupts the child too, and kills it if it does not go.
//
// An error means the command never ran, or could not be waited for. A child
// that ran and failed is not an error; it is a non-zero code.
func Run(ctx context.Context, args, env []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 1, errors.New("no command to run")
	}

	// The command is the developer's own start command, typed on their own
	// command line; running it verbatim is the whole point of the wrapper.
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec // G204: running the caller's command is the feature
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start %s: %w", args[0], err)
	}

	stop := relay(ctx, cmd.Process)
	err := cmd.Wait()
	stop()

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exitErr):
		return exitCode(exitErr.ProcessState), nil
	default:
		return 1, fmt.Errorf("wait for %s: %w", args[0], err)
	}
}

// relay forwards interrupts to the child until the returned function is
// called, which the caller does once the child is reaped.
func relay(ctx context.Context, child *os.Process) func() {
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
				case <-time.After(gracePeriod):
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
