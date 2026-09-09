package admin

import (
	"net/http"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/events"
)

// seen records one request against a host, so a rule written afterwards knows
// how much of that host Faultline has been able to see.
func seen(t *testing.T, s *Server, host string, tier events.Tier) {
	t.Helper()
	s.events.Record(events.Event{ID: events.NextID(), Host: host, Method: http.MethodGet, Tier: tier})
}

const statusRuleFor = `{"name":"down","match":{"host":"api.stripe.com"},"fault":{"type":"status","code":503}}`

func TestCreateWarnsAboutAResponseFaultOnAnEncryptedHost(t *testing.T) {
	s := newTestServer(t)
	seen(t, s, "api.stripe.com", events.TierEncrypted)

	w := do(t, s, http.MethodPost, "/api/rules", statusRuleFor)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: an encrypted host is a warning, not an error", w.Code, http.StatusCreated)
	}
	got := decodeBody[ruleResponse](t, w)
	if len(got.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one", got.Warnings)
	}
	if !strings.Contains(got.Warnings[0], "api.stripe.com") {
		t.Errorf("warning %q does not name the host", got.Warnings[0])
	}
	if got.ID == "" || got.Fault.Type != "status" {
		t.Errorf("the rule itself is missing from the response: %+v", got)
	}
}

func TestNoWarningOnceTheHostIsIntercepted(t *testing.T) {
	s := newTestServer(t)
	seen(t, s, "api.stripe.com", events.TierEncrypted)
	seen(t, s, "api.stripe.com", events.TierIntercepted)

	w := do(t, s, http.MethodPost, "/api/rules", statusRuleFor)

	if got := decodeBody[ruleResponse](t, w).Warnings; len(got) != 0 {
		t.Errorf("warnings = %v, want none: the host has been intercepted", got)
	}
}

func TestNoWarningForAnUnseenHost(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodPost, "/api/rules", statusRuleFor)

	if got := decodeBody[ruleResponse](t, w).Warnings; len(got) != 0 {
		t.Errorf("warnings = %v, want none: nothing has been seen of that host yet", got)
	}
}

func TestNoWarningForAConnectionFault(t *testing.T) {
	s := newTestServer(t)
	seen(t, s, "api.stripe.com", events.TierEncrypted)

	body := `{"name":"slow","match":{"host":"api.stripe.com"},"fault":{"type":"delay","ms":10}}`
	w := do(t, s, http.MethodPost, "/api/rules", body)

	if got := decodeBody[ruleResponse](t, w).Warnings; len(got) != 0 {
		t.Errorf("warnings = %v, want none: a delay applies to encrypted traffic too", got)
	}
}

func TestNoWarningForARuleWithoutAHost(t *testing.T) {
	s := newTestServer(t)
	seen(t, s, "api.stripe.com", events.TierEncrypted)

	body := `{"name":"down","fault":{"type":"status","code":503}}`
	w := do(t, s, http.MethodPost, "/api/rules", body)

	if got := decodeBody[ruleResponse](t, w).Warnings; len(got) != 0 {
		t.Errorf("warnings = %v, want none: the rule names no host to warn about", got)
	}
}

func TestReadingAndTogglingARuleCarryTheWarning(t *testing.T) {
	s := newTestServer(t)
	seen(t, s, "api.stripe.com", events.TierEncrypted)
	id := decodeBody[ruleResponse](t, do(t, s, http.MethodPost, "/api/rules", statusRuleFor)).ID

	for _, target := range []struct{ method, path string }{
		{http.MethodGet, "/api/rules/" + id},
		{http.MethodPut, "/api/rules/" + id},
		{http.MethodPost, "/api/rules/" + id + "/disable"},
		{http.MethodPost, "/api/rules/" + id + "/enable"},
	} {
		body := ""
		if target.method == http.MethodPut {
			body = statusRuleFor
		}
		w := do(t, s, target.method, target.path, body)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: status = %d", target.method, target.path, w.Code)
		}
		if got := decodeBody[ruleResponse](t, w).Warnings; len(got) != 1 {
			t.Errorf("%s %s: warnings = %v, want one", target.method, target.path, got)
		}
	}
}

func TestListingRulesIsUnchanged(t *testing.T) {
	// The list is the rules themselves. A warning is about one rule the caller
	// just wrote or read, not a property of the collection.
	s := newTestServer(t)
	seen(t, s, "api.stripe.com", events.TierEncrypted)
	do(t, s, http.MethodPost, "/api/rules", statusRuleFor)

	got := decodeBody[[]map[string]any](t, do(t, s, http.MethodGet, "/api/rules", ""))
	if len(got) != 1 {
		t.Fatalf("listed %d rules, want 1", len(got))
	}
	if _, ok := got[0]["warnings"]; ok {
		t.Errorf("the rule list carries warnings: %v", got[0])
	}
}
