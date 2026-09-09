// Package forward serves the forward proxy: one local port that any client can
// point HTTP_PROXY at, standing in for every upstream at once.
//
// A forward proxy request carries its destination in the request line, in
// absolute form (`GET http://api.stripe.com/v1/charges HTTP/1.1`) for plain
// HTTP, or as a bare host:port behind CONNECT for HTTPS. Plain requests go
// through the fault transport exactly as an explicit route does; CONNECT opens
// a tunnel through the fault dialer, which can only see the host.
package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/josipmusa/faultline/internal/faults"
)

// DefaultPort is where the forward proxy listens.
const DefaultPort = 9001

// readHeaderTimeout bounds how long a client may take to send its request
// headers. It deliberately does not bound the response: a delay fault is
// supposed to hold a request open for as long as the rule says.
const readHeaderTimeout = 10 * time.Second

// Server is the forward proxy. Build it with NewServer; it is a plain
// http.Handler until Start binds a port.
type Server struct {
	handler http.Handler
	dialer  *faults.Dialer
	log     *slog.Logger

	mu   sync.Mutex
	http *http.Server
	addr string
	wg   sync.WaitGroup
}

// NewServer wires the forward proxy onto the fault pipeline: the transport
// for plain requests, the dialer for CONNECT tunnels. A nil dialer tunnels
// without rules or events; a nil logger means slog.Default.
func NewServer(transport http.RoundTripper, dialer *faults.Dialer, logger *slog.Logger) *Server {
	if dialer == nil {
		dialer = faults.NewDialer(nil, nil)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{handler: proxyHandler(transport, logger), dialer: dialer, log: logger}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		s.tunnel(w, r)
		return
	}
	if err := usable(r); err != nil {
		http.Error(w, "faultline: "+err.message, err.status)
		return
	}
	s.handler.ServeHTTP(w, r)
}

// Start binds the forward proxy port on localhost and serves in the background.
func (s *Server) Start(port int) error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("forward proxy: listening on port %d: %w", port, err)
	}

	srv := &http.Server{Handler: s, ReadHeaderTimeout: readHeaderTimeout}

	s.mu.Lock()
	s.http = srv
	s.addr = ln.Addr().String()
	s.mu.Unlock()

	s.log.Info("forward proxy listening", "addr", ln.Addr().String())

	s.wg.Go(func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("forward proxy stopped", "err", err)
		}
	})
	return nil
}

// Addr is the address the forward proxy is listening on, empty when it is not.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Shutdown stops accepting connections and waits for in-flight requests, giving
// up when ctx expires. It is safe to call more than once.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.http
	s.http, s.addr = nil, ""
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	err := srv.Shutdown(ctx)
	s.wg.Wait()
	return err
}

// refusal is a request Faultline will not proxy, and the reason a human needs
// to hear.
type refusal struct {
	status  int
	message string
}

// usable reports why a plain request cannot be forwarded, or nil when it can.
// CONNECT is dispatched before this runs, so the only acceptable form left is
// the absolute one. Anything else means the client is talking to Faultline as
// if it were an ordinary web server.
func usable(r *http.Request) *refusal {
	if r.URL == nil || !r.URL.IsAbs() {
		return &refusal{
			status: http.StatusBadRequest,
			message: "this is a forward proxy, so requests need an absolute URL in the request line " +
				"(GET http://host/path); point HTTP_PROXY at it or use curl -x instead of requesting a path directly",
		}
	}
	if !strings.EqualFold(r.URL.Scheme, "http") {
		return &refusal{
			status:  http.StatusBadRequest,
			message: fmt.Sprintf("scheme %q is not supported; this proxy forwards http, and https arrives as CONNECT", r.URL.Scheme),
		}
	}
	if r.URL.Host == "" {
		return &refusal{
			status:  http.StatusBadRequest,
			message: "the absolute URL has no host, so there is no upstream to forward to",
		}
	}
	return nil
}

// proxyHandler forwards an absolute-form request to the host it names.
//
// httputil's ReverseProxy is doing the hop-by-hop work the old forward proxy
// did by hand, in both directions and including the headers a Connection
// header lists, which is what a proxy must do. Rewrite, rather than Director,
// also means no X-Forwarded-* headers are added: a forward proxy is not
// supposed to announce its client to the upstream.
func proxyHandler(transport http.RoundTripper, log *slog.Logger) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			// The destination is already in the URL. Only the Host header needs
			// saying, so the upstream sees its own name rather than whatever
			// the client happened to put there.
			r.Out.Host = r.Out.URL.Host
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// The upstream really failed. Faultline says so plainly rather than
			// inventing a response, and keeps the detail in the log.
			log.Error("upstream unreachable", "upstream", r.URL.Host, "method", r.Method, "path", r.URL.Path, "err", err)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "faultline: upstream unreachable\n")
		},
	}
}
