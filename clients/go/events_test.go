package client_test

import (
	"context"
	"testing"
	"time"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/events"
)

func record(t *testing.T, h harness, id, host string, faulted bool) {
	t.Helper()
	h.events.Record(events.Event{
		ID:        id,
		Timestamp: time.Now(),
		Host:      host,
		Method:    "GET",
		Path:      "/get",
		Status:    200,
		Faulted:   faulted,
		Tier:      events.TierPlain,
	})
}

func TestEventsAreFilteredByTheQuery(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	record(t, h, "1", "httpbin.org", false)
	record(t, h, "2", "httpbin.org", true)
	record(t, h, "3", "example.com", false)

	all, err := h.client.Events(ctx, client.EventQuery{})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("Events() = %d events, want 3", len(all))
	}

	faulted := true
	some, err := h.client.Events(ctx, client.EventQuery{Host: "httpbin.org", Faulted: &faulted})
	if err != nil {
		t.Fatalf("Events(filtered): %v", err)
	}
	if len(some) != 1 || some[0].ID != "2" {
		t.Fatalf("Events(filtered) = %+v, want only event 2", some)
	}

	last, err := h.client.Events(ctx, client.EventQuery{Limit: 1})
	if err != nil {
		t.Fatalf("Events(limit): %v", err)
	}
	if len(last) != 1 || last[0].ID != "3" {
		t.Fatalf("Events(limit 1) = %+v, want the newest event", last)
	}
}

func TestUpstreamsCountWhatWasSeen(t *testing.T) {
	h := newHarness(t)

	record(t, h, "1", "httpbin.org", false)
	record(t, h, "2", "httpbin.org", true)

	list, err := h.client.Upstreams(context.Background())
	if err != nil {
		t.Fatalf("Upstreams: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("Upstreams() = %+v, want one host", list)
	}
	if list[0].Host != "httpbin.org" || list[0].Requests != 2 || list[0].Faulted != 1 {
		t.Errorf("upstream = %+v, want httpbin.org with 2 requests and 1 faulted", list[0])
	}
}
