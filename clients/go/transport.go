package client

import (
	"bytes"
	"io"
	"net/http"
)

// An Option adjusts a Client as it is built.
type Option func(*Client)

// WithTransport sends this client's requests through rt instead of the network.
// It is how a client reaches an admin server in its own process, which is what
// the MCP interface on the admin port does: it is a client of the API like the
// CLI and the UI, and dialing its own socket to be one would be a worse way to
// say so.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *Client) { c.http.Transport = rt }
}

// HandlerTransport answers requests by calling h directly, in memory. Nothing
// is serialised onto a connection and nothing is listening: the address a
// client is built with only names the requests in errors.
//
// The response is buffered whole before it is returned, so this is for API
// calls and not for the event stream, which never ends. A streaming endpoint
// needs a real connection.
func HandlerTransport(h http.Handler) http.RoundTripper {
	return handlerTransport{h: h}
}

type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// The handler may read the body and the caller may not assume it is left
	// open, so closing it here matches what a real transport does.
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
	}

	w := &bufferedWriter{header: http.Header{}, status: http.StatusOK}
	t.h.ServeHTTP(w, r.WithContext(r.Context()))

	body := w.body.Bytes()
	resp := &http.Response{
		Status:        http.StatusText(w.status),
		StatusCode:    w.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        w.header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       r,
	}
	return resp, nil
}

// bufferedWriter is the http.ResponseWriter a handler writes into when there is
// no connection under it. It keeps only what a JSON API answer needs: the
// header, the status, and the body.
type bufferedWriter struct {
	header  http.Header
	status  int
	written bool
	body    bytes.Buffer
}

func (w *bufferedWriter) Header() http.Header { return w.header }

func (w *bufferedWriter) WriteHeader(status int) {
	if w.written {
		return // a second WriteHeader is the handler's bug, and net/http ignores it too
	}
	w.written = true
	w.status = status
}

func (w *bufferedWriter) Write(p []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(p)
}

// Flush is what a handler streaming server-sent events calls. There is nothing
// to flush to, and a handler that checks for http.Flusher should still find
// one, so this is deliberately a no-op rather than absent.
func (w *bufferedWriter) Flush() {}
