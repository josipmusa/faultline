//go:build unix

package runner

import (
	"os"
	"os/exec"
	"syscall"
)

// startInOwnGroup makes the child the leader of a new process group, so that
// a signal sent to the group reaches every process it forks as well.
func startInOwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup delivers sig to the whole process group led by proc.
func signalGroup(proc *os.Process, sig os.Signal) error {
	s, ok := sig.(syscall.Signal)
	if !ok {
		return proc.Signal(sig)
	}
	return syscall.Kill(-proc.Pid, s)
}

// killGroup ends every process in the group led by proc without recourse.
func killGroup(proc *os.Process) error {
	return syscall.Kill(-proc.Pid, syscall.SIGKILL)
}

// groupLingers reports whether the group led by proc still has a member, which
// after the leader has been reaped means a grandchild outlived it. Signal 0
// delivers nothing; it only asks whether there is anything to deliver to.
func groupLingers(proc *os.Process) bool {
	return syscall.Kill(-proc.Pid, 0) == nil
}
