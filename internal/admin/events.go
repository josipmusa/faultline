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
//
// Being on the bypass list and having been bypassed are two different things,
// and the row carries both. The list belongs to the forward proxy, and an
// explicit route does not consult it, so a routed upstream that matches the
// list is proxied, recorded and faultable all the same.
type Upstream struct {
	Host     string      `json:"host"`
	Tier     events.Tier `json:"tier,omitempty"`
	Requests int         `json:"requests"`
	Faulted  int         `json:"faulted"`

	// Errors counts the requests that went wrong rather than the ones a rule
	// broke on purpose: a request that never got a status, and one answered
	// 5xx. A 4xx is the upstream answering and is not counted, or an API that
	// deals in 404s would read as broken.
	Errors int `json:"errors"`

	// Bypassed says this host's requests were passed through untouched, which
	// is why nothing was recorded for them.
	Bypassed bool `json:"bypassed"`
	// BypassEntry is the entry on the bypass list that covers this host, empty
	// when none does. It is often not the host: entries are patterns, so a
	// portless entry covers every port and a wildcard every subdomain, and a
	// caller that wants the host proxied again has to know which entry to take
	// off the list. Whether any of the host's traffic reaches the forward
	// proxy at all is a different question, which Bypassed and the counts
	// answer.
	BypassEntry string `json:"bypass_entry,omitempty"`

	LastSeen time.Time `json:"last_seen"`

	// Hint is advice for something wrong with this host that no status code
	// explains, such as a client that refused the interception certificate.
	// Empty when there is nothing to say.
	Hint string `json:"hint,omitempty"`
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
	if s.captures != nil {
		s.captures.Clear()
	}
	w.WriteHeader(http.StatusNoContent)
}

// sessionReport answers with how the application behaved under the faults it
// has been given so far.
func (s *Server) sessionReport(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, reportResponse{Report: s.events.Report(), Warnings: s.reportWarnings()})
}

func (s *Server) listUpstreams(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, upstreamsOf(s.events.Events(), s.bypass, s.trust))
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
// soon as interception starts working for it.
//
// A host bypassed from the start has no events of its own, but one bypassed
// later has the events from before and the requests passed through since, and
// that is still one host and one row. So a recorded row reads whether it is on
// the list rather than inferring it from which set it came from, and requests
// passed through are folded into the row the events made: the list is what is
// in force now, the tier and the faulted and error counts are what Faultline
// actually saw, and requests is every call the host received either way.
//
// trustVars are the trust variables Faultline set for a wrapped child, named
// in the hint a host gets when its client rejected the interception
// certificate.
func upstreamsOf(all []events.Event, bypass *forward.Bypass, trustVars []string) []Upstream {
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
		if e.Error != "" || e.Status >= 500 {
			u.Errors++
		}
		if !e.Timestamp.Before(u.LastSeen) {
			u.LastSeen, u.Tier = e.Timestamp, e.Tier
		}
		// Events arrive oldest first, so the last handshake outcome wins: the
		// hint appears when a client rejects the certificate and goes away by
		// itself once requests start coming through the tunnel, which is what
		// trusting the CA mid-run looks like from here.
		switch {
		case forward.Distrusted(e):
			u.Hint = forward.DistrustHint(trustVars)
		case e.Tier == events.TierIntercepted && e.Error == "":
			u.Hint = ""
		}
	}

	// The requests the forward proxy let by are folded in: into the row the
	// events made when there is one, otherwise into a row of their own, which
	// is a host that has only ever been passed through.
	for _, b := range bypass.Seen() {
		u, recorded := byHost[b.Host]
		if !recorded {
			u = &Upstream{Host: b.Host}
			byHost[b.Host] = u
		}
		u.Requests += b.Requests
		u.Bypassed = true
		if b.LastSeen.After(u.LastSeen) {
			u.LastSeen = b.LastSeen
		}
	}

	out := make([]Upstream, 0, len(byHost))
	for _, u := range byHost {
		u.BypassEntry = bypass.Covering(u.Host)
		out = append(out, *u)
	}
	slices.SortFunc(out, func(a, b Upstream) int { return cmp.Compare(a.Host, b.Host) })
	return out
}
