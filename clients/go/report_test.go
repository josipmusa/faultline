package client_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/events"
)

func TestReportCountsTheSession(t *testing.T) {
	h := newHarness(t)

	now := time.Now()
	h.events.Record(events.Event{ID: "1", Timestamp: now, Host: "httpbin.org", Method: "GET", Path: "/get",
		Status: 503, Faulted: true, RuleID: "down", Tier: events.TierPlain})
	h.events.Record(events.Event{ID: "2", Timestamp: now.Add(time.Second), Host: "httpbin.org", Method: "GET", Path: "/get",
		Status: 200, Tier: events.TierPlain})

	got, err := h.client.Report(context.Background())
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if got.Total != 2 || got.Faulted != 1 || got.Retries != 1 {
		t.Errorf("Report() = %+v, want 2 total, 1 faulted, 1 retry", got)
	}
	if got.MaxRetryWaitMS <= 0 {
		t.Errorf("Report() max retry wait = %d, want the second call's wait", got.MaxRetryWaitMS)
	}
}

// The report is where an agent concludes the application coped. When a rule it
// set up could never fire, that conclusion is wrong, and the warning is what
// tells it so.
func TestReportCarriesTheWarningsForRulesThatCannotFire(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.events.Record(events.Event{ID: events.NextID(), Timestamp: time.Now(),
		Host: "api.stripe.com", Method: "CONNECT", Tier: events.TierEncrypted})

	if _, err := h.client.AddRule(ctx, statusRule("Stripe is down", "api.stripe.com", 503)); err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	got, err := h.client.Report(ctx)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "stripe-is-down") {
		t.Fatalf("warnings = %v, want one naming the rule", got.Warnings)
	}
	if got.Total != 1 {
		t.Errorf("total = %d, want the counts still there", got.Total)
	}
}
