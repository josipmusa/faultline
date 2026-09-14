//go:build !unix

package runner

import (
	"os"
	"os/exec"
)

// Process groups are a Unix notion; elsewhere OwnGroup changes nothing and
// signals go to the child itself.

func startInOwnGroup(*exec.Cmd) {}

func signalGroup(proc *os.Process, sig os.Signal) error { return proc.Signal(sig) }

func killGroup(proc *os.Process) error { return proc.Kill() }

func groupLingers(*os.Process) bool { return false }
