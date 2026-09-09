package client_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	client "github.com/josipmusa/faultline/clients/go"
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
