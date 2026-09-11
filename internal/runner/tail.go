package runner

import (
	"strings"
	"sync"
)

// Tail keeps the end of a stream and throws the rest away. A caller reporting
// what a child printed wants the end of it: that is where a failure says what
// it was, and the beginning is usually a banner.
//
// It is an io.Writer so it can sit on a child's stdout or stderr directly, and
// it is safe for the two goroutines os/exec writes from.
type Tail struct {
	maxLines int
	maxBytes int

	mu        sync.Mutex
	lines     []string
	partial   string
	truncated bool
}

// NewTail returns a Tail holding at most maxLines lines and maxBytes bytes,
// whichever runs out first. A line longer than maxBytes is cut from its front,
// so one enormous line cannot defeat the limit.
func NewTail(maxLines, maxBytes int) *Tail {
	return &Tail{maxLines: maxLines, maxBytes: maxBytes}
}

// Write never fails and never blocks the child: output the caller will not
// report is output it does not have to keep.
func (t *Tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// The pipe hands over whatever chunks it likes, so a line split across two
	// writes is rejoined here rather than counted twice.
	text := t.partial + string(p)
	t.partial = ""

	for {
		line, rest, found := strings.Cut(text, "\n")
		if !found {
			t.partial = line
			break
		}
		t.lines = append(t.lines, line+"\n")
		text = rest
	}

	t.trim()
	return len(p), nil
}

// String is the tail as it stands, the unterminated last line included: a
// child killed mid-line still printed what is there.
func (t *Tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var b strings.Builder
	for _, line := range t.lines {
		b.WriteString(line)
	}
	b.WriteString(t.partial)
	return b.String()
}

// Truncated reports whether anything was thrown away, so a caller can say the
// output is an end rather than the whole of it.
func (t *Tail) Truncated() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.truncated
}

// trim drops from the front until both limits hold, counting the unterminated
// last line against the byte limit as well: it is part of the answer.
func (t *Tail) trim() {
	for len(t.lines) > t.maxLines {
		t.lines = t.lines[1:]
		t.truncated = true
	}

	size := len(t.partial)
	for _, line := range t.lines {
		size += len(line)
	}
	for size > t.maxBytes && len(t.lines) > 0 {
		size -= len(t.lines[0])
		t.lines = t.lines[1:]
		t.truncated = true
	}
	// One line on its own can still be over the limit, so it is cut from the
	// front, which is the end the caller cares about least.
	if over := size - t.maxBytes; over > 0 {
		if len(t.lines) == 1 {
			t.lines[0] = t.lines[0][over:]
		} else {
			t.partial = t.partial[over:]
		}
		t.truncated = true
	}
}
