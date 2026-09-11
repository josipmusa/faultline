package admin

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

// The MCP interface is mounted from outside: clients/go names this package's
// types, so this package cannot import internal/mcp without a cycle. What it
// owns is the route and what answers on it when nothing is mounted.
func TestMCPRouteServesWhatWasMounted(t *testing.T) {
	s := newTestServer(t)
	s.MountMCP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "mounted")
	}))

	w := do(t, s, http.MethodPost, "/mcp", "")

	wantStatus(t, w, http.StatusOK)
	if got := w.Body.String(); got != "mounted" {
		t.Errorf("/mcp = %q, want the mounted handler's answer", got)
	}
}

// A server with no MCP mounted is the one clients/go's harness builds. The
// route still answers, and it answers in the API's error shape rather than
// falling through to the UI, which would hand an agent an HTML page.
func TestMCPRouteSaysWhenNothingIsMounted(t *testing.T) {
	s := newTestServer(t)

	wantError(t, do(t, s, http.MethodPost, "/mcp", ""), http.StatusNotFound, "")
}

// An agent attached to /mcp holds a long-lived stream open. Shutdown has to end
// it rather than wait for it, or a Ctrl-C with an agent connected hangs until
// the shutdown timeout and reports a failure instead of a clean exit. This is
// the same problem the event stream had, and it gets the same answer.
func TestMCPRequestsEndWhenTheServerDrains(t *testing.T) {
	s := newTestServer(t)

	serving := make(chan struct{})
	ended := make(chan struct{})
	s.MountMCP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(serving)
		<-r.Context().Done() // a stream that only the server can end
		close(ended)
	}))

	go func() { _ = do(t, s, http.MethodGet, "/mcp", "") }()
	<-serving

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the MCP request is still running after Shutdown returned")
	}
}

// A late agent is turned away rather than handed a stream about to close, the
// way a late event stream client is.
func TestMCPRouteTurnsAwayAnAgentWhileDraining(t *testing.T) {
	s := newTestServer(t)
	s.MountMCP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "mounted")
	}))

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	wantError(t, do(t, s, http.MethodPost, "/mcp", ""), http.StatusServiceUnavailable, "")
}
