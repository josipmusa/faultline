package faults

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

// throttle is the fault that delivers a real answer at a crawl, the way a
// saturated link or a struggling upstream does. Nothing is changed but the
// speed: every byte still arrives. It needs nothing but a connection, so it
// applies to encrypted traffic too.
type throttle struct{}

func init() { Register(throttle{}) }

func (throttle) Name() string { return "throttle" }

func (throttle) Tier() Tier { return TierConnection }

func (throttle) Schema() Schema {
	return Schema{
		Int("bytes_per_sec").Required().Min(1).Desc("How fast the response is allowed to arrive, in bytes per second"),
	}
}

func (throttle) New(params rules.Params) (Applier, error) {
	var f throttleFault
	if err := Decode(params, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// throttleFault is a configured throttle. The rate applies to what comes back:
// the response body on a request, the upstream half of a tunnel.
type throttleFault struct {
	BytesPerSec int `json:"bytes_per_sec"`
}

// Respond fetches the real response and hands its body over a pace-setter.
// Content-Length is untouched, because the body is not: it only takes longer.
func (f throttleFault) Respond(_ string, req *http.Request, next http.RoundTripper) (*http.Response, error) {
	resp, err := next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &pacedBody{ReadCloser: resp.Body, pacer: f.pacer(req.Context())}
	return resp, nil
}

// Dial opens the tunnel and paces what comes back through it.
func (f throttleFault) Dial(ctx context.Context, _, addr string, next DialFunc) (net.Conn, error) {
	conn, err := next(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return &pacedConn{Conn: conn, pacer: f.pacer(ctx)}, nil
}

func (f throttleFault) pacer(ctx context.Context) *pacer {
	return &pacer{rate: f.BytesPerSec, ctx: ctx}
}

// pacedBody reads a response body no faster than its rate allows.
type pacedBody struct {
	io.ReadCloser
	pacer *pacer
}

func (b *pacedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(b.pacer.slice(p))
	if waitErr := b.pacer.wait(n); waitErr != nil && err == nil {
		err = waitErr
	}
	return n, err
}

// pacedConn reads the upstream half of a tunnel no faster than its rate
// allows. What the client sends is left alone.
type pacedConn struct {
	net.Conn
	pacer *pacer
}

func (c *pacedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(c.pacer.slice(p))
	if waitErr := c.pacer.wait(n); waitErr != nil && err == nil {
		err = waitErr
	}
	return n, err
}

// Unwrap names the connection underneath, so a reset finds the socket.
func (c *pacedConn) Unwrap() net.Conn { return c.Conn }

// pacer holds a stream to a number of bytes per second. It works from when the
// first byte was read rather than from a bucket of tokens: every byte is due at
// a time the rate decides, and a read that finished early waits for it. Reads
// are cut to a tenth of a second's worth so the delay is spread over the whole
// stream instead of landing at the end.
type pacer struct {
	ctx     context.Context //nolint:containedctx // the wait happens during a read, not at construction
	rate    int
	started time.Time
	read    int64
}

// slice shortens a read buffer to what one pacing quantum can carry.
func (p *pacer) slice(b []byte) []byte {
	chunk := max(p.rate/10, 1)
	if len(b) > chunk {
		return b[:chunk]
	}
	return b
}

// wait holds until the bytes just read were due, or gives up early if the
// reader goes away, returning the context error.
func (p *pacer) wait(n int) error {
	if n <= 0 {
		return nil
	}
	if p.started.IsZero() {
		p.started = time.Now() // the first read is what starts the clock
	}
	p.read += int64(n)

	due := p.started.Add(time.Duration(float64(p.read) / float64(p.rate) * float64(time.Second)))
	return waitUntil(p.ctx, due)
}
