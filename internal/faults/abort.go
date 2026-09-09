package faults

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
)

// ErrClientReset says a fault has already reset the client connection, so
// there is nothing left to answer. The proxy edges recognise it and stay
// quiet rather than writing a response into a socket that is gone.
var ErrClientReset = errors.New("faultline: the connection was reset by a rule")

// ResetError travels the other way: out of a connection a fault is resetting,
// towards whoever is copying bytes through it. A tunnel has no request to fail,
// so this is how the copy learns to reset the client rather than close it
// politely.
type ResetError struct {
	Host   string
	RuleID string
}

func (e *ResetError) Error() string {
	return "faultline: connection to " + e.Host + " reset by rule " + e.RuleID
}

// IsReset reports whether err is a connection a rule reset.
func IsReset(err error) bool {
	var reset *ResetError
	return errors.As(err, &reset)
}

// Abort closes conn so its peer sees a reset rather than an orderly close. It
// is the only place in Faultline that produces one: a lingerless close is what
// turns a Go Close into the RST a client reports as "connection reset by peer".
func Abort(conn net.Conn) {
	if tcp := tcpConn(conn); tcp != nil {
		// Straight to the socket: closing a TLS connection would send the peer
		// a polite close_notify first, which is the opposite of a reset.
		_ = tcp.SetLinger(0)
		_ = tcp.Close()
		return
	}
	_ = conn.Close()
}

// tcpConn finds the socket under the layers a connection wears by the time a
// fault reaches it: a buffered connection around a hijacked one, or a TLS
// connection around that. Anything else has no linger to set and is closed as
// it is.
func tcpConn(conn net.Conn) *net.TCPConn {
	for range 8 { // deep enough for every real wrapping, and never a cycle
		switch c := conn.(type) {
		case *net.TCPConn:
			return c
		case interface{ NetConn() net.Conn }:
			conn = c.NetConn()
		case interface{ Unwrap() net.Conn }:
			conn = c.Unwrap()
		default:
			return nil
		}
		if conn == nil {
			return nil
		}
	}
	return nil
}

// AbortResponse resets the client connection behind w. The response writer is
// taken over first, because a served connection belongs to the HTTP server
// until it is hijacked.
func AbortResponse(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		slog.Default().Warn("cannot reset the connection: this server does not support hijacking")
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		slog.Default().Warn("cannot reset the connection", "err", err)
		return
	}
	Abort(conn)
}

// aborterKey addresses the aborter in a request context.
type aborterKey struct{}

// WithAbort wraps h so the faults acting on its requests can reset the client
// connection. A fault owns neither end of that connection, so every proxy edge
// that serves requests through the pipeline installs this.
//
// A fault asks for the reset while the response is still being copied, but the
// reset happens once the handler has unwound: the proxy underneath flushes the
// response from a goroutine of its own, and taking the connection out from
// under it mid-copy is a race. Nothing reaches the client in between, because a
// fault that asks for a reset also fails the read or the round trip.
func WithAbort(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requested atomic.Bool
		defer func() {
			if requested.Load() {
				AbortResponse(w)
			}
		}()
		ctx := context.WithValue(r.Context(), aborterKey{}, func() { requested.Store(true) })
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// abortClient asks for the client connection this request arrived on to be
// reset, reporting whether it could. It is false only where no edge installed an aborter, which
// leaves the fault to fail the request in the ordinary way.
func abortClient(ctx context.Context) bool {
	abort, ok := ctx.Value(aborterKey{}).(func())
	if !ok {
		return false
	}
	abort()
	return true
}
