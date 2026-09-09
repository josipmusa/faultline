package client_test

import (
	"context"
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
