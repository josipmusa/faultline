//go:build !unix

package runner

import "os"

// exitCode reads the child's exit status. Signals are a Unix idea; elsewhere
// an unknown status is simply a failure.
func exitCode(state *os.ProcessState) int {
	if code := state.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
