package admin

import "net/http"

// A Rearmer forgets the behavior state rules keep between requests, so a spent
// first_n counts from the beginning again. The fault pipeline's Gate is the one
// implementation; it is reached through an interface so the API does not depend
// on the pipeline to clear events.
type Rearmer interface {
	// Reset forgets every rule's behavior state without changing any rule.
	Reset()
}

// Rearms lets POST /api/sessions/current/reset re-arm the rules as well as
// clear what was observed. Call it before Start. A server without one still
// resets: it clears the events and the captures, which is everything it holds
// itself.
func (s *Server) Rearms(r Rearmer) { s.rearm = r }

// resetSession puts the session back to the start of a measurement without
// disturbing the setup being measured. What Faultline observed goes, and every
// rule's behavior state is re-armed; the rules and the active scenario stay
// exactly as they are.
//
// That split is the point. An agent verifying the same behavior twice needs the
// second run to see the fault the first one spent, and needs the rules it
// arranged to still be there. Turning rules off as well would make a reset
// destroy a situation a human set up in the UI.
func (s *Server) resetSession(w http.ResponseWriter, _ *http.Request) {
	s.events.Clear()
	if s.captures != nil {
		s.captures.Clear()
	}
	if s.rearm != nil {
		s.rearm.Reset()
	}
	w.WriteHeader(http.StatusNoContent)
}
