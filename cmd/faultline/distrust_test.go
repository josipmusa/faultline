package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/proxy/forward"
)

func rejected(host string) events.Event {
	return events.Event{
		ID: events.NextID(), Host: host, Method: "CONNECT",
		Tier: events.TierIntercepted, Error: forward.ErrClientRejectedCertificate,
	}
}

// A child that does not trust the CA fails every call, so the notice has to
// reach the terminal during the run, once per host: the failure repeats with
// every request and the advice does not change.
func TestWatchDistrustSaysSoOncePerHost(t *testing.T) {
	rec := events.NewRecorder(events.DefaultSize)
	t.Cleanup(rec.Close)
	var out bytes.Buffer

	stop := watchDistrust(rec, &out, []string{"SSL_CERT_FILE"})
	rec.Record(rejected("httpbin.org"))
	rec.Record(rejected("httpbin.org"))
	rec.Record(rejected("api.stripe.com"))
	rec.Record(events.Event{ID: events.NextID(), Host: "example.com", Status: 200, Tier: events.TierIntercepted})
	stop()

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output = %q, want one line per distrusting host", out.String())
	}
	if !strings.HasPrefix(lines[0], "trust: httpbin.org: ") || !strings.HasPrefix(lines[1], "trust: api.stripe.com: ") {
		t.Errorf("lines = %q, want them in the order the hosts failed", lines)
	}
	if !strings.Contains(lines[0], "docs/trust.md") || !strings.Contains(lines[0], "SSL_CERT_FILE") {
		t.Errorf("line = %q, want the hint and the variables that were set", lines[0])
	}
}

func TestWatchDistrustSaysNothingWhenEveryHandshakeWorks(t *testing.T) {
	rec := events.NewRecorder(events.DefaultSize)
	t.Cleanup(rec.Close)
	var out bytes.Buffer

	stop := watchDistrust(rec, &out, nil)
	rec.Record(events.Event{ID: events.NextID(), Host: "httpbin.org", Status: 200, Tier: events.TierIntercepted})
	stop()

	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing", out.String())
	}
}
