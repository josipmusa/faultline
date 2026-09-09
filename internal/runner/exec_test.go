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

	code, err := Run(context.Background(), []string{"sh", "-c", "echo out; echo err >&2"}, nil, nil, &stdout, &stderr)
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
	code, err := Run(context.Background(), []string{"sh", "-c", "exit 3"}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
}

func TestRunGivesTheChildTheEnvironment(t *testing.T) {
	var stdout bytes.Buffer
	env := Env(nil, "http://127.0.0.1:9001", []string{"localhost"}, "")

	code, err := Run(context.Background(), []string{"sh", "-c", "echo $HTTP_PROXY $NO_PROXY"}, env, nil, &stdout, nil)
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

	code, err := Run(context.Background(), []string{"sh", "-c", "cat"}, nil, strings.NewReader("hello"), &stdout, nil)
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
	_, err := Run(context.Background(), []string{"faultline-no-such-command"}, nil, nil, nil, nil)
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

	start := time.Now()
	code, err := Run(ctx, []string{"sh", "-c", "sleep 30"}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("the child ran for %s; it should have been interrupted", elapsed)
	}
	if code == 0 {
		t.Errorf("exit code = 0, want the interrupted child's non-zero code")
	}
}
