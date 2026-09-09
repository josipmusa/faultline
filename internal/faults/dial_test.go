package faults

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/rules"
)

// tcpUpstream listens and accepts connections without speaking any protocol:
// the tunnel side of Faultline never looks at the bytes.
func tcpUpstream(t *testing.T) (addr string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return ln.Addr().String()
}

// closedPort returns an address nothing listens on.
func closedPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func withHost(r rules.Rule, host string) rules.Rule {
	r.Match.Host = host
	return r
}

func TestDialWithNoRuleConnectsAndRecordsAnEncryptedEvent(t *testing.T) {
	addr := tcpUpstream(t)
	rec := events.NewRecorder(10)
	d := NewDialer(rules.New(), rec, nil)

	conn, err := d.Dial(context.Background(), addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = conn.Close()

	e := onlyEvent(t, rec)
	if e.Tier != events.TierEncrypted {
		t.Errorf("tier = %q, want %q", e.Tier, events.TierEncrypted)
	}
	if e.Method != http.MethodConnect {
		t.Errorf("method = %q, want CONNECT", e.Method)
	}
	if e.Path != "" {
		t.Errorf("path = %q, want none: nothing past the host is visible", e.Path)
	}
	if e.Host != addr {
		t.Errorf("host = %q, want %q (a non-default port is kept)", e.Host, addr)
	}
	if e.Status != http.StatusOK {
		t.Errorf("status = %d, want 200 for an established tunnel", e.Status)
	}
	if e.Faulted || e.RuleID != "" {
		t.Errorf("event reports a fault: %+v", e)
	}
}

func TestDialRefusesWhenARuleSaysSo(t *testing.T) {
	rec := events.NewRecorder(10)
	// No upstream exists for this name; a refused connection is never dialed,
	// and the default port is dropped so the rule matches the bare host.
	d := NewDialer(storeWith(t, withHost(refuseRule("stripe-down"), "api.stripe.invalid")), rec, nil)

	conn, err := d.Dial(context.Background(), "api.stripe.invalid:443")
	if conn != nil {
		_ = conn.Close()
		t.Fatal("Dial returned a connection for a refused host")
	}
	var refused *RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want a *RefusedError", err)
	}
	if refused.RuleID != "stripe-down" || refused.Host != "api.stripe.invalid" {
		t.Errorf("RefusedError = %+v, want the rule id and the bare host", refused)
	}

	e := onlyEvent(t, rec)
	if !e.Faulted || e.RuleID != "stripe-down" {
		t.Errorf("event does not report the refuse rule: %+v", e)
	}
	if e.Host != "api.stripe.invalid" {
		t.Errorf("host = %q, want the default port stripped", e.Host)
	}
	if e.Status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502, what the client is told", e.Status)
	}
	if e.Tier != events.TierEncrypted || e.Method != http.MethodConnect {
		t.Errorf("tier/method = %s/%s, want encrypted/CONNECT", e.Tier, e.Method)
	}
}

func TestDialHoldsTheConnectionForADelayRule(t *testing.T) {
	addr := tcpUpstream(t)
	rec := events.NewRecorder(10)
	d := NewDialer(storeWith(t, delayRule("slow", 200)), rec, nil)

	start := time.Now()
	conn, err := d.Dial(context.Background(), addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = conn.Close()

	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("Dial took %v, want at least the 200ms delay", elapsed)
	}
	e := onlyEvent(t, rec)
	if !e.Faulted || e.RuleID != "slow" {
		t.Errorf("event does not report the delay rule: %+v", e)
	}
	if e.Status != http.StatusOK {
		t.Errorf("status = %d, want 200: a delayed tunnel still opens", e.Status)
	}
	if e.DurationMS < 200 {
		t.Errorf("duration = %dms, want it to include the delay", e.DurationMS)
	}
}

func TestDialGivesUpTheDelayWhenTheClientGoesAway(t *testing.T) {
	addr := tcpUpstream(t)
	rec := events.NewRecorder(10)
	d := NewDialer(storeWith(t, delayRule("slow", 10_000)), rec, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	conn, err := d.Dial(ctx, addr)
	if conn != nil {
		_ = conn.Close()
		t.Fatal("Dial connected after the client gave up")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the context error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Dial took %v, want it to stop with the context", elapsed)
	}
	if e := onlyEvent(t, rec); e.Status != 0 {
		t.Errorf("status = %d, want 0: the tunnel never opened", e.Status)
	}
}

func TestDialIgnoresRulesThatCannotBeSeenOnAConnection(t *testing.T) {
	addr := tcpUpstream(t)
	rec := events.NewRecorder(10)

	pathRefuse := refuseRule("path-rule")
	pathRefuse.Match.Path = "/**"
	methodDelay := delayRule("method-rule", 5_000)
	methodDelay.Match.Method = "GET"

	d := NewDialer(storeWith(t, pathRefuse, methodDelay), rec, nil)

	start := time.Now()
	conn, err := d.Dial(context.Background(), addr)
	if err != nil {
		t.Fatalf("Dial: %v, want a plain connection: neither rule is host-only", err)
	}
	_ = conn.Close()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Dial took %v, want no delay from a rule that needs a method", elapsed)
	}
	if e := onlyEvent(t, rec); e.Faulted {
		t.Errorf("event reports a fault: %+v", e)
	}
}

func TestDialSkipsAResponseFaultAndTakesTheNextConnectionFault(t *testing.T) {
	rec := events.NewRecorder(10)
	// A status fault needs to see the request, which a tunnel hides. It steps
	// aside rather than blocking the refuse rule behind it.
	d := NewDialer(storeWith(t, statusRule("status-first", 503, ""), refuseRule("refuse-second")), rec, nil)

	conn, err := d.Dial(context.Background(), "api.stripe.invalid:443")
	if conn != nil {
		_ = conn.Close()
	}
	var refused *RefusedError
	if !errors.As(err, &refused) || refused.RuleID != "refuse-second" {
		t.Fatalf("err = %v, want refusal by refuse-second", err)
	}
}

func TestDialReportsAnUnreachableUpstream(t *testing.T) {
	rec := events.NewRecorder(10)
	d := NewDialer(rules.New(), rec, nil)

	conn, err := d.Dial(context.Background(), closedPort(t))
	if conn != nil {
		_ = conn.Close()
		t.Fatal("Dial returned a connection to a closed port")
	}
	if err == nil {
		t.Fatal("Dial returned no error for a closed port")
	}
	var refused *RefusedError
	if errors.As(err, &refused) {
		t.Errorf("err = %v is a RefusedError, but no rule refused anything", err)
	}
	if e := onlyEvent(t, rec); e.Status != 0 || e.Faulted {
		t.Errorf("event = %+v, want no status and no fault for a real failure", e)
	}
}
