package forward

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
)

// serveOneConn serves HTTP requests arriving on a single connection until the
// client hangs up. Both tunnel paths that can read the requests inside a
// tunnel use it: the interceptor once it has terminated the TLS, and the
// plaintext path when the client never started any.
//
// One http.Server per connection is what makes this work: the listener hands
// over its one connection and then blocks until that connection has been
// served, so Serve returns when the client is done and nothing lingers.
func serveOneConn(ctx context.Context, conn net.Conn, h http.Handler, log *slog.Logger) {
	done := make(chan struct{})
	var once sync.Once
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ConnState: func(_ net.Conn, state http.ConnState) {
			if state == http.StateClosed || state == http.StateHijacked {
				once.Do(func() { close(done) })
			}
		},
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelDebug),
	}
	_ = srv.Serve(&oneConnListener{conn: conn, done: done})
}

// oneConnListener hands out a single connection, then blocks until that
// connection has been served and reports itself closed.
type oneConnListener struct {
	conn net.Conn
	done <-chan struct{}
	once sync.Once
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	var conn net.Conn
	l.once.Do(func() { conn = l.conn })
	if conn != nil {
		return conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *oneConnListener) Close() error   { return nil }
func (l *oneConnListener) Addr() net.Addr { return l.conn.LocalAddr() }
