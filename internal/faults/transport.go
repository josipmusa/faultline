// Package faults applies fault rules to traffic on its way to an upstream.
//
// The pipeline is an http.RoundTripper that wraps another one: it finds the
// rule matching a request, applies that rule's fault, and records an event
// whether or not anything was faulted. Faults act on real traffic. When no rule
// matches, or the upstream is unreachable, the real result is what comes back.
package faults

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/josipmusa/faultline/internal/capture"
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
	gate   *Gate

	// captures holds the headers and bodies of recent exchanges, or is nil
	// when nothing is capturing them. Set with CaptureTo.
	captures *capture.Store
}

// New wraps base with the fault pipeline. The tier says how much of the traffic
// this transport can see, and is stamped on every event it records, so a fault
// that could not apply can later explain itself. A nil base means
// http.DefaultTransport; a nil store means nothing ever matches; a nil recorder
// means nothing is recorded. The gate holds the state stateful behaviors keep;
// pass the same one to everything reading the same store, or nil for a
// transport that keeps behavior state of its own.
func New(base http.RoundTripper, store *rules.Store, rec *events.Recorder, tier events.Tier, gate *Gate) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}
	if tier == "" {
		tier = events.TierPlain
	}
	if gate == nil {
		gate = NewGate()
	}
	return &Transport{base: base, rules: store, events: rec, tier: tier, gate: gate}
}

// RoundTrip applies the first rule that matches the request and whose behavior
// accepts it, and forwards the request unless the fault answered it outright.
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

	next := t.upstream(host)
	// The capture is filed before the event is recorded, so a client that
	// sees an event on the stream can always fetch the capture behind it.
	sent, tee := t.teeRequest(req)

	if rule, applier, ok := t.match(host, req); ok {
		e.Faulted, e.RuleID = true, rule.ID

		resp, err := applier.Respond(rule.ID, sent, next)
		if err != nil {
			t.file(e.ID, req, tee, nil)
			t.record(e, start) // no status: the request never got one
			return nil, err
		}
		e.Status, e.BytesOut = resp.StatusCode, knownLength(resp.ContentLength)
		t.file(e.ID, req, tee, resp)
		t.record(e, start)
		return resp, nil
	}

	resp, err := next.RoundTrip(sent)
	if err != nil {
		t.file(e.ID, req, tee, nil)
		t.record(e, start)
		return nil, err
	}

	e.Status, e.BytesOut = resp.StatusCode, knownLength(resp.ContentLength)
	t.file(e.ID, req, tee, resp)
	t.record(e, start)
	return resp, nil
}

// upstream is the real round trip, as the faults see it: a fault calls it to
// send the request on, and a fault that answers by itself never does. An
// upstream failure is named here so a fault's own error, such as a cancelled
// delay, travels on unwrapped.
func (t *Transport) upstream(host string) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := t.base.RoundTrip(req)
		if err != nil {
			return nil, fmt.Errorf("faultline: upstream %s: %w", host, err)
		}
		return resp, nil
	})
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// match returns the first enabled rule that applies to the request, with its
// fault ready to run. Store order is rule precedence, and a rule decides a
// request only when it matches, builds, and its behavior accepts: a rule whose
// behavior declines has counted the request but hands it to the rules behind
// it, so a spent first_n or a percent miss never shadows the rule after it. A
// rule the API accepted always builds; one that does not can only have come
// from elsewhere, so it is reported and then skipped rather than failing the
// request.
func (t *Transport) match(host string, req *http.Request) (rules.Rule, Applier, bool) {
	if t.rules == nil {
		return rules.Rule{}, nil, false
	}
	for _, r := range t.rules.List() {
		if !r.Matches(host, req.Method, req.URL.Path, req.Header) {
			continue
		}
		applier, err := Build(r.Fault)
		if err != nil {
			slog.Default().Warn("rule applies no fault", "rule", r.ID, "err", err)
			continue
		}
		if t.gate.Applies(r) {
			return r, applier, true
		}
	}
	return rules.Rule{}, nil, false
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
