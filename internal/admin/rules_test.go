package admin

import (
	"net/http"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

const slowRule = `{"name":"Stripe is slow","match":{"host":"api.stripe.com"},"fault":{"type":"delay","ms":2000}}`

func TestListRulesStartsEmptyAndKeepsInsertionOrder(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodGet, "/api/rules", "")
	wantStatus(t, w, http.StatusOK)
	if got := w.Body.String(); got != "[]\n" {
		t.Errorf("empty list = %q, want an empty JSON array", got)
	}

	for _, name := range []string{"first", "second", "third"} {
		wantStatus(t, do(t, s, http.MethodPost, "/api/rules",
			`{"name":"`+name+`","match":{"host":"a.example.com"},"fault":{"type":"status","code":503}}`),
			http.StatusCreated)
	}

	got := decodeBody[[]rules.Rule](t, do(t, s, http.MethodGet, "/api/rules", ""))
	if len(got) != 3 || got[0].Name != "first" || got[2].Name != "third" {
		t.Fatalf("list = %+v, want first, second, third", got)
	}
}

func TestCreateRuleMintsAnIDAndEnablesTheRule(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodPost, "/api/rules", slowRule)

	wantStatus(t, w, http.StatusCreated)
	got := decodeBody[rules.Rule](t, w)
	if got.ID != "stripe-is-slow" {
		t.Errorf("id = %q, want it derived from the name", got.ID)
	}
	if !got.Enabled {
		t.Error("a rule posted without an enabled field should start enabled")
	}
	if got.Fault.MS != 2000 || got.Match.Host != "api.stripe.com" {
		t.Errorf("rule = %+v, want the posted match and fault", got)
	}
	if _, err := s.rules.Get("stripe-is-slow"); err != nil {
		t.Errorf("the rule did not reach the store: %v", err)
	}
}

func TestCreateRuleHonoursAnExplicitEnabledFalse(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodPost, "/api/rules",
		`{"name":"off","enabled":false,"match":{"host":"a.example.com"},"fault":{"type":"delay","ms":10}}`)

	wantStatus(t, w, http.StatusCreated)
	if decodeBody[rules.Rule](t, w).Enabled {
		t.Error("enabled false was ignored")
	}
}

func TestCreateRuleKeepsAnExplicitIDAndRejectsADuplicate(t *testing.T) {
	s := newTestServer(t)
	body := `{"id":"slow-stripe","name":"Stripe is slow","match":{"host":"api.stripe.com"},"fault":{"type":"delay","ms":10}}`

	w := do(t, s, http.MethodPost, "/api/rules", body)
	wantStatus(t, w, http.StatusCreated)
	if got := decodeBody[rules.Rule](t, w).ID; got != "slow-stripe" {
		t.Errorf("id = %q, want the one that was posted", got)
	}

	wantError(t, do(t, s, http.MethodPost, "/api/rules", body), http.StatusConflict, "id")
}

func TestCreateRuleGivesGeneratedIDsASuffix(t *testing.T) {
	s := newTestServer(t)

	first := decodeBody[rules.Rule](t, do(t, s, http.MethodPost, "/api/rules", slowRule))
	second := decodeBody[rules.Rule](t, do(t, s, http.MethodPost, "/api/rules", slowRule))
	third := decodeBody[rules.Rule](t, do(t, s, http.MethodPost, "/api/rules", slowRule))

	if first.ID != "stripe-is-slow" || second.ID != "stripe-is-slow-2" || third.ID != "stripe-is-slow-3" {
		t.Errorf("ids = %q, %q, %q, want a numeric suffix on the clashes", first.ID, second.ID, third.ID)
	}
}

func TestCreateRuleAcceptsARefuseFault(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodPost, "/api/rules",
		`{"name":"stripe is down","match":{"host":"api.stripe.com"},"fault":{"type":"refuse"}}`)

	wantStatus(t, w, http.StatusCreated)
	if got := decodeBody[rules.Rule](t, w).Fault.Type; got != rules.FaultRefuse {
		t.Errorf("fault type = %q, want %q", got, rules.FaultRefuse)
	}
}

func TestCreateRuleRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		field string
	}{
		{"empty body", "", ""},
		{"not JSON", "{", ""},
		{"unknown field", `{"name":"x","match":{"host":"a"},"fault":{"type":"delay","ms":1},"behaviour":"first_n"}`, ""},
		{"no name", `{"match":{"host":"a.example.com"},"fault":{"type":"delay","ms":1}}`, "name"},
		{"id with a slash", `{"id":"a/b","name":"x","fault":{"type":"delay","ms":1}}`, "id"},
		{"host is a url", `{"name":"x","match":{"host":"https://a.example.com"},"fault":{"type":"delay","ms":1}}`, "match.host"},
		{"method is not a method", `{"name":"x","match":{"method":"GET /x"},"fault":{"type":"delay","ms":1}}`, "match.method"},
		{"path without a leading slash", `{"name":"x","match":{"path":"orders/*"},"fault":{"type":"delay","ms":1}}`, "match.path"},
		{"empty header name", `{"name":"x","match":{"header":{"":"1"}},"fault":{"type":"delay","ms":1}}`, "match.header"},
		{"no fault type", `{"name":"x","match":{"host":"a.example.com"},"fault":{"ms":1}}`, "fault.type"},
		{"unknown fault type", `{"name":"x","fault":{"type":"explode"}}`, "fault.type"},
		{"delay without ms", `{"name":"x","fault":{"type":"delay"}}`, "fault.ms"},
		{"negative jitter", `{"name":"x","fault":{"type":"delay","ms":10,"jitter_ms":-1}}`, "fault.jitter_ms"},
		{"delay with a status code", `{"name":"x","fault":{"type":"delay","ms":10,"code":503}}`, "fault.code"},
		{"status without a code", `{"name":"x","fault":{"type":"status"}}`, "fault.code"},
		{"status code out of range", `{"name":"x","fault":{"type":"status","code":99}}`, "fault.code"},
		{"status with a delay", `{"name":"x","fault":{"type":"status","code":503,"ms":10}}`, "fault.ms"},
		{"refuse with a delay", `{"name":"x","fault":{"type":"refuse","ms":10}}`, "fault.ms"},
		{"refuse with jitter", `{"name":"x","fault":{"type":"refuse","jitter_ms":10}}`, "fault.jitter_ms"},
		{"refuse with a status code", `{"name":"x","fault":{"type":"refuse","code":503}}`, "fault.code"},
		{"refuse with a body", `{"name":"x","fault":{"type":"refuse","body":"nope"}}`, "fault.body"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)

			wantError(t, do(t, s, http.MethodPost, "/api/rules", tt.body), http.StatusBadRequest, tt.field)

			if got := s.rules.List(); len(got) != 0 {
				t.Errorf("a rejected rule reached the store: %+v", got)
			}
		})
	}
}

func TestGetRule(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/rules", slowRule)

	w := do(t, s, http.MethodGet, "/api/rules/stripe-is-slow", "")
	wantStatus(t, w, http.StatusOK)
	if got := decodeBody[rules.Rule](t, w); got.Name != "Stripe is slow" {
		t.Errorf("rule = %+v, want the one that was created", got)
	}

	wantError(t, do(t, s, http.MethodGet, "/api/rules/nope", ""), http.StatusNotFound, "")
}

func TestUpdateRuleReplacesIt(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/rules", slowRule)

	w := do(t, s, http.MethodPut, "/api/rules/stripe-is-slow",
		`{"name":"Stripe is down","match":{"host":"api.stripe.com"},"fault":{"type":"status","code":503,"body":"nope"}}`)

	wantStatus(t, w, http.StatusOK)
	got := decodeBody[rules.Rule](t, w)
	if got.ID != "stripe-is-slow" || got.Name != "Stripe is down" || got.Fault.Type != rules.FaultStatus || got.Fault.MS != 0 {
		t.Errorf("rule = %+v, want the delay replaced by a 503", got)
	}

	stored, err := s.rules.Get("stripe-is-slow")
	if err != nil || stored.Fault.Code != 503 {
		t.Errorf("stored = %+v, err = %v, want the update to have landed", stored, err)
	}
}

func TestUpdateRuleRejectsAnIDMismatchAndAnUnknownID(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/rules", slowRule)

	mismatch := `{"id":"other","name":"x","fault":{"type":"delay","ms":1}}`
	wantError(t, do(t, s, http.MethodPut, "/api/rules/stripe-is-slow", mismatch), http.StatusBadRequest, "id")

	unknown := `{"name":"x","fault":{"type":"delay","ms":1}}`
	wantError(t, do(t, s, http.MethodPut, "/api/rules/nope", unknown), http.StatusNotFound, "")
}

func TestDeleteRule(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/rules", slowRule)

	w := do(t, s, http.MethodDelete, "/api/rules/stripe-is-slow", "")
	wantStatus(t, w, http.StatusNoContent)
	if got := w.Body.Len(); got != 0 {
		t.Errorf("delete wrote %d bytes, want an empty body", got)
	}
	if got := s.rules.List(); len(got) != 0 {
		t.Errorf("store = %+v, want it empty", got)
	}

	wantError(t, do(t, s, http.MethodDelete, "/api/rules/stripe-is-slow", ""), http.StatusNotFound, "")
}

func TestEnableAndDisableRule(t *testing.T) {
	s := newTestServer(t)
	do(t, s, http.MethodPost, "/api/rules", slowRule)

	w := do(t, s, http.MethodPost, "/api/rules/stripe-is-slow/disable", "")
	wantStatus(t, w, http.StatusOK)
	if decodeBody[rules.Rule](t, w).Enabled {
		t.Error("disable returned an enabled rule")
	}

	w = do(t, s, http.MethodPost, "/api/rules/stripe-is-slow/enable", "")
	wantStatus(t, w, http.StatusOK)
	if !decodeBody[rules.Rule](t, w).Enabled {
		t.Error("enable returned a disabled rule")
	}

	wantError(t, do(t, s, http.MethodPost, "/api/rules/nope/enable", ""), http.StatusNotFound, "")
	wantError(t, do(t, s, http.MethodPost, "/api/rules/nope/disable", ""), http.StatusNotFound, "")
}
