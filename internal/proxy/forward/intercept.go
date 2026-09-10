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

	// bypassed answers whether a tunnel's host has been put on the bypass
	// list since the tunnel was opened. Set by the Server that owns the list.
	bypassed func(addr string) bool
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

	serveOneConn(ctx, tlsConn, i.handler(target), i.log)
}

// handler forwards decrypted requests to the host the CONNECT named. The
// requests arrive in origin form, so the destination comes from the tunnel
// rather than the request line; the Host header the client sent stays as it
// is, the same as the plain handler.
func (i *Interceptor) handler(target string) http.Handler {
	return faults.WithAbort(&httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme = "https"
			r.Out.URL.Host = target
		},
		Transport: i.tunnelTransport(target),
		ErrorLog:  slog.NewLogLogger(i.log.Handler(), slog.LevelDebug),
		// Flush every write instead of waiting for a buffer to fill, so a fault
		// that paces or cuts a body reaches the client as it happens rather
		// than all at once at the end.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, faults.ErrClientReset) {
				return // a rule already reset the connection; there is nobody left to tell
			}
			i.log.Error("upstream unreachable", "upstream", faults.StripDefaultPort(target), "method", r.Method, "path", r.URL.Path, "err", err)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "faultline: upstream unreachable\n")
		},
	})
}

// tunnelTransport is the pipeline for one tunnel's requests, which drops out
// of the way if the host is bypassed while the tunnel is open. Without a
// bypass list there is nothing to consult and the pipeline is used directly.
func (i *Interceptor) tunnelTransport(target string) http.RoundTripper {
	if i.bypassed == nil {
		return i.transport
	}
	return &tunnelTransport{
		faulted:   i.transport,
		untouched: withoutTheFaultPipeline(i.transport),
		addr:      target,
		bypassed:  i.bypassed,
	}
}

// withoutTheFaultPipeline is what a bypassed request inside an open tunnel
// travels over: the transport the pipeline itself dials with, so the request
// reaches the upstream over the same connections and the same trust as the
// ones before it, with none of the rules or the recording. A transport that is
// not a fault pipeline is already that.
func withoutTheFaultPipeline(t http.RoundTripper) http.RoundTripper {
	if p, ok := t.(interface{ Base() http.RoundTripper }); ok {
		return p.Base()
	}
	return t
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
// carries. A client that does not trust the CA, or pins its upstream, sends a
// certificate alert, hangs up once it has seen the leaf, or sends an alert
// Faultline cannot read; all three are reported as a rejection, since from
// Faultline's side they are the same thing and the fix is the same.
//
// The unreadable alert is what OpenSSL clients do over TLS 1.3: the server
// half of the handshake is finished and its keys have moved on by the time the
// client gives up, so the alert arrives under the earlier keys and fails to
// decrypt. A bad record MAC on a connection whose only traffic so far is a
// handshake is that, not corruption on the wire.
func describeHandshakeError(err error) string {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "remote error" && strings.Contains(opErr.Err.Error(), "certificate") {
		return ErrClientRejectedCertificate
	}
	if errors.As(err, &opErr) && opErr.Op == "local error" && strings.Contains(opErr.Err.Error(), "bad record MAC") {
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
