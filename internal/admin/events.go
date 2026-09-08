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
)

// Upstream is one distinct host Faultline has seen, with how much of it it
// could see and how much traffic went through it.
type Upstream struct {
	Host     string      `json:"host"`
	Tier     events.Tier `json:"tier"`
	Requests int         `json:"requests"`
	Faulted  int         `json:"faulted"`
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
	s.writeJSON(w, http.StatusOK, upstreamsOf(s.events.Events()))
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

// upstreamsOf folds the recorded events into one row per host. The tier is the
// one from the most recent event, so a host moves from encrypted to intercepted
// as soon as interception starts working for it.
func upstreamsOf(all []events.Event) []Upstream {
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

	out := make([]Upstream, 0, len(byHost))
	for _, u := range byHost {
		out = append(out, *u)
	}
	slices.SortFunc(out, func(a, b Upstream) int { return cmp.Compare(a.Host, b.Host) })
	return out
}
