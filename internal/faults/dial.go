package faults

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	gate   *Gate
}

// NewDialer builds a Dialer over the rule store and recorder. A nil store means
// nothing ever matches; a nil recorder means nothing is recorded. The gate is
// the one the transports over the same store use, so a rule counts one dial and
// one request alike; nil gives the dialer behavior state of its own.
func NewDialer(store *rules.Store, rec *events.Recorder, gate *Gate) *Dialer {
	d := &net.Dialer{Timeout: 30 * time.Second}
	if gate == nil {
		gate = NewGate()
	}
	return &Dialer{dial: d.DialContext, rules: store, events: rec, gate: gate}
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

	if rule, applier, ok := d.match(host); ok && d.gate.Applies(rule) {
		e.Faulted, e.RuleID = true, rule.ID

		conn, err := applier.Dial(ctx, rule.ID, addr, d.dial)
		switch {
		case err == nil:
			e.Status = http.StatusOK
			d.record(e, start)
			return conn, nil
		case isRefused(err):
			e.Status = http.StatusBadGateway
			d.record(e, start) // the tunnel was never opened
			return nil, err
		default:
			d.record(e, start)
			return nil, d.dialError(host, err)
		}
	}

	conn, err := d.dial(ctx, "tcp", addr)
	if err != nil {
		d.record(e, start)
		return nil, d.dialError(host, err)
	}

	e.Status = http.StatusOK
	d.record(e, start)
	return conn, nil
}

// match returns the first enabled rule that both applies to a bare connection
// and carries a fault that can act on one. A response fault on a host-only rule
// is skipped rather than blocking the rules behind it: it needs to see the
// request, and the encrypted tier on the event says why it did not run. So is a
// rule whose fault will not build, which is reported and then ignored.
func (d *Dialer) match(host string) (rules.Rule, Tunneler, bool) {
	if d.rules == nil {
		return rules.Rule{}, nil, false
	}
	for _, r := range d.rules.List() {
		if !r.MatchesConnection(host) {
			continue
		}
		applier, err := Build(r.Fault)
		if err != nil {
			slog.Default().Warn("rule applies no fault", "rule", r.ID, "err", err)
			continue
		}
		if tunneler, ok := applier.(Tunneler); ok {
			return r, tunneler, true
		}
	}
	return rules.Rule{}, nil, false
}

// dialError names the upstream a failed dial was for. A fault's own error, such
// as a cancelled delay, is not an upstream failure and is left as it is.
func (d *Dialer) dialError(host string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("faultline: upstream %s: %w", host, err)
}

// isRefused reports whether a rule turned the connection away.
func isRefused(err error) bool {
	var refused *RefusedError
	return errors.As(err, &refused)
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
