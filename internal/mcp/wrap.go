package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/runner"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

// defaultWrapMS and maxWrapMS bound how long a wrapped command may run. A
// command that never exits, a development server most of all, would otherwise
// hold the agent's session open until something else gave up.
const (
	defaultWrapMS = 60_000
	maxWrapMS     = 300_000
)

// tailLines and tailBytes bound what comes back from each stream. The agent's
// context is the scarce thing here, and a failure says what it was at the end
// of the output rather than the beginning.
const (
	tailLines = 100
	tailBytes = 8 << 10
)

// wrapGrace is how long a command that outlived its timeout gets to exit on
// its own before it is killed. Shorter than the wrapper's own grace period,
// which is sized for a development server being shut down by hand.
const wrapGrace = 2 * time.Second

type startWrappedInput struct {
	Command   []string `json:"command" jsonschema:"the command and its arguments, as a list; it is run directly, so there is no shell and no pipes or redirection"`
	Dir       string   `json:"dir,omitempty" jsonschema:"the working directory to run it in; the directory Faultline was started in by default"`
	TimeoutMS int      `json:"timeout_ms,omitempty" jsonschema:"how long to let it run in milliseconds, 60000 by default and 300000 at most"`
}

// wrapResult is what the command did. A timeout is one of its answers rather
// than an error, the same way wait_for_event's is: the tail still says how far
// the command got.
type wrapResult struct {
	ExitCode        int    `json:"exit_code" jsonschema:"the command's own exit code, or 128 plus the signal number when a signal ended it"`
	TimedOut        bool   `json:"timed_out" jsonschema:"whether the command outlived its timeout and was stopped"`
	DurationMS      int64  `json:"duration_ms" jsonschema:"how long the command ran in milliseconds"`
	StdoutTail      string `json:"stdout_tail" jsonschema:"the end of what the command wrote to stdout"`
	StderrTail      string `json:"stderr_tail" jsonschema:"the end of what the command wrote to stderr"`
	StdoutTruncated bool   `json:"stdout_truncated" jsonschema:"whether stdout was longer than the tail reported"`
	StderrTruncated bool   `json:"stderr_truncated" jsonschema:"whether stderr was longer than the tail reported"`
}

// addWrapTools registers start_wrapped. mirror is where the child's output is
// echoed for a human watching the terminal, and may be nil; it is never stdout,
// which in a stdio session is the protocol itself.
func addWrapTools(s *sdk.Server, c *client.Client, mirror io.Writer) {
	sdk.AddTool(s, &sdk.Tool{
		Name: "start_wrapped",
		Description: "Run a command with its outbound HTTP and HTTPS calls going through Faultline, " +
			"and report how it went. This is `faultline run` as a tool: the command is started with the " +
			"proxy variables set and, when Faultline is intercepting HTTPS, the variables that point Go, " +
			"Python, curl, Node, git and a JVM at its CA.\n\n" +
			"Use it to exercise the application under the rules you have added: add_rule to break a " +
			"dependency, start_wrapped to run the test suite or the command that calls it, then " +
			"get_report and get_events to see what it actually did.\n\n" +
			"The command runs to completion or until its timeout, so it suits a test suite or a one-shot " +
			"script rather than a development server that never exits. A timeout is an answer and not a " +
			"failure: timed_out comes back true with the output so far, and nothing is left running.\n\n" +
			"There is no shell, so the command is a list and a pipe or a redirection in it is not " +
			"interpreted. Wrap it in `sh -c` yourself if you need one.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in startWrappedInput) (*sdk.CallToolResult, wrapResult, error) {
		res, err := startWrapped(ctx, c, mirror, in)
		return nil, res, err
	})
}

