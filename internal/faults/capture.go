package faults

import (
	"io"
	"net/http"
	"sync"

	"github.com/josipmusa/faultline/internal/capture"
)

// CaptureTo files the headers and bodies of every exchange this transport
// handles into store, so a human can open an event and read what went over
// the wire. A transport with no store captures nothing, which is the default:
// the pipeline works the same either way.
func (t *Transport) CaptureTo(store *capture.Store) { t.captures = store }

// teeRequest routes the request body through a tee, so what the pipeline
// sends upstream is also what gets captured. It returns the request to use
// from here on, which is a shallow copy when there was a body to wrap: a
// RoundTripper has no business rewriting the request it was handed.
//
// A request with no body is left alone. Handing http.Transport a non-nil body
// that reads as empty is not the same thing as handing it none.
func (t *Transport) teeRequest(req *http.Request) (*http.Request, *capture.Tee) {
	if t.captures == nil || req.Body == nil || req.Body == http.NoBody {
		return req, nil
	}
	tee := capture.NewTee(req.Body)
	sent := *req
	sent.Body = tee
	return &sent, tee
}

// file writes the exchange into the capture store and routes the response
// body through a tee that completes the entry once the body has been
// delivered. The response is what the client receives, faults included, so a
// rewritten or cut body shows what the client actually got rather than what
// the upstream sent.
func (t *Transport) file(id string, req *http.Request, sent *capture.Tee, resp *http.Response) {
	if t.captures == nil {
		return
	}

	c := capture.Capture{EventID: id, Request: capture.Side{Headers: req.Header.Clone()}}
	if sent != nil {
		c.Request.Body, c.Request.Truncated = sent.Captured()
	}
	if resp != nil {
		c.Response.Headers = resp.Header.Clone()
	}
	t.captures.Put(c)

	if resp == nil || resp.Body == nil {
		return
	}
	tee := capture.NewTee(resp.Body)
	resp.Body = &completing{Tee: tee, store: t.captures, id: id}
}

// completing is a captured response body that files what it captured once the
// body is closed, which is the only moment the whole of it is known.
type completing struct {
	*capture.Tee

	store *capture.Store
	id    string
	once  sync.Once
}

func (c *completing) Close() error {
	c.once.Do(func() {
		body, truncated := c.Captured()
		c.store.Complete(c.id, body, truncated)
	})
	return c.Tee.Close()
}

// compile-time proof that a completing body is still a body.
var _ io.ReadCloser = (*completing)(nil)
