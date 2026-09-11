package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
)

// An agent that closes the session, or a Ctrl-C, is how a stdio server normally
// ends. Reporting either as a failure makes `faultline mcp` look broken every
// time it is used correctly.
func TestEndedCleanlyAcceptsTheNormalEndings(t *testing.T) {
	for _, err := range []error{
		nil,
		io.EOF,
		fmt.Errorf("server is closing: %w", io.EOF),
		context.Canceled,
		fmt.Errorf("reading next message: %w", context.Canceled),
	} {
		if !endedCleanly(err) {
			t.Errorf("endedCleanly(%v) = false, want true", err)
		}
	}
}

func TestEndedCleanlyKeepsARealFailure(t *testing.T) {
	for _, err := range []error{
		errors.New("write /dev/stdout: broken pipe"),
		context.DeadlineExceeded,
	} {
		if endedCleanly(err) {
			t.Errorf("endedCleanly(%v) = true, want false", err)
		}
	}
}
