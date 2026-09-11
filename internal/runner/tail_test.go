package runner

import (
	"fmt"
	"strings"
	"testing"
)

// A tail is what a caller reports when the whole of a child's output is more
// than the answer needs: the end, which is where a failure says what it was.
func TestTailKeepsTheEnd(t *testing.T) {
	tail := NewTail(3, 1024)

	for i := range 10 {
		_, _ = fmt.Fprintf(tail, "line %d\n", i)
	}

	want := "line 7\nline 8\nline 9\n"
	if got := tail.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if !tail.Truncated() {
		t.Error("Truncated() = false after dropping seven lines")
	}
}

func TestTailKeepsEverythingThatFits(t *testing.T) {
	tail := NewTail(10, 1024)

	_, _ = fmt.Fprint(tail, "one\ntwo\n")

	if got := tail.String(); got != "one\ntwo\n" {
		t.Errorf("String() = %q, want the whole of it", got)
	}
	if tail.Truncated() {
		t.Error("Truncated() = true while nothing was dropped")
	}
}

// A child that writes one enormous line would otherwise defeat a line limit,
// so bytes bound it too.
func TestTailBoundsBytesAsWellAsLines(t *testing.T) {
	tail := NewTail(100, 16)

	_, _ = fmt.Fprint(tail, strings.Repeat("x", 100))

	if got := len(tail.String()); got > 16 {
		t.Errorf("String() is %d bytes, want 16 at most", got)
	}
	if !tail.Truncated() {
		t.Error("Truncated() = false after dropping most of a line")
	}
}

// Output arrives in whatever chunks the pipe hands over, so a line split
// across two writes is still one line.
func TestTailJoinsWritesThatSplitALine(t *testing.T) {
	tail := NewTail(2, 1024)

	_, _ = fmt.Fprint(tail, "he")
	_, _ = fmt.Fprint(tail, "llo\nworld")

	if got := tail.String(); got != "hello\nworld" {
		t.Errorf("String() = %q, want the two lines rejoined", got)
	}
}
