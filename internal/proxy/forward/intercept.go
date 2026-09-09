package forward

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

// handshakeTimeout bounds how long a client may take to complete the TLS
// handshake after the tunnel opens.
const handshakeTimeout = 10 * time.Second

// ErrClientRejectedCertificate is the error recorded when a client ends the
// handshake after seeing the leaf Faultline minted. That is what a client
// that does not trust the CA, or that pins its upstream's certificate, does.
const ErrClientRejectedCertificate = "client rejected certificate; CA not trusted or pinned"

// Interceptor terminates the TLS inside a CONNECT tunnel with a certificate
// minted for the host, then serves the decrypted requests through the
// intercepted-tier fault pipeline. It is the intercepted counterpart of the
// blind tunnel.
type Interceptor struct {
	issuer    *tlsmitm.Issuer
	transport http.RoundTripper
	events    *events.Recorder
	log       *slog.Logger
}

// NewInterceptor wires interception onto the intercepted-tier pipeline. The
// transport should be a faults.Transport with tier intercepted, so every
// request inside a tunnel is recorded as one; the recorder is for the
// handshakes that never get that far. A nil recorder records nothing; a nil
// logger means slog.Default.
func NewInterceptor(issuer *tlsmitm.Issuer, transport http.RoundTripper, rec *events.Recorder, logger *slog.Logger) *Interceptor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Interceptor{issuer: issuer, transport: transport, events: rec, log: logger}
}

// serve runs one intercepted tunnel to completion: handshake with the client
// as target, then serve its requests until it hangs up.
func (i *Interceptor) serve(ctx context.Context, client net.Conn, target string) {
	start := time.Now()
	host := faults.StripDefaultPort(target)

	tlsConn := tls.Server(client, i.issuer.ServerConfig(target))
	handshakeCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	err := tlsConn.HandshakeContext(handshakeCtx)
	cancel()
	if err != nil {
		reason := describeHandshakeError(err)
		i.log.Warn("TLS interception failed", "upstream", host, "reason", reason, "err", err)
		i.record(events.Event{
			ID:     events.NextID(),
			Host:   host,
			Method: http.MethodConnect,
			Tier:   events.TierIntercepted,
			Error:  reason,
		}, start)
		return
	}

	// One http.Server per tunnel: the listener hands over the single
	// connection and then blocks until it has been served, so Serve returns
	// when the client is done and nothing lingers.
	done := make(chan struct{})
	var once sync.Once
	srv := &http.Server{
		Handler:           i.handler(target),
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ConnState: func(_ net.Conn, state http.ConnState) {
			if state == http.StateClosed || state == http.StateHijacked {
				once.Do(func() { close(done) })
			}
		},
		ErrorLog: slog.NewLogLogger(i.log.Handler(), slog.LevelDebug),
	}
	_ = srv.Serve(&oneConnListener{conn: tlsConn, done: done})
}

// handler forwards decrypted requests to the host the CONNECT named. The
// requests arrive in origin form, so the destination comes from the tunnel
// rather than the request line; the Host header the client sent stays as it
// is, the same as the plain handler.
func (i *Interceptor) handler(target string) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme = "https"
			r.Out.URL.Host = target
		},
		Transport: i.transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			i.log.Error("upstream unreachable", "upstream", faults.StripDefaultPort(target), "method", r.Method, "path", r.URL.Path, "err", err)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "faultline: upstream unreachable\n")
		},
	}
}

func (i *Interceptor) record(e events.Event, start time.Time) {
	if i.events == nil {
		return
	}
	e.Timestamp = time.Now()
	e.DurationMS = time.Since(start).Milliseconds()
	i.events.Record(e)
}

// describeHandshakeError turns a failed handshake into the sentence the event
// carries. A client that does not trust the CA, or pins its upstream, either
// sends a certificate alert or simply hangs up once it has seen the leaf; both
// are reported as a rejection, since from Faultline's side they are the same
// thing and the fix is the same.
func describeHandshakeError(err error) string {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "remote error" && strings.Contains(opErr.Err.Error(), "certificate") {
		return ErrClientRejectedCertificate
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return ErrClientRejectedCertificate
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "client did not complete the TLS handshake in time"
	}
	return "TLS handshake failed: " + err.Error()
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
