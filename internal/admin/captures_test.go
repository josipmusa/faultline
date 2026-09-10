package admin

import (
	"net/http"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/capture"
	"github.com/josipmusa/faultline/internal/events"
)

// withCaptures gives the server a capture store and files one capture against
// the newest seeded event, returning the store and that event's id.
func withCaptures(t *testing.T, s *Server) (*capture.Store, string) {
	t.Helper()
	caps := capture.NewStore(0)
	s.CapturesFrom(caps)

	seed(t, s)
	all := s.events.Events()
	id := all[len(all)-1].ID

	caps.Put(capture.Capture{
		EventID:  id,
		Request:  capture.Side{Headers: http.Header{"X-Test": []string{"1"}}, Body: []byte("who=me")},
		Response: capture.Side{Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"ok":true}`), Truncated: true},
	})
	return caps, id
}

func TestGetCaptureReturnsBothSides(t *testing.T) {
	s := newTestServer(t)
	_, id := withCaptures(t, s)

	w := do(t, s, http.MethodGet, "/api/events/"+id+"/capture", "")

	wantStatus(t, w, http.StatusOK)
	got := decodeBody[capture.Capture](t, w)
	if got.EventID != id {
		t.Errorf("event_id = %q, want %q", got.EventID, id)
	}
	if string(got.Request.Body) != "who=me" {
		t.Errorf("request body = %q, want %q", got.Request.Body, "who=me")
	}
	if got.Request.Headers.Get("X-Test") != "1" {
		t.Errorf("request header X-Test = %q, want 1", got.Request.Headers.Get("X-Test"))
	}
	if string(got.Response.Body) != `{"ok":true}` {
		t.Errorf("response body = %q, want the JSON body", got.Response.Body)
	}
	if !got.Response.Truncated {
		t.Error("response truncated = false, want true")
	}
}

// The two 404s say different things: one means the id is wrong, the other
// means the id is right and the capture has aged out of the budget.
func TestGetCaptureOnAnUnknownEventSaysTheIdIsUnknown(t *testing.T) {
	s := newTestServer(t)
	withCaptures(t, s)

	w := do(t, s, http.MethodGet, "/api/events/999999/capture", "")

	wantStatus(t, w, http.StatusNotFound)
	if got := decodeBody[apiError](t, w).Message; !strings.Contains(got, "no event") {
		t.Errorf("error = %q, want it to say the event is unknown", got)
	}
}

func TestGetCaptureOnAnEvictedCaptureSaysSo(t *testing.T) {
	s := newTestServer(t)
	caps, _ := withCaptures(t, s)
	caps.Clear()

	all := s.events.Events()
	w := do(t, s, http.MethodGet, "/api/events/"+all[0].ID+"/capture", "")

	wantStatus(t, w, http.StatusNotFound)
	if got := decodeBody[apiError](t, w).Message; !strings.Contains(got, "no longer held") {
		t.Errorf("error = %q, want it to say the capture is no longer held", got)
	}
}

// A server with no capture store at all answers the same way as one whose
// capture has aged out: there is nothing to read either way.
func TestGetCaptureWithNoStoreIsNotFound(t *testing.T) {
	s := newTestServer(t)
	seed(t, s)

	w := do(t, s, http.MethodGet, "/api/events/"+s.events.Events()[0].ID+"/capture", "")

	wantStatus(t, w, http.StatusNotFound)
}

func TestClearingEventsClearsTheirCaptures(t *testing.T) {
	s := newTestServer(t)
	caps, id := withCaptures(t, s)

	wantStatus(t, do(t, s, http.MethodDelete, "/api/events", ""), http.StatusNoContent)

	if _, ok := caps.Get(id); ok {
		t.Error("a capture survived DELETE /api/events, want it cleared with the events")
	}
}

// An encrypted event was never captured, so its capture is simply absent; the
// tier on the event is what tells the UI to explain why.
func TestAnEncryptedEventHasNoCapture(t *testing.T) {
	s := newTestServer(t)
	withCaptures(t, s)

	var encrypted string
	for _, e := range s.events.Events() {
		if e.Tier == events.TierEncrypted {
			encrypted = e.ID
		}
	}
	if encrypted == "" {
		t.Fatal("the seed recorded no encrypted event")
	}

	wantStatus(t, do(t, s, http.MethodGet, "/api/events/"+encrypted+"/capture", ""), http.StatusNotFound)
}