func startWrapped(ctx context.Context, c *client.Client, mirror io.Writer, in startWrappedInput) (wrapResult, error) {
	if len(in.Command) == 0 {
		return wrapResult{}, errors.New("command is empty; give the command and its arguments as a list")
	}
	timeout, err := in.timeout()
	if err != nil {
		return wrapResult{}, err
	}

	env, err := childEnv(ctx, c, mirror)
	if err != nil {
		return wrapResult{}, err
	}

	stdout := runner.NewTail(tailLines, tailBytes)
	stderr := runner.NewTail(tailLines, tailBytes)

	// The child's output is kept and, for a human watching, echoed to stderr.
	// Never to stdout: in a stdio session that is the protocol.
	var out, errOut io.Writer = stdout, stderr
	if mirror != nil {
		out, errOut = io.MultiWriter(stdout, mirror), io.MultiWriter(stderr, mirror)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started := time.Now()
	child := runner.Cmd{
		Args: in.Command, Env: env, Dir: in.Dir,
		Stdout: out, Stderr: errOut,
		// An agent that named a timeout wants the command stopped at it, not
		// long after, and a test suite or a script needs no time to wind down.
		Grace: wrapGrace,
	}
	code, runErr := child.Run(runCtx)
	elapsed := time.Since(started)

	// A command that never started is the agent's mistake to fix - a typo in
	// the name, a directory that is not there - so it is an error and not a
	// result. One that ran and was stopped by its timeout is a result.
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	if runErr != nil && !timedOut {
		return wrapResult{}, runErr
	}

	return wrapResult{
		ExitCode:        code,
		TimedOut:        timedOut,
		DurationMS:      elapsed.Milliseconds(),
		StdoutTail:      stdout.String(),
		StderrTail:      stderr.String(),
		StdoutTruncated: stdout.Truncated(),
		StderrTruncated: stderr.Truncated(),
	}, nil
}

// childEnv builds the environment that sends a child's traffic through the
// instance this server is talking to. It asks that instance rather than
// assuming, so a Faultline on another port, or one passing HTTPS through
// untouched, is described correctly instead of guessed at.
func childEnv(ctx context.Context, c *client.Client, notice io.Writer) ([]string, error) {
	cfg, err := c.Config(ctx)
	if err != nil {
		return nil, fmt.Errorf("asking faultline how to reach it: %w", err)
	}
	if cfg.ProxyURL == "" {
		return nil, errors.New("this faultline is not running a forward proxy, so there is nothing to " +
			"send a command through")
	}

	caPath, javaStore := "", ""
	if cfg.Intercepting {
		if caPath, err = caCertPath(); err != nil {
			return nil, err
		}
		javaStore = runner.JavaTrustStoreOrNone(caPath, notice)
	}

	return runner.Env(os.Environ(), cfg.ProxyURL, cfg.NoProxy, caPath, javaStore), nil
}

// caCertPath finds the CA the instance is intercepting with. Handing a child a
// bundle that is not the one the proxy signs with would fail every HTTPS call
// it makes for a reason invisible from the failure, so a CA that cannot be read
// is refused here rather than discovered there.
func caCertPath() (string, error) {
	dir, err := tlsmitm.DefaultDir()
	if err != nil {
		return "", err
	}
	ca, err := tlsmitm.Load(dir)
	if err != nil {
		return "", fmt.Errorf("faultline is intercepting HTTPS but its CA could not be read from %s, "+
			"so a command started here would not trust it: %w", dir, err)
	}
	return ca.CertPath, nil
}

func (in startWrappedInput) timeout() (time.Duration, error) {
	ms := in.TimeoutMS
	switch {
	case ms == 0:
		ms = defaultWrapMS
	case ms < 0:
		return 0, fmt.Errorf("timeout_ms must be positive, not %d", ms)
	case ms > maxWrapMS:
		return 0, fmt.Errorf("timeout_ms must be %d or less, not %d; run the command in smaller pieces "+
			"if it needs longer", maxWrapMS, ms)
	}
	return time.Duration(ms) * time.Millisecond, nil
}
