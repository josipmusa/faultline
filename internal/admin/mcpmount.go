package admin

import (
	"context"
	"net/http"
)

// MountMCP serves the agent interface at /mcp on the admin port. Call it before
// Start.
//
// The handler comes from outside rather than being built here: clients/go names
// this package's wire types, and internal/mcp is a client of the API through
// clients/go, so this package importing it would close a cycle. Whoever
// assembles the process owns that wiring, and this package owns the route and
// the lifecycle.
func (s *Server) MountMCP(h http.Handler) { s.mcp = h }

// serveMCP hands the request to the mounted agent interface, under a context
// that ends when the server starts draining.
//
// That cancellation is the point. A streamable MCP session holds a
// text/event-stream open for as long as the agent is attached, and such a
// connection never goes idle, so http.Server.Shutdown would wait for it until
// the shutdown deadline and then report a failure. Ending the request instead
// lets the response finish, the connection go idle, and Ctrl-C stay the clean
// exit it is everywhere else. The event stream needed the same treatment for
// the same reason.
//
// The route exists whether anything is mounted or not, so an agent that finds
// nothing there is told so in the API's error shape instead of falling through
// to the UI and being handed a page.
func (s *Server) serveMCP(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		s.fail(w, missing("the MCP interface is not mounted on this instance; "+
			"`faultline mcp` serves the same tools over stdio"))
		return
	}
	if !s.enterStream() {
		s.fail(w, unavailable("faultline is shutting down"))
		return
	}
	defer s.leaveStream()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-s.drained():
			cancel()
		case <-ctx.Done():
		}
	}()

	s.mcp.ServeHTTP(w, r.WithContext(ctx))
}
