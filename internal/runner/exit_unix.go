//go:build unix

package runner

import (
	"os"
	"syscall"
)

// exitCode reads the child's exit status. A process ended by a signal has no
// code of its own, so it gets the shell's convention, 128 plus the signal.
func exitCode(state *os.ProcessState) int {
	if code := state.ExitCode(); code >= 0 {
		return code
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return 1
}
