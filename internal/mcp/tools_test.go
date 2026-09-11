package mcp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/rules"
)

// delayRule is the arguments for a rule an agent would add first: one host, one
// fault, nothing clever.
func delayRule(name, host string, ms int) map[string]any {
	return map[string]any{
		"name":  name,
		"match": map[string]any{"host": host},
		"fault": map[string]any{"type": "delay", "ms": ms},
	}
}

func (h harness) record(t *testing.T, host string, faulted bool) {
	t.Helper()
	h.events.Record(events.Event{
		ID:        events.NextID(),
		Timestamp: time.Now(),
		Host:      host,
		Method:    "GET",
		Path:      "/get",
		Status:    200,
		Faulted:   faulted,
		Tier:      events.TierPlain,
	})
}

func TestAddRuleStoresItAndMintsAnID(t *testing.T) {
	h := newHarness(t)

	got := out[client.Rule](t, h.call(t, "add_rule", delayRule("Stripe is slow", "api.stripe.com", 2000)))

	if got.ID != "stripe-is-slow" {
		t.Errorf("id = %q, want stripe-is-slow derived from the name", got.ID)
	}
	if !got.Enabled {
		t.Error("the rule came back disabled, want a new rule in force")
	}
	if got.Fault.Type != "delay" || got.Fault.Params["ms"] != float64(2000) {
		t.Errorf("fault = %+v, want a 2000ms delay", got.Fault)
	}
	if got.Match.Host != "api.stripe.com" {
		t.Errorf("match host = %q, want api.stripe.com", got.Match.Host)
	}

	// The store is the proof: the tool went through the API, not into a copy.
	stored := h.rules.List()
	if len(stored) != 1 || stored[0].ID != "stripe-is-slow" {
		t.Errorf("the store holds %+v, want the one rule", stored)
	}
}

func TestAddRuleTakesABehavior(t *testing.T) {
	h := newHarness(t)

	args := delayRule("flaky", "api.stripe.com", 10)
	args["behavior"] = map[string]any{"type": "first_n", "n": 2}

	got := out[client.Rule](t, h.call(t, "add_rule", args))

	if got.Behavior == nil || got.Behavior.Type != "first_n" {
		t.Fatalf("behavior = %+v, want first_n", got.Behavior)
	}
	if got.Behavior.Params["n"] != float64(2) {
		t.Errorf("behavior n = %v, want 2", got.Behavior.Params["n"])
	}
}

func TestAddRuleCanStartDisabled(t *testing.T) {
	h := newHarness(t)

	args := delayRule("armed later", "api.stripe.com", 10)
	args["enabled"] = false

	if got := out[client.Rule](t, h.call(t, "add_rule", args)); got.Enabled {
		t.Error("the rule is enabled, want it left off as asked")
	}
}

// Validation stays on the server, and what comes back has to name the field or
// the agent cannot fix it by itself.
func TestAddRuleReportsTheFieldItRefused(t *testing.T) {
	h := newHarness(t)

	args := delayRule("bad", "api.stripe.com", -1)
	got := h.callErr(t, "add_rule", args)

	if !strings.Contains(got, "fault.ms") {
		t.Errorf("refusal = %q, want it to name fault.ms", got)
	}
}

func TestAddRuleRefusesAFaultItDoesNotHave(t *testing.T) {
	h := newHarness(t)

	args := delayRule("bad", "api.stripe.com", 10)
	args["fault"] = map[string]any{"type": "explode"}

	// The schema holds the closed set of fault types, so the refusal names the
	// bad one and every alternative, rather than only saying no.
	got := h.callErr(t, "add_rule", args)
	for _, want := range []string{"explode", "delay", "status"} {
		if !strings.Contains(got, want) {
			t.Errorf("refusal = %q, want it to mention %q", got, want)
		}
	}
}

func TestListRulesKeepsTheOrderTheyApplyIn(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"first", "second", "third"} {
		h.call(t, "add_rule", delayRule(name, "api.stripe.com", 10))
	}

	got := out[rulesOut](t, h.call(t, "list_rules", nil))

	if len(got.Rules) != 3 || got.Rules[0].Name != "first" || got.Rules[2].Name != "third" {
		t.Errorf("list_rules = %+v, want first, second, third", got.Rules)
	}
}

func TestRemoveRuleDeletesIt(t *testing.T) {
	h := newHarness(t)
	h.call(t, "add_rule", delayRule("doomed", "api.stripe.com", 10))

	h.call(t, "remove_rule", map[string]any{"id": "doomed"})

	if got := h.rules.List(); len(got) != 0 {
		t.Errorf("the store holds %+v after remove_rule, want nothing", got)
	}
}

func TestRemoveRuleSaysWhenThereIsNoSuchRule(t *testing.T) {
	h := newHarness(t)

	if got := h.callErr(t, "remove_rule", map[string]any{"id": "ghost"}); !strings.Contains(got, "ghost") {
		t.Errorf("refusal = %q, want it to name the id", got)
	}
}

