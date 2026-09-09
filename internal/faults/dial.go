package faults

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/rules"
)

// Dialer opens the upstream side of a CONNECT tunnel. It is the connection-tier
// counterpart of Transport: it sees a host and nothing else, so only rules that
// ask for nothing but a host, and only faults that work on a connection, apply.
// Every dial records an event in the encrypted tier, faulted or not.
type Dialer struct {
	dial   func(ctx context.Context, network, addr string) (net.Conn, error)
	rules  *rules.Store
	events *events.Recorder
}

// RefusedError says a rule turned the connection away before it was dialed.
// The proxy turns it into the answer a client would get from an upstream that
// is not listening, plus the rule id.
type RefusedError struct {
	Host   string
	RuleID string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("connection to %s refused by rule %s", e.Host, e.RuleID)
}

// NewDialer builds a Dialer over the rule store and recorder. A nil store means
// nothing ever matches; a nil recorder means nothing is recorded.
func NewDialer(store *rules.Store, rec *events.Recorder) *Dialer {
	d := &net.Dialer{Timeout: 30 * time.Second}
	return &Dialer{dial: d.DialContext, rules: store, events: rec}
}

// Dial connects to addr, a host:port from a CONNECT request line, after
// applying the first matching connection fault. It returns a *RefusedError when
// a rule refused the connection, the context error when the client gave up
// during a delay, and the dial error when the upstream is unreachable.
func (d *Dialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	start := time.Now()
	host := StripDefaultPort(addr)

	e := events.Event{
		ID:     events.NextID(),
		Host:   host,
		Method: http.MethodConnect,
		Tier:   events.TierEncrypted,
	}

	if rule, ok := d.match(host); ok {
		e.Faulted, e.RuleID = true, rule.ID
		switch rule.Fault.Type {
		case rules.FaultDelay:
			if err := applyDelay(ctx, rule.Fault); err != nil {
				d.record(e, start) // no status: the tunnel never opened
				return nil, err
			}
		case rules.FaultRefuse:
			e.Status = http.StatusBadGateway
			d.record(e, start)
			return nil, &RefusedError{Host: host, RuleID: rule.ID}
		}
	}

	conn, err := d.dial(ctx, "tcp", addr)
	if err != nil {
		d.record(e, start)
		return nil, fmt.Errorf("faultline: upstream %s: %w", host, err)
	}

	e.Status = http.StatusOK
	d.record(e, start)
	return conn, nil
}

// match returns the first enabled rule that both applies to a bare connection
// and carries a fault that can act on one. A response fault on a host-only
// rule is skipped rather than blocking the rules behind it: it needs to see
// the request, and the encrypted tier on the event says why it did not run.
func (d *Dialer) match(host string) (rules.Rule, bool) {
	if d.rules == nil {
		return rules.Rule{}, false
	}
	for _, r := range d.rules.List() {
		if r.MatchesConnection(host) && r.Fault.IsConnection() {
			return r, true
		}
	}
	return rules.Rule{}, false
}

// record timestamps the event and hands it to the recorder. The duration
// covers the delay and the dial; the bytes that flow afterwards are opaque
// and are not counted.
func (d *Dialer) record(e events.Event, start time.Time) {
	if d.events == nil {
		return
	}
	e.Timestamp = time.Now()
	e.DurationMS = time.Since(start).Milliseconds()
	d.events.Record(e)
}
