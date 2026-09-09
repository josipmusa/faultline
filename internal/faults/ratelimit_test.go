package faults

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

func buildRateLimit(t *testing.T, params rules.Params) Applier {
	t.Helper()
	applier, err := Build(rules.Fault{Type: "rate_limit", Params: params})
	if err != nil {
		t.Fatalf("building the rate_limit fault: %v", err)
	}
	return applier
}

func TestRateLimitAnswersWithTooManyRequestsAndARetryHint(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://api.stripe.com/v1/charges", nil)

	called := false
	next := roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil //nolint:nilnil // the upstream must not be called
	})

	resp, err := buildRateLimit(t, rules.Params{"retry_after_sec": 30}).Respond("throttled", req, next)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if called {
		t.Error("the upstream was called")
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}
	if got := resp.Header.Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want %q", got, "30")
	}
	if got := resp.Header.Get(FaultHeader); got != "throttled" {
		t.Errorf("%s = %q, want the rule id", FaultHeader, got)
	}
}

func TestRateLimitLeavesNoBodyBehind(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://api.stripe.com/v1/charges", nil)

	resp, err := buildRateLimit(t, rules.Params{"retry_after_sec": 0}).
		Respond("throttled", req, http.DefaultTransport)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("body = %q, want it empty", body)
	}
	if got := resp.Header.Get("Retry-After"); got != "0" {
		t.Errorf("Retry-After = %q, want %q: retry now is still a hint", got, "0")
	}
}
