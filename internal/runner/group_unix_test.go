//go:build unix

package runner

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A shell does not pass signals on to what it started, so cancelling a
// wrapped `sh -c "npm test"` ends the shell and leaves npm running with the
// proxy environment. OwnGroup puts the child in its own process group and
// signals the group, so the grandchild goes too.
func TestRunWithOwnGroupEndsTheGrandchildOnCancel(t *testing.T) {
	pid, _ := runShellWithBackgroundSleep(t, true)

	if !waitForProcessToVanish(pid, 5*time.Second) {
		t.Fatalf("grandchild %d is still running five seconds after the context was cancelled", pid)
	}
}

// The mirror: without OwnGroup only the shell is signalled, which is the
// behavior `faultline run` relies on for interactive commands and the reason
// the group behavior is opt-in.
func TestRunWithoutOwnGroupLeavesTheGrandchildRunning(t *testing.T) {
	pid, _ := runShellWithBackgroundSleep(t, false)

	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("grandchild %d is gone (%v); Run without OwnGroup should have signalled only its own child", pid, err)
	}
}

// runShellWithBackgroundSleep starts a shell that forks a long sleep, prints
// the sleep's pid and waits, then cancels the run once the pid is known and
// returns that pid after Run has come back. Any surviving sleep is killed
// when the test ends.
func runShellWithBackgroundSleep(t *testing.T, ownGroup bool) (int, int) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pr, pw := io.Pipe()
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := Cmd{
			Args:     []string{"sh", "-c", "sleep 30 & echo $!; wait"},
			Stdout:   pw,
			Grace:    200 * time.Millisecond,
			OwnGroup: ownGroup,
		}.Run(ctx)
		_ = pw.Close()
		done <- result{code, err}
	}()

	line, err := bufio.NewReader(pr).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the grandchild pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("the shell printed %q, want the grandchild pid", line)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	cancel()
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("Run: %v", res.err)
		}
		return pid, res.code
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return within ten seconds of the context being cancelled")
		return 0, 0
	}
}

func waitForProcessToVanish(pid int, within time.Duration) bool {
	deadline := time.NewTimer(within)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()

	for {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		select {
		case <-deadline.C:
			return false
		case <-tick.C:
		}
	}
}
