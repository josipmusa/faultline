// Package faults applies fault rules to traffic on its way to an upstream.
//
// The pipeline is an http.RoundTripper that wraps another one: it finds the
// rule matching a request, applies that rule's fault, and records an event
// whether or not anything was faulted. Faults act on real traffic. When no rule
// matches, or the upstream is unreachable, the real result is what comes back.
package faults

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/rules"
)

// Transport applies fault rules and records what happened. Its zero value is
// not usable; build one with New.
type Transport struct {
	base   http.RoundTripper
	rules  *rules.Store
	events *events.Recorder
	tier   events.Tier
}

// New wraps base with the fault pipeline. The tier says how much of the traffic
// this transport can see, and is stamped on every event it records, so a fault
// that could not apply can later explain itself. A nil base means
// http.DefaultTransport; a nil store means nothing ever matches; a nil recorder
// means nothing is recorded.
func New(base http.RoundTripper, store *rules.Store, rec *events.Recorder, tier events.Tier) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}
	if tier == "" {
		tier = events.TierPlain
	}
	return &Transport{base: base, rules: store, events: rec, tier: tier}
}

// RoundTrip applies the first matching rule and forwards the request unless a
// fault answered it outright.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	host := hostOf(req)

	e := events.Event{
		ID:      events.NextID(),
		Host:    host,
		Method:  req.Method,
		Path:    req.URL.Path,
		Tier:    t.tier,
		BytesIn: knownLength(req.ContentLength),
	}

	if rule, ok := t.match(host, req); ok {
		switch rule.Fault.Type {
		case rules.FaultDelay:
			e.Faulted, e.RuleID = true, rule.ID
			if err := applyDelay(req.Context(), rule.Fault); err != nil {
				t.record(e, start) // no status: the request never got one
				return nil, err
			}
		case rules.FaultStatus:
			e.Faulted, e.RuleID = true, rule.ID
			resp := syntheticResponse(req, rule)
			e.Status, e.BytesOut = resp.StatusCode, resp.ContentLength
			t.record(e, start)
			return resp, nil
		case rules.FaultRefuse:
			e.Faulted, e.RuleID = true, rule.ID
			resp := refusedResponse(req, host, rule.ID)
			e.Status, e.BytesOut = resp.StatusCode, resp.ContentLength
			t.record(e, start)
			return resp, nil
		}
		// An unknown fault type applies nothing, so the event reports no fault.
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.record(e, start)
		return nil, fmt.Errorf("faultline: upstream %s: %w", host, err)
	}

	e.Status, e.BytesOut = resp.StatusCode, knownLength(resp.ContentLength)
	t.record(e, start)
	return resp, nil
}

// match returns the first enabled rule that applies to the request. Store order
// is rule precedence.
func (t *Transport) match(host string, req *http.Request) (rules.Rule, bool) {
	if t.rules == nil {
		return rules.Rule{}, false
	}
	for _, r := range t.rules.List() {
		if r.Matches(host, req.Method, req.URL.Path, req.Header) {
			return r, true
		}
	}
	return rules.Rule{}, false
}

// record timestamps the event and hands it to the recorder. The duration covers
// everything Faultline did, injected delay included, but stops at the response
// headers: streaming a body afterwards is not counted.
func (t *Transport) record(e events.Event, start time.Time) {
	if t.events == nil {
		return
	}
	e.Timestamp = time.Now()
	e.DurationMS = time.Since(start).Milliseconds()
	t.events.Record(e)
}

// hostOf derives the upstream host that rules match against. Default ports are
// dropped so one rule for `api.stripe.com` covers both schemes, and any other
// port is kept so a rule can target `localhost:8080`. Requests arriving at an
// explicit route have no host in the URL, so the Host header stands in.
func hostOf(req *http.Request) string {
	host := req.URL.Host
	if host == "" {
		host = req.Host
	}
	return StripDefaultPort(host)
}

// StripDefaultPort drops :80 and :443 from a host:port and leaves any other
// port in place, so one rule for `api.stripe.com` covers both schemes and a
// rule can still target `localhost:8080`. The proxies use it too, so every
// event names an upstream the same way.
func StripDefaultPort(host string) string {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		return host // no port to strip
	}
	if port == "80" || port == "443" {
		return name
	}
	return host
}

// knownLength turns an http.Request or http.Response content length into a byte
// count. A length of -1 means the sender never said, which counts as zero
// rather than as a guess.
func knownLength(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}
