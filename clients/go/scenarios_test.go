package client_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/rules"
)

func TestScenariosListAndSwitch(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	slow := delayRule("Httpbin is slow", "httpbin.org", 2000)
	slow.ID = "slow"
	h.rules.Replace([]rules.Rule{slow})
	h.scenarios.Replace([]rules.Scenario{{Name: "api-slow", Rules: []string{"slow"}}})

	list, err := h.client.Scenarios(ctx)
	if err != nil {
		t.Fatalf("Scenarios: %v", err)
	}
	if len(list) != 1 || list[0].Name != "api-slow" || list[0].Active {
		t.Fatalf("Scenarios() = %+v, want one inactive api-slow", list)
	}

	on, err := h.client.SetScenarioActive(ctx, "api-slow", true)
	if err != nil {
		t.Fatalf("SetScenarioActive(true): %v", err)
	}
	if !on.Active {
		t.Error("scenario is not active after activate")
	}

	off, err := h.client.SetScenarioActive(ctx, "api-slow", false)
	if err != nil {
		t.Fatalf("SetScenarioActive(false): %v", err)
	}
	if off.Active {
		t.Error("scenario is still active after deactivate")
	}
}

func TestAnUnknownScenarioIsA404(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.SetScenarioActive(context.Background(), "ghost", true)

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("SetScenarioActive(ghost) error = %v, want a 404 APIError", err)
	}
}
