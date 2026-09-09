package faults

import (
	"context"
	"io"
	"net"
	"net/http"

	"github.com/josipmusa/faultline/internal/rules"
)

// reset is the fault that breaks the connection off, the way an upstream does
// when it dies mid-answer or a middlebox drops the flow. It needs nothing but a
// connection, so it applies to encrypted traffic too.
type reset struct{}

func init() { Register(reset{}) }

func (reset) Name() string { return "reset" }

func (reset) Tier() Tier { return TierConnection }

func (reset) Schema() Schema {
	return Schema{
		Int("after_bytes").Min(0),
	}
}

func (reset) New(params rules.Params) (Applier, error) {
	var f resetFault
	if err := Decode(params, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// resetFault is a configured reset. AfterBytes counts what reaches the client:
// response body bytes on a request, and the upstream half of a tunnel. Zero
// means the connection breaks before anything does.
type resetFault struct {
	AfterBytes int `json:"after_bytes"`
}

// Respond breaks the client connection instead of answering. With bytes to
// deliver first, the real response is fetched and its body cut short, so the
// client sees a genuine answer stop mid-flight; with none, nothing is sent
// upstream at all.
func (f resetFault) Respond(_ string, req *http.Request, next http.RoundTripper) (*http.Response, error) {
	if f.AfterBytes <= 0 {
		abortClient(req.Context())
		return nil, ErrClientReset
	}

	resp, err := next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &cutBody{ReadCloser: resp.Body, left: f.AfterBytes, ctx: req.Context()}
	return resp, nil
}

// Dial opens the tunnel and cuts it once its bytes have gone through. The
// tunnel is real: a reset is something an upstream does to a connection that
// exists, and refusing to open one is what the refuse fault is for.
func (f resetFault) Dial(ctx context.Context, ruleID, addr string, next DialFunc) (net.Conn, error) {
	conn, err := next(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return &cutConn{
		Conn: conn,
		left: f.AfterBytes,
		err:  &ResetError{Host: StripDefaultPort(addr), RuleID: ruleID},
	}, nil
}

// cutBody delivers a fixed number of bytes of a real response and then resets
// the client. The error reaches whoever is copying the body, which by then has
// nowhere to be written anyway.
type cutBody struct {
	io.ReadCloser
	ctx  context.Context //nolint:containedctx // the cut happens during a read, not at construction
	left int
	cut  bool
}

func (b *cutBody) Read(p []byte) (int, error) {
	if b.cut {
		return 0, ErrClientReset
	}
	if len(p) > b.left {
		p = p[:b.left]
	}
	n, err := b.ReadCloser.Read(p)
	b.left -= n
	if b.left > 0 {
		return n, err
	}
	b.cut = true
	abortClient(b.ctx)
	return n, ErrClientReset
}

// cutConn carries a fixed number of bytes from the upstream half of a tunnel
// and then reports the reset, which the copy turns into one on the client.
type cutConn struct {
	net.Conn
	left int
	cut  bool
	err  *ResetError
}

func (c *cutConn) Read(p []byte) (int, error) {
	if c.cut || c.left <= 0 {
		c.cut = true
		return 0, c.err
	}
	if len(p) > c.left {
		p = p[:c.left]
	}
	n, err := c.Conn.Read(p)
	c.left -= n
	if c.left > 0 {
		return n, err
	}
	c.cut = true
	return n, c.err
}

// Unwrap names the connection underneath, so a reset finds the socket.
func (c *cutConn) Unwrap() net.Conn { return c.Conn }
