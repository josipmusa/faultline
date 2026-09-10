package capture

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// closeCounter is a body that says whether it was closed.
type closeCounter struct {
	io.Reader
	closed bool
	err    error
}

func (c *closeCounter) Close() error { c.closed = true; return c.err }

func TestTeePassesTheBodyThroughUntouched(t *testing.T) {
	src := &closeCounter{Reader: strings.NewReader("the whole body")}
	tee := NewTee(src)

	got, err := io.ReadAll(tee)
	if err != nil {
		t.Fatalf("reading through the tee: %v", err)
	}
	if string(got) != "the whole body" {
		t.Errorf("read %q, want %q", got, "the whole body")
	}

	body, truncated := tee.Captured()
	if string(body) != "the whole body" {
		t.Errorf("captured %q, want %q", body, "the whole body")
	}
	if truncated {
		t.Error("captured truncated = true, want false for a body under the cap")
	}
}

func TestTeeKeepsOnlyTheCapAndSaysSo(t *testing.T) {
	src := &closeCounter{Reader: strings.NewReader(strings.Repeat("x", MaxBodyBytes+10))}
	tee := NewTee(src)

	got, err := io.ReadAll(tee)
	if err != nil {
		t.Fatalf("reading through the tee: %v", err)
	}
	// The reader downstream still gets everything; only the capture is cut.
	if len(got) != MaxBodyBytes+10 {
		t.Errorf("read %d bytes, want %d: the tee must not shorten the body", len(got), MaxBodyBytes+10)
	}

	body, truncated := tee.Captured()
	if len(body) != MaxBodyBytes {
		t.Errorf("captured %d bytes, want the cap of %d", len(body), MaxBodyBytes)
	}
	if !truncated {
		t.Error("captured truncated = false, want true for a body over the cap")
	}
}

func TestTeeClosesWhatItWraps(t *testing.T) {
	src := &closeCounter{Reader: strings.NewReader("body"), err: errors.New("close failed")}
	tee := NewTee(src)

	if err := tee.Close(); err == nil || !src.closed {
		t.Errorf("Close() err = %v, closed = %v; want the wrapped body closed and its error returned", err, src.closed)
	}
}

func TestTeeOfNothingCapturesNothing(t *testing.T) {
	tee := NewTee(nil)
	if body, truncated := tee.Captured(); body != nil || truncated {
		t.Errorf("captured (%q, %v) from a nil body, want (nil, false)", body, truncated)
	}
	if _, err := tee.Read(make([]byte, 4)); !errors.Is(err, io.EOF) {
		t.Errorf("Read on a nil body err = %v, want io.EOF", err)
	}
	if err := tee.Close(); err != nil {
		t.Errorf("Close on a nil body err = %v, want nil", err)
	}
}
