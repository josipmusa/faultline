package capture

import (
	"bytes"
	"io"
	"sync"
)

// Tee wraps a body and keeps the first MaxBodyBytes of what is read through
// it. The reader downstream sees the body unchanged: capturing must never
// alter the traffic it is there to explain.
//
// It is safe for concurrent use because the proxy flushes a response body from
// a goroutine of its own while the transport that owns the capture reads it
// back on close.
type Tee struct {
	rc io.ReadCloser

	mu        sync.Mutex
	kept      bytes.Buffer
	truncated bool
}

// NewTee wraps rc. A nil body reads as empty and captures nothing, which is
// what a GET with no request body looks like here.
func NewTee(rc io.ReadCloser) *Tee { return &Tee{rc: rc} }

// Read passes a read through to the wrapped body, keeping a copy of what it
// returned until the cap is reached.
func (t *Tee) Read(p []byte) (int, error) {
	if t.rc == nil {
		return 0, io.EOF
	}
	n, err := t.rc.Read(p)
	if n > 0 {
		t.keep(p[:n])
	}
	return n, err
}

// Close closes the wrapped body. What was captured stays readable after it.
func (t *Tee) Close() error {
	if t.rc == nil {
		return nil
	}
	return t.rc.Close()
}

// Captured is what was read so far, up to the cap, and whether there was more
// of it than that.
func (t *Tee) Captured() ([]byte, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.kept.Len() == 0 {
		return nil, t.truncated
	}
	return bytes.Clone(t.kept.Bytes()), t.truncated
}

// keep files as much of a read as the cap still has room for.
func (t *Tee) keep(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	room := MaxBodyBytes - t.kept.Len()
	if len(p) > room {
		p, t.truncated = p[:room], true
	}
	t.kept.Write(p)
}
