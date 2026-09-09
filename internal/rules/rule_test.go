package rules

import (
	"encoding/json"
	"testing"
)

func TestRuleJSONRoundTrip(t *testing.T) {
	const in = `{
  "id": "slow-stripe",
  "name": "Stripe is slow",
  "enabled": true,
  "match": { "host": "api.stripe.com", "method": "POST", "path": "/v1/charges/*", "header": { "X-Test": "1" } },
  "fault": { "type": "delay", "ms": 2000, "jitter_ms": 500 }
}`

	var r Rule
	if err := json.Unmarshal([]byte(in), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if r.ID != "slow-stripe" || r.Name != "Stripe is slow" || !r.Enabled {
		t.Errorf("identity fields wrong: %+v", r)
	}
	if r.Match.Host != "api.stripe.com" || r.Match.Method != "POST" || r.Match.Path != "/v1/charges/*" {
		t.Errorf("match fields wrong: %+v", r.Match)
	}
	if r.Match.Header["X-Test"] != "1" {
		t.Errorf("match header wrong: %+v", r.Match.Header)
	}
	if r.Fault.Type != FaultDelay || r.Fault.MS != 2000 || r.Fault.JitterMS != 500 {
		t.Errorf("fault fields wrong: %+v", r.Fault)
	}

	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"slow-stripe","name":"Stripe is slow","enabled":true,` +
		`"match":{"host":"api.stripe.com","method":"POST","path":"/v1/charges/*","header":{"X-Test":"1"}},` +
		`"fault":{"type":"delay","ms":2000,"jitter_ms":500}}`
	if string(out) != want {
		t.Errorf("marshal:\n got %s\nwant %s", out, want)
	}
}

func TestStatusFaultJSON(t *testing.T) {
	r := Rule{
		ID:      "stripe-down",
		Name:    "Stripe is down",
		Enabled: true,
		Match:   Match{Host: "api.stripe.com"},
		Fault:   Fault{Type: FaultStatus, Code: 503, Body: "upstream unavailable"},
	}

	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"stripe-down","name":"Stripe is down","enabled":true,` +
		`"match":{"host":"api.stripe.com"},` +
		`"fault":{"type":"status","code":503,"body":"upstream unavailable"}}`
	if string(out) != want {
		t.Errorf("marshal:\n got %s\nwant %s", out, want)
	}
}

func TestWithEnabledReturnsCopy(t *testing.T) {
	original := Rule{
		ID:      "r1",
		Enabled: true,
		Match:   Match{Host: "api.stripe.com", Header: map[string]string{"X-Test": "1"}},
		Fault:   Fault{Type: FaultDelay, MS: 100},
	}

	disabled := original.WithEnabled(false)

	if !original.Enabled {
		t.Error("WithEnabled must not mutate the receiver")
	}
	if disabled.Enabled {
		t.Error("WithEnabled(false) must return a disabled rule")
	}
	if disabled.ID != original.ID || disabled.Match.Host != original.Match.Host {
		t.Error("WithEnabled must carry the other fields over")
	}

	disabled.Match.Header["X-Test"] = "tampered"
	if original.Match.Header["X-Test"] != "1" {
		t.Error("the copy must not share the header map with the original")
	}
}

func TestCloneDeepCopiesHeader(t *testing.T) {
	original := Rule{
		ID:    "r1",
		Match: Match{Host: "api.stripe.com", Header: map[string]string{"X-Test": "1"}},
	}

	clone := original.Clone()
	clone.Match.Header["X-Test"] = "tampered"
	delete(clone.Match.Header, "X-Test")

	if original.Match.Header["X-Test"] != "1" {
		t.Error("Clone must not share the header map with the original")
	}
}

func TestCloneWithNilHeader(t *testing.T) {
	original := Rule{ID: "r1", Match: Match{Host: "api.stripe.com"}}

	clone := original.Clone()

	if clone.Match.Header != nil {
		t.Errorf("Clone of a nil header should stay nil, got %v", clone.Match.Header)
	}
}

func TestUnmarshalDoesNotAliasCallerHeader(t *testing.T) {
	// A rule built from a caller-owned map must not be affected when that map changes.
	header := map[string]string{"X-Test": "1"}
	r := Rule{Enabled: true, Match: Match{Host: "h", Header: header}}.Clone()

	header["X-Test"] = "2"

	if !r.Matches("h", "GET", "/", headerOf("X-Test", "1")) {
		t.Error("the rule should still match the value it was built with")
	}
}

func TestRefuseFaultJSON(t *testing.T) {
	var f Fault
	if err := json.Unmarshal([]byte(`{"type":"refuse"}`), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Type != FaultRefuse {
		t.Errorf("type = %q, want %q", f.Type, FaultRefuse)
	}
	out, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != `{"type":"refuse"}` {
		t.Errorf("marshal = %s, want only the type: a refuse fault has no parameters", out)
	}
}

func TestIsConnectionFault(t *testing.T) {
	tests := []struct {
		typ  FaultType
		want bool
	}{
		{FaultDelay, true},
		{FaultRefuse, true},
		{FaultStatus, false},
		{FaultType("explode"), false},
	}
	for _, tt := range tests {
		if got := (Fault{Type: tt.typ}).IsConnection(); got != tt.want {
			t.Errorf("Fault{Type: %q}.IsConnection() = %v, want %v", tt.typ, got, tt.want)
		}
	}
}
