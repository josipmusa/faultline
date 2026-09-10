package admin

import (
	"net/http"

	"github.com/josipmusa/faultline/internal/capture"
)

// CapturesFrom serves the headers and bodies held in store at
// GET /api/events/{id}/capture. A server with no capture store answers that
// nothing was captured, which is also what a server whose captures have aged
// out says: from the caller's side the two are the same.
func (s *Server) CapturesFrom(store *capture.Store) { s.captures = store }

// getCapture answers with what went over the wire for one event. The two
// not-found cases are told apart on purpose: an unknown id means the caller is
// asking about the wrong event, while an id that was recorded but has no
// capture means the exchange has aged out of the capture budget, or was
// encrypted and never had one.
func (s *Server) getCapture(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !s.recorded(id) {
		s.fail(w, missing("no event %s", id))
		return
	}
	if s.captures == nil {
		s.fail(w, missing("the capture for event %s is no longer held", id))
		return
	}
	c, ok := s.captures.Get(id)
	if !ok {
		s.fail(w, missing("the capture for event %s is no longer held", id))
		return
	}
	s.writeJSON(w, http.StatusOK, c)
}

// recorded says whether an event with this id is still in the ring.
func (s *Server) recorded(id string) bool {
	for _, e := range s.events.Events() {
		if e.ID == id {
			return true
		}
	}
	return false
}
