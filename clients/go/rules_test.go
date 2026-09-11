package client_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/events"
)

func delayRule(name, host string, ms int) client.Rule {
	return client.Rule{
		Name:    name,
		Enabled: true,
		Match:   client.Match{Host: host},
		Fault:   client.Fault{Type: "delay", Params: client.Params{"ms": ms}},
	}
}

func TestRulesRoundTripThroughTheAPI(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	added, err := h.client.AddRule(ctx, delayRule("Httpbin is slow", "httpbin.org", 2000))
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	if added.ID != "httpbin-is-slow" {
		t.Errorf("id = %q, want the slug the API mints", added.ID)
	}
	if got := added.Fault.Params["ms"]; got != float64(2000) {
		t.Errorf("fault ms = %v, want 2000", got)
	}

	list, err := h.client.Rules(ctx)
	if err != nil {
		t.Fatalf("Rules: %v", err)
	}
	if len(list) != 1 || list[0].ID != added.ID {
		t.Fatalf("Rules() = %+v, want the one rule just added", list)
	}
}

func TestSetRuleEnabledFlipsTheFlag(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	added, err := h.client.AddRule(ctx, delayRule("Httpbin is slow", "httpbin.org", 2000))
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	off, err := h.client.SetRuleEnabled(ctx, added.ID, false)
	if err != nil {
		t.Fatalf("SetRuleEnabled(false): %v", err)
	}
	if off.Enabled {
		t.Error("rule is still enabled after disable")
	}

	on, err := h.client.SetRuleEnabled(ctx, added.ID, true)
	if err != nil {
		t.Fatalf("SetRuleEnabled(true): %v", err)
	}
	if !on.Enabled {
		t.Error("rule is still disabled after enable")
	}
}

func TestDeleteRuleRemovesIt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	added, err := h.client.AddRule(ctx, delayRule("Httpbin is slow", "httpbin.org", 2000))
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	if err := h.client.DeleteRule(ctx, added.ID); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}

	list, err := h.client.Rules(ctx)
	if err != nil {
		t.Fatalf("Rules: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("Rules() = %+v, want none left", list)
	}
}

func TestAnUnknownRuleIsA404(t *testing.T) {
	h := newHarness(t)

	err := h.client.DeleteRule(context.Background(), "ghost")

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("DeleteRule(ghost) error = %v, want a 404 APIError", err)
	}
}

func statusRule(name, host string, code int) client.Rule {
	return client.Rule{
		Name:    name,
		Enabled: true,
		Match:   client.Match{Host: host},
		Fault:   client.Fault{Type: "status", Params: client.Params{"code": code}},
	}
}

// A response-tier fault on a host only ever seen encrypted is stored and does
// nothing, and the API says so. The client has to carry that through or the
// CLI and the agent both report an outage that never happened.
func TestRuleWarningsSurviveTheClient(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.events.Record(events.Event{ID: events.NextID(), Host: "api.stripe.com", Method: http.MethodGet, Tier: events.TierEncrypted})

	added, err := h.client.AddRule(ctx, statusRule("Stripe is down", "api.stripe.com", 503))
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	if len(added.Warnings) != 1 || !strings.Contains(added.Warnings[0], "api.stripe.com") {
		t.Fatalf("AddRule warnings = %v, want one naming the host", added.Warnings)
	}

	read, err := h.client.Rule(ctx, added.ID)
	if err != nil {
		t.Fatalf("Rule: %v", err)
	}
	if len(read.Warnings) != 1 {
		t.Errorf("Rule warnings = %v, want one", read.Warnings)
	}

	off, err := h.client.SetRuleEnabled(ctx, added.ID, false)
	if err != nil {
		t.Fatalf("SetRuleEnabled: %v", err)
	}
	if len(off.Warnings) != 1 {
		t.Errorf("SetRuleEnabled warnings = %v, want one", off.Warnings)
	}
}
