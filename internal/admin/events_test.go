package admin

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/proxy/forward"
)

// seed records three events: two plain calls to httpbin, one of them faulted,
// and one encrypted call to stripe.
func seed(t *testing.T, s *Server) {
	t.Helper()
	base := time.Now().Add(-time.Minute)
	for i, e := range []events.Event{
		{Host: "httpbin.org", Method: "GET", Path: "/get", Status: 200, Tier: events.TierPlain},
		{Host: "api.stripe.com", Method: "CONNECT", Status: 200, Tier: events.TierEncrypted},
		{Host: "httpbin.org", Method: "POST", Path: "/post", Status: 503, Faulted: true, RuleID: "boom", Tier: events.TierPlain},
	} {
		e.ID = events.NextID()
		e.Timestamp = base.Add(time.Duration(i) * time.Second)
		s.events.Record(e)
	}
}

func TestListEventsReturnsThemOldestFirst(t *testing.T) {
	s := newTestServer(t)
	seed(t, s)

	w := do(t, s, http.MethodGet, "/api/events", "")

	wantStatus(t, w, http.StatusOK)
	got := decodeBody[[]events.Event](t, w)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	if got[0].Path != "/get" || got[2].Path != "/post" {
		t.Errorf("order = %q then %q, want the oldest first", got[0].Path, got[2].Path)
	}
	if !got[2].Faulted || got[2].RuleID != "boom" {
		t.Errorf("last event = %+v, want it attributed to the rule", got[2])
	}
}

func TestListEventsFilters(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"everything", "", 3},
		{"by host", "?host=httpbin.org", 2},
		{"host ignores case", "?host=HTTPBIN.ORG", 2},
		{"unknown host", "?host=nope.example.com", 0},
		{"faulted only", "?faulted=true", 1},
		{"clean only", "?faulted=false", 2},
		{"limit keeps the most recent", "?limit=1", 1},
		{"limit above the count", "?limit=99", 3},
		{"host and faulted together", "?host=httpbin.org&faulted=true", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			seed(t, s)

			w := do(t, s, http.MethodGet, "/api/events"+tt.query, "")

			wantStatus(t, w, http.StatusOK)
			got := decodeBody[[]events.Event](t, w)
			if len(got) != tt.want {
				t.Fatalf("got %d events, want %d: %+v", len(got), tt.want, got)
			}
			if tt.query == "?limit=1" && got[0].Path != "/post" {
				t.Errorf("limit kept %q, want the most recent event", got[0].Path)
			}
		})
	}
}

func TestListEventsRejectsBadQueries(t *testing.T) {
	tests := []struct{ query, field string }{
		{"?limit=lots", "limit"},
		{"?limit=0", "limit"},
		{"?limit=-1", "limit"},
		{"?faulted=maybe", "faulted"},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			s := newTestServer(t)

			wantError(t, do(t, s, http.MethodGet, "/api/events"+tt.query, ""), http.StatusBadRequest, tt.field)
		})
	}
}

func TestClearEvents(t *testing.T) {
	s := newTestServer(t)
	seed(t, s)

	w := do(t, s, http.MethodDelete, "/api/events", "")

	wantStatus(t, w, http.StatusNoContent)
	if got := s.events.Events(); len(got) != 0 {
		t.Errorf("recorder still holds %d events", len(got))
	}
	if got := do(t, s, http.MethodGet, "/api/events", "").Body.String(); got != "[]\n" {
		t.Errorf("list after clear = %q, want an empty JSON array", got)
	}
}

func TestListUpstreams(t *testing.T) {
	s := newTestServer(t)
	seed(t, s)

	w := do(t, s, http.MethodGet, "/api/upstreams", "")

	wantStatus(t, w, http.StatusOK)
	got := decodeBody[[]Upstream](t, w)
	if len(got) != 2 {
		t.Fatalf("got %d upstreams, want 2: %+v", len(got), got)
	}
	if got[0].Host != "api.stripe.com" || got[1].Host != "httpbin.org" {
		t.Fatalf("hosts = %q, %q, want them sorted", got[0].Host, got[1].Host)
	}
	if got[0].Tier != events.TierEncrypted || got[0].Requests != 1 || got[0].Faulted != 0 {
		t.Errorf("stripe = %+v, want one clean encrypted request", got[0])
	}
	if got[1].Tier != events.TierPlain || got[1].Requests != 2 || got[1].Faulted != 1 {
		t.Errorf("httpbin = %+v, want two plain requests, one faulted", got[1])
	}
}

// A host graduates from encrypted to intercepted once interception starts
// working, so the row reports the tier of the most recent event.
func TestListUpstreamsReportsTheLatestTier(t *testing.T) {
	s := newTestServer(t)
	now := time.Now()
	s.events.Record(events.Event{ID: "1", Host: "api.stripe.com", Timestamp: now.Add(-time.Minute), Tier: events.TierEncrypted})
	s.events.Record(events.Event{ID: "2", Host: "api.stripe.com", Timestamp: now, Tier: events.TierIntercepted})

	got := decodeBody[[]Upstream](t, do(t, s, http.MethodGet, "/api/upstreams", ""))

	if len(got) != 1 || got[0].Tier != events.TierIntercepted {
		t.Errorf("upstreams = %+v, want one row at tier intercepted", got)
	}
}

func TestListUpstreamsStartsEmpty(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodGet, "/api/upstreams", "")

	wantStatus(t, w, http.StatusOK)
	if got := w.Body.String(); got != "[]\n" {
		t.Errorf("body = %q, want an empty JSON array", got)
	}
}

// Bypassed hosts never produce events, so the upstreams list is where the
// operator learns that a host was seen and deliberately skipped.
func TestListUpstreamsIncludesBypassedHosts(t *testing.T) {
	bypass, err := forward.NewBypass([]string{"localhost"})
	if err != nil {
		t.Fatalf("NewBypass: %v", err)
	}
	s := newTestServerWith(t, bypass)
	seed(t, s)
	bypass.Saw("localhost:8080")
	bypass.Saw("localhost:8080")

	w := do(t, s, http.MethodGet, "/api/upstreams", "")

	wantStatus(t, w, http.StatusOK)
	got := decodeBody[[]Upstream](t, w)
	if len(got) != 3 {
		t.Fatalf("got %d upstreams, want 3: %+v", len(got), got)
	}
	if got[0].Host != "api.stripe.com" || got[1].Host != "httpbin.org" || got[2].Host != "localhost:8080" {
		t.Fatalf("hosts = %q, %q, %q, want them sorted together", got[0].Host, got[1].Host, got[2].Host)
	}
	if got[0].Bypassed || got[1].Bypassed {
		t.Errorf("recorded upstreams are marked bypassed: %+v", got[:2])
	}
	local := got[2]
	if !local.Bypassed || local.Requests != 2 || local.Faulted != 0 || local.Tier != "" || local.LastSeen.IsZero() {
		t.Errorf("localhost = %+v, want bypassed, two requests, no tier, a last_seen", local)
	}
	if !strings.Contains(w.Body.String(), `"bypassed":false`) || strings.Contains(w.Body.String(), `"tier":""`) {
		t.Errorf("body = %s, want bypassed always present and an empty tier omitted", w.Body.String())
	}
}