func TestSetRuleEnabledTurnsARuleOffAndOn(t *testing.T) {
	h := newHarness(t)
	h.call(t, "add_rule", delayRule("toggle", "api.stripe.com", 10))

	off := out[client.Rule](t, h.call(t, "set_rule_enabled", map[string]any{"id": "toggle", "enabled": false}))
	if off.Enabled {
		t.Error("the rule is still enabled after being turned off")
	}

	on := out[client.Rule](t, h.call(t, "set_rule_enabled", map[string]any{"id": "toggle", "enabled": true}))
	if !on.Enabled {
		t.Error("the rule is still disabled after being turned on")
	}
}

func TestListUpstreamsFoldsTheEventsByHost(t *testing.T) {
	h := newHarness(t)
	h.record(t, "api.stripe.com", true)
	h.record(t, "api.stripe.com", false)
	h.record(t, "httpbin.org", false)

	got := out[upstreamsOut](t, h.call(t, "list_upstreams", nil))

	if len(got.Upstreams) != 2 {
		t.Fatalf("list_upstreams = %+v, want two hosts", got.Upstreams)
	}
	stripe := got.Upstreams[0]
	if stripe.Host != "api.stripe.com" || stripe.Requests != 2 || stripe.Faulted != 1 {
		t.Errorf("stripe row = %+v, want 2 requests and 1 faulted", stripe)
	}
	if stripe.Tier != events.TierPlain {
		t.Errorf("stripe tier = %q, want plain", stripe.Tier)
	}
}

func TestGetEventsFiltersTheSameWayTheAPIDoes(t *testing.T) {
	h := newHarness(t)
	h.record(t, "api.stripe.com", true)
	h.record(t, "api.stripe.com", false)
	h.record(t, "httpbin.org", false)

	all := out[eventsOut](t, h.call(t, "get_events", nil))
	if len(all.Events) != 3 {
		t.Errorf("get_events = %d events, want 3", len(all.Events))
	}

	byHost := out[eventsOut](t, h.call(t, "get_events", map[string]any{"host": "api.stripe.com"}))
	if len(byHost.Events) != 2 {
		t.Errorf("get_events host=api.stripe.com = %d events, want 2", len(byHost.Events))
	}

	faulted := out[eventsOut](t, h.call(t, "get_events", map[string]any{"faulted": true}))
	if len(faulted.Events) != 1 || !faulted.Events[0].Faulted {
		t.Errorf("get_events faulted=true = %+v, want the one faulted event", faulted.Events)
	}

	limited := out[eventsOut](t, h.call(t, "get_events", map[string]any{"limit": 1}))
	if len(limited.Events) != 1 || limited.Events[0].Host != "httpbin.org" {
		t.Errorf("get_events limit=1 = %+v, want the most recent event", limited.Events)
	}
}

func TestGetReportCountsWhatHappened(t *testing.T) {
	h := newHarness(t)
	h.record(t, "api.stripe.com", true)
	h.record(t, "api.stripe.com", false)

	got := out[client.Report](t, h.call(t, "get_report", nil))

	if got.Total != 2 || got.Faulted != 1 {
		t.Errorf("get_report = %+v, want 2 total and 1 faulted", got)
	}
}

func TestWaitForEventReturnsACallThatArrivesDuringTheWait(t *testing.T) {
	h := newHarness(t)

	go func() {
		time.Sleep(150 * time.Millisecond)
		h.record(t, "api.stripe.com", true)
	}()

	got := out[waitOut](t, h.call(t, "wait_for_event",
		map[string]any{"host": "api.stripe.com", "timeout_ms": 5000}))

	if !got.Matched {
		t.Fatalf("wait_for_event = %+v, want a match", got)
	}
	if got.Event == nil || got.Event.Host != "api.stripe.com" {
		t.Errorf("wait_for_event event = %+v, want the stripe call", got.Event)
	}
}

// Nothing arriving is an ordinary answer. The tool must not report an error, or
// an agent checking that a call is no longer made reads a failure instead.
func TestWaitForEventReportsNoMatchWithoutFailing(t *testing.T) {
	h := newHarness(t)

	got := out[waitOut](t, h.call(t, "wait_for_event",
		map[string]any{"host": "nothing.example.com", "timeout_ms": 300}))

	if got.Matched {
		t.Errorf("wait_for_event = %+v, want no match", got)
	}
	if got.Event != nil {
		t.Errorf("wait_for_event returned an event with matched false: %+v", got.Event)
	}
}

func TestWaitForEventRefusesATimeoutItWillNotHonour(t *testing.T) {
	h := newHarness(t)

	if got := h.callErr(t, "wait_for_event", map[string]any{"timeout_ms": 600000}); !strings.Contains(got, "300000") {
		t.Errorf("refusal = %q, want it to name the limit", got)
	}
}

