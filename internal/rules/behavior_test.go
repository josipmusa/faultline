package rules

import (
	"encoding/json"
	"testing"
)

func TestBehaviorJSONRoundTrip(t *testing.T) {
	const in = `{
  "id": "flaky-stripe",
  "name": "Stripe fails twice",
  "enabled": true,
  "match": { "host": "api.stripe.com" },
  "fault": { "type": "status", "code": 503 },
  "behavior": { "type": "first_n", "n": 2 }
}`

	var r Rule
	if err := json.Unmarshal([]byte(in), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if r.Behavior == nil {
		t.Fatal("behavior is nil, want first_n")
	}
	if r.Behavior.Type != "first_n" {
		t.Errorf("behavior type = %q, want %q", r.Behavior.Type, "first_n")
	}
	if r.Behavior.Params["n"] != float64(2) {
		t.Errorf("behavior params wrong: %+v", r.Behavior.Params)
	}

	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"flaky-stripe","name":"Stripe fails twice","enabled":true,` +
		`"match":{"host":"api.stripe.com"},` +
		`"fault":{"type":"status","code":503},` +
		`"behavior":{"type":"first_n","n":2}}`
	if string(out) != want {
		t.Errorf("marshal:\n got %s\nwant %s", out, want)
	}
}

func TestRuleWithoutBehaviorMarshalsWithoutTheField(t *testing.T) {
	out, err := json.Marshal(Rule{ID: "r1", Name: "r1", Fault: Fault{Type: "status", Params: Params{"code": 503}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"r1","name":"r1","enabled":false,"match":{},"fault":{"type":"status","code":503}}`
	if string(out) != want {
		t.Errorf("marshal:\n got %s\nwant %s", out, want)
	}
}

func TestCloneCopiesBehaviorParams(t *testing.T) {
	original := Rule{
		ID:       "r1",
		Fault:    Fault{Type: "status", Params: Params{"code": 503}},
		Behavior: &Behavior{Type: "first_n", Params: Params{"n": 2}},
	}

	clone := original.Clone()
	clone.Behavior.Params["n"] = 9

	if original.Behavior.Params["n"] != 2 {
		t.Errorf("editing the clone changed the original: %+v", original.Behavior.Params)
	}
	if clone.Behavior == original.Behavior {
		t.Error("clone shares the behavior pointer with the original")
	}
}
