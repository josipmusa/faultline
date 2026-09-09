package admin

import (
	"cmp"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/proxy/forward"
)

// Upstream is one distinct host Faultline has seen, with how much of it it
// could see and how much traffic went through it. A bypassed host was passed
// through on purpose: nothing was recorded for it, so it has no tier and its
// requests are the ones the forward proxy let by.
type Upstream struct {
	Host     string      `json:"host"`
	Tier     events.Tier `json:"tier,omitempty"`
	Requests int         `json:"requests"`
	Faulted  int         `json:"faulted"`
	Bypassed bool        `json:"bypassed"`
	LastSeen time.Time   `json:"last_seen"`
}

type eventQuery struct {
	limit   int
	host    string
	faulted *bool
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	q, err := parseEventQuery(r.URL.Query())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, q.apply(s.events.Events()))
}

func (s *Server) clearEvents(w http.ResponseWriter, _ *http.Request) {
	s.events.Clear()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listUpstreams(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, upstreamsOf(s.events.Events(), s.bypass.Seen()))
}

func parseEventQuery(v url.Values) (eventQuery, error) {
	q := eventQuery{host: v.Get("host")}

	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return eventQuery{}, invalid("limit", "limit %q is not a positive whole number", raw)
		}
		q.limit = n
	}

	if raw := v.Get("faulted"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return eventQuery{}, invalid("faulted", "faulted %q is not true or false", raw)
		}
		q.faulted = &b
	}

	return q, nil
}

// apply keeps the events the query asked for, oldest first, and trims to the
// most recent limit of them.
func (q eventQuery) apply(all []events.Event) []events.Event {
	out := make([]events.Event, 0, len(all))
	for _, e := range all {
		if q.host != "" && !strings.EqualFold(q.host, e.Host) {
			continue
		}
		if q.faulted != nil && *q.faulted != e.Faulted {
			continue
		}
		out = append(out, e)
	}

	if q.limit > 0 && len(out) > q.limit {
		out = out[len(out)-q.limit:]
	}
	return out
}

// upstreamsOf folds the recorded events into one row per host, then adds a
// row for every bypassed host the forward proxy has seen. The tier is the one
// from the most recent event, so a host moves from encrypted to intercepted as
// soon as interception starts working for it. A bypassed host has no events,
// so the two sets never overlap.
func upstreamsOf(all []events.Event, bypassed []forward.Bypassed) []Upstream {
	byHost := make(map[string]*Upstream)

	for _, e := range all {
		u, ok := byHost[e.Host]
		if !ok {
			u = &Upstream{Host: e.Host}
			byHost[e.Host] = u
		}
		u.Requests++
		if e.Faulted {
			u.Faulted++
		}
		if !e.Timestamp.Before(u.LastSeen) {
			u.LastSeen, u.Tier = e.Timestamp, e.Tier
		}
	}

	out := make([]Upstream, 0, len(byHost)+len(bypassed))
	for _, u := range byHost {
		out = append(out, *u)
	}
	for _, b := range bypassed {
		out = append(out, Upstream{Host: b.Host, Requests: b.Requests, Bypassed: true, LastSeen: b.LastSeen})
	}
	slices.SortFunc(out, func(a, b Upstream) int { return cmp.Compare(a.Host, b.Host) })
	return out
}