func TestScenarioToolsTurnASituationOnAndOff(t *testing.T) {
	h := newHarness(t)
	h.call(t, "add_rule", delayRule("stripe down", "api.stripe.com", 10))
	if err := h.scenarios.Add(rules.Scenario{Name: "outage", Rules: []string{"stripe-down"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	listed := out[scenariosOut](t, h.call(t, "list_scenarios", nil))
	if len(listed.Scenarios) != 1 || listed.Scenarios[0].Name != "outage" || listed.Scenarios[0].Active {
		t.Fatalf("list_scenarios = %+v, want one inactive scenario", listed.Scenarios)
	}

	on := out[client.Scenario](t, h.call(t, "activate_scenario", map[string]any{"name": "outage"}))
	if !on.Active {
		t.Errorf("activate_scenario = %+v, want it active", on)
	}
	if _, active := h.scenarios.List(); active != "outage" {
		t.Errorf("the active scenario is %q, want outage", active)
	}

	off := out[client.Scenario](t, h.call(t, "deactivate_scenario", map[string]any{"name": "outage"}))
	if off.Active {
		t.Errorf("deactivate_scenario = %+v, want it inactive", off)
	}
}

func TestActivateScenarioSaysWhenThereIsNoSuchScenario(t *testing.T) {
	h := newHarness(t)

	if got := h.callErr(t, "activate_scenario", map[string]any{"name": "ghost"}); !strings.Contains(got, "ghost") {
		t.Errorf("refusal = %q, want it to name the scenario", got)
	}
}

func TestResetSessionClearsTheEventsAndRearmsTheRules(t *testing.T) {
	h := newHarness(t)
	h.record(t, "api.stripe.com", true)
	h.call(t, "add_rule", delayRule("kept", "api.stripe.com", 10))

	got := out[resetOut](t, h.call(t, "reset_session", nil))
	if !got.Reset {
		t.Errorf("reset_session = %+v, want reset true", got)
	}

	if left := out[eventsOut](t, h.call(t, "get_events", nil)); len(left.Events) != 0 {
		t.Errorf("%d events after reset_session, want 0", len(left.Events))
	}
	if h.gate.calls != 1 {
		t.Errorf("the rules were re-armed %d times, want 1", h.gate.calls)
	}
	if kept := h.rules.List(); len(kept) != 1 {
		t.Errorf("the store holds %+v after reset_session, want the rule kept", kept)
	}
}

// The add_rule description is where an agent learns what a fault takes, since
// an MCP schema cannot carry an open parameter bag. It is generated from the
// registry, so every registered fault has to appear in it.
func TestAddRuleDescriptionListsEveryFault(t *testing.T) {
	h := newHarness(t)

	listed, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var description string
	for _, tool := range listed.Tools {
		if tool.Name == "add_rule" {
			description = tool.Description
		}
	}

	for _, want := range []string{"delay", "status", "first_n", "percent"} {
		if !strings.Contains(description, want) {
			t.Errorf("the add_rule description does not mention %q:\n%s", want, description)
		}
	}
	// The bounds matter as much as the names: an agent that cannot see them
	// discovers them by being refused.
	if !strings.Contains(description, "min") {
		t.Errorf("the add_rule description carries no parameter bounds:\n%s", description)
	}
}

// waitOut and resetOut mirror the tools' own output types, which are
// unexported. Decoding into a copy of the shape is the agent's view of it.
type waitOut struct {
	Matched  bool          `json:"matched"`
	Event    *client.Event `json:"event"`
	WaitedMS int64         `json:"waited_ms"`
}

type resetOut struct {
	Reset bool `json:"reset"`
}

type rulesOut struct {
	Rules []client.Rule `json:"rules"`
}

type upstreamsOut struct {
	Upstreams []client.Upstream `json:"upstreams"`
}

type eventsOut struct {
	Events []client.Event `json:"events"`
}

type scenariosOut struct {
	Scenarios []client.Scenario `json:"scenarios"`
}

// statusRuleArgs is a response-tier fault, which is the kind that cannot apply
// to a host Faultline has only ever seen encrypted.
func statusRuleArgs(name, host string, code int) map[string]any {
	return map[string]any{
		"name":  name,
		"match": map[string]any{"host": host},
		"fault": map[string]any{"type": "status", "code": code},
	}
}

func (h harness) encrypted(t *testing.T, host string) {
	t.Helper()
	h.events.Record(events.Event{
		ID:        events.NextID(),
		Timestamp: time.Now(),
		Host:      host,
		Method:    "CONNECT",
		Tier:      events.TierEncrypted,
	})
}

func TestAddRuleCarriesTheWarningThatItCannotFire(t *testing.T) {
	h := newHarness(t)
	h.encrypted(t, "api.stripe.com")

	got := out[client.RuleResult](t, h.call(t, "add_rule", statusRuleArgs("Stripe is down", "api.stripe.com", 503)))

	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "api.stripe.com") {
		t.Fatalf("warnings = %v, want one naming the host", got.Warnings)
	}
	if got.ID != "stripe-is-down" {
		t.Errorf("id = %q, want the rule itself still there", got.ID)
	}
}

func TestGetReportCarriesTheWarningsForRulesThatCannotFire(t *testing.T) {
	h := newHarness(t)
	h.encrypted(t, "api.stripe.com")
	h.call(t, "add_rule", statusRuleArgs("Stripe is down", "api.stripe.com", 503))

	got := out[client.ReportResult](t, h.call(t, "get_report", nil))

	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "stripe-is-down") {
		t.Errorf("warnings = %v, want one naming the rule", got.Warnings)
	}
}
