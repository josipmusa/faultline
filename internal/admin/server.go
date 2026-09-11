// Package admin serves the HTTP API over the rule store and the event recorder.
package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/josipmusa/faultline/internal/capture"
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/rules"
)

// DefaultPort is the admin port: the HTTP API, and later the UI, the WebSocket
// and MCP, all live behind it.
const DefaultPort = 9000

const readHeaderTimeout = 10 * time.Second

// Server is the admin HTTP API over the rule store, the scenarios, and the
// event recorder.
type Server struct {
	rules     *rules.Store
	scenarios *rules.Scenarios
	events    *events.Recorder
	captures  *capture.Store
	bypass    *forward.Bypass
	trust     []string
	log       *slog.Logger
	mux       *http.ServeMux
	watcher   *ruleWatcher
	persist   Persister
	rearm     Rearmer
	mcp       http.Handler
	// draining is closed when Shutdown begins, so a request that would
	// otherwise outlive the server can end itself.
	drain     chan struct{}
	drainOnce sync.Once

	mu       sync.Mutex
	http     *http.Server
	addr     string
	draining bool
	streams  sync.WaitGroup
	wg       sync.WaitGroup
}

// NewServer wires the API onto a rule store, an event recorder and the forward
// proxy's bypass list, which may be nil. trustVars are the trust variables
// Faultline set for a wrapped child, reported with a host whose client
// rejected the interception certificate; there are none when Faultline runs no
// child. Nothing is listening until Start is called; the Server is a plain
// http.Handler until then.
func NewServer(store *rules.Store, scenarios *rules.Scenarios, rec *events.Recorder, bypass *forward.Bypass, trustVars []string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		rules:     store,
		scenarios: scenarios,
		events:    rec,
		bypass:    bypass,
		trust:     trustVars,
		log:       logger,
		mux:       http.NewServeMux(),
		watcher:   newRuleWatcher(store.Changes()),
		drain:     make(chan struct{}),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.health)

	s.mux.HandleFunc("GET /api/config", s.getConfig)

	s.mux.HandleFunc("GET /api/catalogue", s.listCatalogue)

	s.mux.HandleFunc("GET /api/rules", s.listRules)
	s.mux.HandleFunc("POST /api/rules", s.createRule)
	s.mux.HandleFunc("GET /api/rules/{id}", s.getRule)
	s.mux.HandleFunc("PUT /api/rules/{id}", s.updateRule)
	s.mux.HandleFunc("DELETE /api/rules/{id}", s.deleteRule)
	s.mux.HandleFunc("POST /api/rules/{id}/enable", s.enableRule)
	s.mux.HandleFunc("POST /api/rules/{id}/disable", s.disableRule)

	s.mux.HandleFunc("GET /api/scenarios", s.listScenarios)
	s.mux.HandleFunc("POST /api/scenarios", s.createScenario)
	s.mux.HandleFunc("POST /api/scenarios/{name}/activate", s.activateScenario)
	s.mux.HandleFunc("POST /api/scenarios/{name}/deactivate", s.deactivateScenario)

	s.mux.HandleFunc("GET /api/events", s.listEvents)
	s.mux.HandleFunc("GET /api/events/stream", s.streamEvents)
	s.mux.HandleFunc("GET /api/events/{id}/capture", s.getCapture)
	s.mux.HandleFunc("DELETE /api/events", s.clearEvents)
	s.mux.HandleFunc("GET /api/upstreams", s.listUpstreams)
	s.mux.HandleFunc("POST /api/bypass", s.addBypass)
	s.mux.HandleFunc("DELETE /api/bypass/{host}", s.removeBypass)
	s.mux.HandleFunc("GET /api/sessions/current/report", s.sessionReport)
	s.mux.HandleFunc("POST /api/sessions/current/reset", s.resetSession)

	s.mux.HandleFunc("/mcp", s.serveMCP)

	// The UI takes over /, so unknown endpoints keep the JSON error shape under
	// /api rather than answering a mistyped page with it.
	s.mux.HandleFunc("/api/", s.unknown)
	s.mux.Handle("/", uiHandler())
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Start binds the admin port on localhost and serves in the background.
func (s *Server) Start(port int) error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("admin: listening on port %d: %w", port, err)
	}

	srv := &http.Server{Handler: s, ReadHeaderTimeout: readHeaderTimeout}

	s.mu.Lock()
	s.http = srv
	s.addr = ln.Addr().String()
	s.mu.Unlock()

	s.log.Info("admin listening", "addr", ln.Addr().String())

	s.wg.Go(func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("admin stopped", "err", err)
		}
	})
	return nil
}

// Addr is the address the admin server is listening on, empty when it is not.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Shutdown stops serving, letting in-flight requests finish within ctx.
// Shutdown stops taking new work and waits, until ctx expires, for what is
// already running to finish: first the event streams, then the plain requests.
// drained is closed once the server has started shutting down. A handler for a
// request that would otherwise outlive the server waits on it.
func (s *Server) drained() <-chan struct{} { return s.drain }

// Shutdown stops serving, letting in-flight requests finish within ctx.
// Shutdown stops taking new work and waits, until ctx expires, for what is
// already running to finish: first the event streams, then the plain requests.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.http
	s.http, s.addr = nil, ""
	s.draining = true
	s.mu.Unlock()

	// Ending the long-lived requests comes before waiting for the HTTP server:
	// a streamable MCP session is an ordinary connection that never goes idle,
	// so Shutdown would wait out its whole deadline on one.
	s.drainOnce.Do(func() { close(s.drain) })

	// Closing the watcher tells every open stream to close. Waiting for them is
	// on us: a hijacked connection is invisible to http.Server.Shutdown, so
	// without this the process can exit before the close frame is flushed.
	s.watcher.close()
	errs := []error{waitFor(ctx, &s.streams)}

	if srv != nil {
		errs = append(errs, srv.Shutdown(ctx))
		s.wg.Wait()
	}
	return errors.Join(errs...)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) unknown(w http.ResponseWriter, r *http.Request) {
	s.fail(w, missing("no endpoint at %s %s", r.Method, r.URL.Path))
}
