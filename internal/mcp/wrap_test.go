package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The exit code is the answer: an agent asked to check whether a test suite
// still passes under a fault reads this and nothing else.
func TestStartWrappedReportsTheExitCode(t *testing.T) {
	h := newHarness(t)

	got := out[wrapResult](t, h.call(t, "start_wrapped", map[string]any{
		"command": []any{"sh", "-c", "exit 3"},
	}))

	if got.ExitCode != 3 {
		t.Errorf("exit_code = %d, want 3", got.ExitCode)
	}
	if got.TimedOut {
		t.Error("timed_out = true for a command that exited on its own")
	}
}

// Both streams come back, because a failure says what it was on whichever one
// the runtime happened to choose.
func TestStartWrappedReportsBothStreams(t *testing.T) {
	h := newHarness(t)

	got := out[wrapResult](t, h.call(t, "start_wrapped", map[string]any{
		"command": []any{"sh", "-c", "echo to stdout; echo to stderr >&2"},
	}))

	if !strings.Contains(got.StdoutTail, "to stdout") {
		t.Errorf("stdout_tail = %q, want what the child printed", got.StdoutTail)
	}
	if !strings.Contains(got.StderrTail, "to stderr") {
		t.Errorf("stderr_tail = %q, want what the child printed", got.StderrTail)
	}
}

// The child's environment is the whole point: without it the calls it makes go
// nowhere near Faultline.
func TestStartWrappedSendsTheChildThroughTheProxy(t *testing.T) {
	h := newHarness(t)

	got := out[wrapResult](t, h.call(t, "start_wrapped", map[string]any{
		"command": []any{"sh", "-c", "echo $HTTP_PROXY"},
	}))

	if !strings.Contains(got.StdoutTail, "http://127.0.0.1:9001") {
		t.Errorf("stdout_tail = %q, want the child to have seen HTTP_PROXY", got.StdoutTail)
	}
}

// An agent has no shell to cd with, so the directory is an argument.
func TestStartWrappedRunsWhereItIsTold(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker"), nil, 0o600); err != nil {
		t.Fatalf("writing the marker: %v", err)
	}

	got := out[wrapResult](t, h.call(t, "start_wrapped", map[string]any{
		"command": []any{"ls"},
		"dir":     dir,
	}))

	if !strings.Contains(got.StdoutTail, "marker") {
		t.Errorf("stdout_tail = %q, want the listing of the directory asked for", got.StdoutTail)
	}
}

// A timeout is an answer and not a failure, the same way wait_for_event's is:
// the agent needs the tail to see how far the command got.
func TestStartWrappedTimesOutWithoutFailing(t *testing.T) {
	h := newHarness(t)

	got := out[wrapResult](t, h.call(t, "start_wrapped", map[string]any{
		"command":    []any{"sh", "-c", "echo started; sleep 30"},
		"timeout_ms": 300,
	}))

	if !got.TimedOut {
		t.Error("timed_out = false for a command that outlived its timeout")
	}
	if !strings.Contains(got.StdoutTail, "started") {
		t.Errorf("stdout_tail = %q, want how far the command got before the timeout", got.StdoutTail)
	}
}

// A build that prints thousands of lines must not arrive whole: the agent's
// context is the scarce thing here.
func TestStartWrappedTruncatesLongOutput(t *testing.T) {
	h := newHarness(t)

	got := out[wrapResult](t, h.call(t, "start_wrapped", map[string]any{
		"command": []any{"sh", "-c", "i=0; while [ $i -lt 500 ]; do echo line $i; i=$((i+1)); done"},
	}))

	if !got.StdoutTruncated {
		t.Error("stdout_truncated = false after 500 lines of output")
	}
	if !strings.Contains(got.StdoutTail, "line 499") {
		t.Errorf("stdout_tail does not end at the last line: %q", lastLines(got.StdoutTail))
	}
	if strings.Contains(got.StdoutTail, "line 0\n") {
		t.Error("stdout_tail still carries the first line, so nothing was dropped")
	}
}

// An instance with no forward proxy has nothing to send a command through, and
// saying so beats running it with an environment that does nothing.
func TestStartWrappedRefusesWhenThereIsNoProxy(t *testing.T) {
	h := newHarness(t)
	h.api.ProxiesAt("", false)

	msg := h.callErr(t, "start_wrapped", map[string]any{"command": []any{"true"}})
	if !strings.Contains(msg, "proxy") {
		t.Errorf("error = %q, want it to say there is no proxy to use", msg)
	}
}

func TestStartWrappedRefusesAnEmptyCommand(t *testing.T) {
	h := newHarness(t)

	if msg := h.callErr(t, "start_wrapped", map[string]any{"command": []any{}}); !strings.Contains(msg, "command") {
		t.Errorf("error = %q, want it to name the empty command", msg)
	}
}

func TestStartWrappedRefusesATimeoutItWillNotHonour(t *testing.T) {
	h := newHarness(t)

	msg := h.callErr(t, "start_wrapped", map[string]any{
		"command":    []any{"true"},
		"timeout_ms": 999_999,
	})
	if !strings.Contains(msg, "timeout_ms") {
		t.Errorf("error = %q, want it to name the field and its ceiling", msg)
	}
}

// wrapResult mirrors the tool's output so the test reads the wire and not the
// package's own type.
type wrapResult struct {
	ExitCode        int    `json:"exit_code"`
	TimedOut        bool   `json:"timed_out"`
	DurationMS      int64  `json:"duration_ms"`
	StdoutTail      string `json:"stdout_tail"`
	StderrTail      string `json:"stderr_tail"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
}

func lastLines(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, "\n")
}
