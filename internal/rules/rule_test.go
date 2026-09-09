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
	if r.Fault.Type != "delay" {
		t.Errorf("fault type = %q, want %q", r.Fault.Type, "delay")
	}
	if r.Fault.Params["ms"] != float64(2000) || r.Fault.Params["jitter_ms"] != float64(500) {
		t.Errorf("fault params wrong: %+v", r.Fault.Params)
	}

	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"slow-stripe","name":"Stripe is slow","enabled":true,` +
		`"match":{"host":"api.stripe.com","method":"POST","path":"/v1/charges/*","header":{"X-Test":"1"}},` +
		`"fault":{"type":"delay","jitter_ms":500,"ms":2000}}`
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
		Fault:   Fault{Type: "status", Params: Params{"code": 503, "body": "upstream unavailable"}},
	}

	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"stripe-down","name":"Stripe is down","enabled":true,` +
		`"match":{"host":"api.stripe.com"},` +
		`"fault":{"type":"status","body":"upstream unavailable","code":503}}`
	if string(out) != want {
		t.Errorf("marshal:\n got %s\nwant %s", out, want)
	}
}

func TestWithEnabledReturnsCopy(t *testing.T) {
	original := Rule{
		ID:      "r1",
		Enabled: true,
		Match:   Match{Host: "api.stripe.com", Header: map[string]string{"X-Test": "1"}},
		Fault:   Fault{Type: "delay", Params: Params{"ms": 100}},
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
	if f.Type != "refuse" {
		t.Errorf("type = %q, want %q", f.Type, "refuse")
	}
	if len(f.Params) != 0 {
		t.Errorf("params = %v, want none: a refuse fault has no parameters", f.Params)
	}
	out, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != `{"type":"refuse"}` {
		t.Errorf("marshal = %s, want only the type: a refuse fault has no parameters", out)
	}
}

func TestFaultKeepsUnknownParams(t *testing.T) {
	// The domain type knows no fault, so every key but the type is a parameter,
	// even one no fault declares. Rejecting it is the API boundary's job.
	var f Fault
	if err := json.Unmarshal([]byte(`{"type":"delay","ms":10,"nonsense":true}`), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Params["nonsense"] != true {
		t.Errorf("params = %v, want the unknown key kept", f.Params)
	}

	out, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != `{"type":"delay","ms":10,"nonsense":true}` {
		t.Errorf("marshal = %s, want the parameters back unchanged", out)
	}
}

func TestCloneDeepCopiesFaultParams(t *testing.T) {
	original := Rule{ID: "r1", Fault: Fault{Type: "delay", Params: Params{"ms": 100}}}

	clone := original.Clone()
	clone.Fault.Params["ms"] = 9000

	if original.Fault.Params["ms"] != 100 {
		t.Error("Clone must not share the fault parameters with the original")
	}
}

func TestCloneWithNilFaultParams(t *testing.T) {
	original := Rule{ID: "r1", Fault: Fault{Type: "refuse"}}

	if clone := original.Clone(); clone.Fault.Params != nil {
		t.Errorf("Clone of nil params should stay nil, got %v", clone.Fault.Params)
	}
}
