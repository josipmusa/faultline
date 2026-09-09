package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Stream follows the read-only event stream, calling onMessage for every
// message until the context is cancelled, the server closes the socket, or
// onMessage returns an error. A cancelled context comes back as its own error,
// so a caller can tell "I stopped this" from "it stopped".
//
// The socket is read-only by design: nothing is ever written to it, which is
// also what keeps the server from closing it on a policy violation.
func (c *Client) Stream(ctx context.Context, onMessage func(Message) error) error {
	conn, err := c.dialStream(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()

	for {
		var m Message
		if err := wsjson.Read(ctx, conn, &m); err != nil {
			return streamError(ctx, err)
		}
		if err := onMessage(m); err != nil {
			return err
		}
	}
}

func (c *Client) dialStream(ctx context.Context) (*websocket.Conn, error) {
	target := *c.base
	target.Scheme = "ws"
	if c.base.Scheme == "https" {
		target.Scheme = "wss"
	}
	target.Path = c.base.Path + "/api/events/stream"

	// The admin socket checks the Origin header against the Host when one is
	// sent, so send the address we are dialling rather than leaving a browser's
	// header to chance. Nothing is weakened on the server for a command line
	// client's sake.
	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{c.base.String()}},
	}

	conn, resp, err := websocket.Dial(ctx, target.String(), opts)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		if resp != nil && resp.StatusCode >= http.StatusBadRequest {
			return nil, fmt.Errorf("client: the event stream refused the connection: %w", err)
		}
		return nil, c.reachError(err)
	}
	return conn, nil
}

// streamError reads the end of a stream. A close the server sent on purpose is
// the end of the stream, not a failure.
func streamError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return nil
	}
	return fmt.Errorf("client: reading the event stream: %w", err)
}
