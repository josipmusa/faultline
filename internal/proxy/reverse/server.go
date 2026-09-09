// Package reverse serves explicit routes: one local port that stands in for one
// upstream, with the fault pipeline in between.
//
// An explicit route is the simplest way to attach Faultline to an application.
// The application points at `localhost:9100` instead of the real upstream, and
// everything else — matching, faults, recording — happens in the transport.
package reverse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/josipmusa/faultline/internal/faults"
)

// FirstPort is where explicit routes start when no port is given.
const FirstPort = 9100

// readHeaderTimeout bounds how long a client may take to send its request
// headers. It deliberately does not bound the response: a delay fault is
// supposed to hold a request open for as long as the rule says.
const readHeaderTimeout = 10 * time.Second

// Route is one local port standing in for one upstream. A Port of 0 lets the
// operating system pick a free one, and Addr reports what it picked.
type Route struct {
	Name     string
	Port     int
	Upstream *url.URL
}

// Server runs one listener per route. Build it with NewServer, then Start.
type Server struct {
	routes    []Route
	transport http.RoundTripper
	log       *slog.Logger

	mu      sync.Mutex
	servers []*http.Server
	addrs   map[string]string
	wg      sync.WaitGroup
}

// NewServer validates the routes and prepares a server for them. The transport
// is the fault pipeline; a nil logger means slog.Default.
func NewServer(routes []Route, transport http.RoundTripper, logger *slog.Logger) (*Server, error) {
	if err := validate(routes); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		routes:    slices.Clone(routes),
		transport: transport,
		log:       logger,
		addrs:     make(map[string]string, len(routes)),
	}, nil
}

// Start binds every route before serving any of them, so a port clash is
// reported instead of leaving some routes half-up. Serving continues in the
// background until Shutdown.
func (s *Server) Start() error {
	for _, r := range s.routes {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(r.Port)))
		if err != nil {
			// Undo the routes that did bind: a half-started server is worse
			// than none, because the user cannot tell which ports are live.
			_ = s.Shutdown(context.Background())
			return fmt.Errorf("route %q: listening on port %d: %w", r.Name, r.Port, err)
		}

		srv := &http.Server{
			Handler:           handler(r.Upstream, s.transport, s.log),
			ReadHeaderTimeout: readHeaderTimeout,
			ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelDebug),
		}

		s.mu.Lock()
		s.servers = append(s.servers, srv)
		s.addrs[r.Name] = ln.Addr().String()
		s.mu.Unlock()

		s.log.Info("route listening", "route", r.Name, "addr", ln.Addr().String(), "upstream", r.Upstream.String())

		s.wg.Go(func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.log.Error("route stopped", "route", r.Name, "err", err)
			}
		})
	}
	return nil
}

// Addr is the address a route is listening on, or "" if it is not listening.
func (s *Server) Addr(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addrs[name]
}

// Shutdown stops accepting connections and waits for in-flight requests, giving
// up when ctx expires. It is safe to call more than once.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	servers := s.servers
	s.servers = nil
	clear(s.addrs)
	s.mu.Unlock()

	var errs []error
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	s.wg.Wait()
	return errors.Join(errs...)
}

// handler proxies to one upstream through the fault pipeline. httputil's
// ReverseProxy already strips hop-by-hop headers in both directions, and
// SetURL rewrites the outbound Host to the upstream, which is what an explicit
// route wants: the upstream must see its own name, not `localhost:9100`.
func handler(upstream *url.URL, transport http.RoundTripper, log *slog.Logger) http.Handler {
	return faults.WithAbort(&httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstream)
		},
		Transport: transport,
		ErrorLog:  slog.NewLogLogger(log.Handler(), slog.LevelDebug),
		// Flush every write instead of waiting for a buffer to fill, so a fault
		// that paces or cuts a body reaches the client as it happens rather
		// than all at once at the end.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, faults.ErrClientReset) {
				return // a rule already reset the connection; there is nobody left to tell
			}
			// The upstream really failed. Faultline says so plainly rather than
			// inventing a response, and keeps the detail in the log.
			log.Error("upstream unreachable", "upstream", upstream.Host, "method", r.Method, "path", r.URL.Path, "err", err)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "faultline: upstream unreachable\n")
		},
	})
}

// validate checks the route set as a whole: every route usable, no two sharing
// a name or a port.
func validate(routes []Route) error {
	if len(routes) == 0 {
		return errors.New("no routes configured")
	}

	names := make(map[string]bool, len(routes))
	ports := make(map[int]bool, len(routes))

	for _, r := range routes {
		if err := r.validate(); err != nil {
			return err
		}
		if names[r.Name] {
			return fmt.Errorf("duplicate route name %q", r.Name)
		}
		names[r.Name] = true
		if r.Port != 0 {
			if ports[r.Port] {
				return fmt.Errorf("duplicate route port %d", r.Port)
			}
			ports[r.Port] = true
		}
	}
	return nil
}

// validate reports whether the route can be served.
func (r Route) validate() error {
	if r.Name == "" {
		return errors.New("route name is empty")
	}
	if r.Upstream == nil || r.Upstream.Host == "" {
		return fmt.Errorf("route %q: upstream must be an absolute URL like https://api.stripe.com", r.Name)
	}
	if r.Upstream.Scheme != "http" && r.Upstream.Scheme != "https" {
		return fmt.Errorf("route %q: upstream scheme %q is not supported, use http or https", r.Name, r.Upstream.Scheme)
	}
	if r.Port < 0 || r.Port > 65535 {
		return fmt.Errorf("route %q: port %d is outside 1-65535", r.Name, r.Port)
	}
	return nil
}
