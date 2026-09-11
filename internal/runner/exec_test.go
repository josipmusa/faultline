package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunForwardsOutputAndReportsSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code, err := Cmd{Args: []string{"sh", "-c", "echo out; echo err >&2"}, Stdout: &stdout, Stderr: &stderr}.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if got := stdout.String(); got != "out\n" {
		t.Errorf("stdout = %q, want %q", got, "out\n")
	}
	if got := stderr.String(); got != "err\n" {
		t.Errorf("stderr = %q, want %q", got, "err\n")
	}
}

func TestRunMirrorsAFailingExitCode(t *testing.T) {
	code, err := Cmd{Args: []string{"sh", "-c", "exit 3"}}.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
}

func TestRunGivesTheChildTheEnvironment(t *testing.T) {
	var stdout bytes.Buffer
	env := Env(nil, "http://127.0.0.1:9001", []string{"localhost"}, "", "")

	code, err := Cmd{Args: []string{"sh", "-c", "echo $HTTP_PROXY $NO_PROXY"}, Env: env, Stdout: &stdout}.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(stdout.String()); got != "http://127.0.0.1:9001 localhost" {
		t.Errorf("child saw %q, want the proxy and bypass list", got)
	}
}

func TestRunReadsStdin(t *testing.T) {
	var stdout bytes.Buffer

	code, err := Cmd{Args: []string{"sh", "-c", "cat"}, Stdin: strings.NewReader("hello"), Stdout: &stdout}.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := stdout.String(); got != "hello" {
		t.Errorf("stdout = %q, want %q", got, "hello")
	}
}

func TestRunReportsACommandItCannotStart(t *testing.T) {
	_, err := Cmd{Args: []string{"faultline-no-such-command"}}.Run(context.Background())
	if err == nil {
		t.Fatal("Run accepted a command that does not exist")
	}
	if !strings.Contains(err.Error(), "faultline-no-such-command") {
		t.Errorf("error %q does not name the command", err)
	}
}

func TestRunInterruptsTheChildWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// sleep is run directly rather than through `sh -c`, so the process the
	// interrupt is aimed at is the one that has to act on it. A shell in
	// between makes the test measure the shell: bash execs a lone command and
	// so dies of the interrupt itself, while dash forks and waits, leaving
	// nothing for the interrupt to end. Run only ever signals its own child,
	// which is what this asserts.
	start := time.Now()
	code, err := Cmd{Args: []string{"sleep", "30"}}.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Comfortably inside the grace period, so being killed at the end of it
	// fails here rather than passing as an interrupt that worked.
	if elapsed := time.Since(start); elapsed > DefaultGrace/2 {
		t.Fatalf("the child ran for %s; it should have been interrupted well inside the %s grace period", elapsed, DefaultGrace)
	}
	if code == 0 {
		t.Errorf("exit code = 0, want the interrupted child's non-zero code")
	}
}

// An agent asking for a test suite in a subdirectory has no shell to cd with,
// so the directory is the runner's to set.
func TestRunUsesTheDirectoryItIsGiven(t *testing.T) {
	dir := t.TempDir()
	var stdout strings.Builder

	code, err := Cmd{Args: []string{"sh", "-c", "pwd"}, Dir: dir, Stdout: &stdout}.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	// macOS resolves the temporary directory through a symlink, so the child's
	// own answer is compared with the child's own resolution of it.
	var want strings.Builder
	resolve := Cmd{Args: []string{"sh", "-c", "cd " + dir + " && pwd"}, Stdout: &want}
	if _, err := resolve.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != strings.TrimSpace(want.String()) {
		t.Errorf("the child ran in %q, want %q", strings.TrimSpace(stdout.String()), strings.TrimSpace(want.String()))
	}
}

// A shell does not pass a signal on to the command it is running, so killing
// the child can leave a grandchild holding the output pipes open. Waiting on
// those would make a cancelled command take as long as the command it was
// meant to cut short, so the pipes are given a deadline of their own.
func TestRunDoesNotWaitForAnOrphanHoldingThePipes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = Cmd{
			Args:   []string{"sh", "-c", "sleep 30"},
			Stdout: &stdout,
			Grace:  50 * time.Millisecond,
		}.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run is still waiting on a grandchild's pipes ten seconds after being cancelled")
	}
}
