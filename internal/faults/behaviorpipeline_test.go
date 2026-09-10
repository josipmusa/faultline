package faults

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/rules"
)

// statuses runs n requests through a transport and returns the status of each,
// so a sequence of faulted and real responses reads at a glance. The upstream
// answers 418, which no rule here produces.
func statuses(t *testing.T, tr *Transport, url string, n int) []int {
	t.Helper()
	out := make([]int, 0, n)
	for range n {
		resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, url, ""))
		if err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		_ = readBody(t, resp)
		out = append(out, resp.StatusCode)
	}
	return out
}

func TestBehaviorLetsRequestsPastTheFaultThroughToUpstream(t *testing.T) {
	up, hits := upstream(t)
	rec := events.NewRecorder(10)
	rule := statusRule("flaky", 503, "")
	rule.Behavior = &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 2}}

	tr := New(http.DefaultTransport, storeWith(t, rule), rec, events.TierPlain, NewGate())

	got := statuses(t, tr, up.URL+"/charges", 3)
	if want := []int{503, 503, http.StatusTeapot}; !slices.Equal(got, want) {
		t.Errorf("statuses = %v, want %v", got, want)
	}
	if hits.Load() != 1 {
		t.Errorf("upstream hit %d times, want 1", hits.Load())
	}

	recorded := rec.Events()
	if len(recorded) != 3 {
		t.Fatalf("recorded %d events, want 3: %+v", len(recorded), recorded)
	}
	for i, e := range recorded {
		wantFaulted := i < 2
		if e.Faulted != wantFaulted {
			t.Errorf("event %d faulted = %v, want %v", i, e.Faulted, wantFaulted)
		}
		if wantFaulted && e.RuleID != "flaky" {
			t.Errorf("event %d rule = %q, want %q", i, e.RuleID, "flaky")
		}
		if !wantFaulted && e.RuleID != "" {
			t.Errorf("event %d names rule %q for a request the behavior passed", i, e.RuleID)
		}
	}
}

func TestBehaviorPassingFallsThroughToTheNextRule(t *testing.T) {
	up, hits := upstream(t)
	rec := events.NewRecorder(10)
	first := statusRule("flaky", 503, "")
	first.Behavior = &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 1}}
	second := statusRule("always", 500, "")

	tr := New(http.DefaultTransport, storeWith(t, first, second), rec, events.TierPlain, NewGate())

	got := statuses(t, tr, up.URL+"/charges", 2)
	if want := []int{503, 500}; !slices.Equal(got, want) {
		t.Errorf("statuses = %v, want %v: a rule whose behavior declines hands the request on", got, want)
	}
	if hits.Load() != 0 {
		t.Errorf("upstream hit %d times, want 0", hits.Load())
	}

	recorded := rec.Events()
	if len(recorded) != 2 {
		t.Fatalf("recorded %d events, want 2: %+v", len(recorded), recorded)
	}
	if recorded[0].RuleID != "flaky" || recorded[1].RuleID != "always" {
		t.Errorf("events name rules %q and %q, want flaky then always", recorded[0].RuleID, recorded[1].RuleID)
	}
}

func TestEveryBehaviorDecliningSendsTheRequestUpstream(t *testing.T) {
	up, hits := upstream(t)
	rec := events.NewRecorder(10)
	first := statusRule("flaky", 503, "")
	first.Behavior = &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 1}}
	second := statusRule("flakier", 500, "")
	second.Behavior = &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 1}}

	tr := New(http.DefaultTransport, storeWith(t, first, second), rec, events.TierPlain, NewGate())

	got := statuses(t, tr, up.URL+"/charges", 3)
	if want := []int{503, 500, http.StatusTeapot}; !slices.Equal(got, want) {
		t.Errorf("statuses = %v, want %v", got, want)
	}
	if hits.Load() != 1 {
		t.Errorf("upstream hit %d times, want 1", hits.Load())
	}
	last := rec.Events()[2]
	if last.Faulted || last.RuleID != "" {
		t.Errorf("last event = %+v, want no fault and no rule", last)
	}
}

func TestARuleBehindTheOneThatAppliedKeepsItsBehaviorState(t *testing.T) {
	up, _ := upstream(t)
	first := statusRule("always", 503, "")
	first.Behavior = &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 2}}
	second := statusRule("later", 500, "")
	second.Behavior = &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 1}}

	tr := New(http.DefaultTransport, storeWith(t, first, second), events.NewRecorder(10), events.TierPlain, NewGate())

	// The second rule is never consulted while the first applies, so its one
	// failure is still unspent when the first runs out.
	got := statuses(t, tr, up.URL+"/charges", 4)
	if want := []int{503, 503, 500, http.StatusTeapot}; !slices.Equal(got, want) {
		t.Errorf("statuses = %v, want %v: a shadowed rule must not count requests it never saw", got, want)
	}
}

func TestOneGateCountsARuleOnceAcrossPipelines(t *testing.T) {
	up, _ := upstream(t)
	rule := statusRule("flaky", 503, "")
	rule.Behavior = &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 2}}

	store := storeWith(t, rule)
	gate := NewGate()
	plain := New(http.DefaultTransport, store, events.NewRecorder(10), events.TierPlain, gate)
	intercepted := New(http.DefaultTransport, store, events.NewRecorder(10), events.TierIntercepted, gate)

	if got := statuses(t, plain, up.URL+"/charges", 1); got[0] != 503 {
		t.Fatalf("plain status = %d, want 503", got[0])
	}
	got := statuses(t, intercepted, up.URL+"/charges", 2)
	if want := []int{503, http.StatusTeapot}; !slices.Equal(got, want) {
		t.Errorf("intercepted statuses = %v, want %v: the two tiers count separately", got, want)
	}
}

func TestATransportWithoutAGateStillHonoursBehaviors(t *testing.T) {
	up, _ := upstream(t)
	rule := statusRule("flaky", 503, "")
	rule.Behavior = &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 1}}

	tr := New(http.DefaultTransport, storeWith(t, rule), events.NewRecorder(10), events.TierPlain, nil)

	got := statuses(t, tr, up.URL+"/charges", 2)
	if want := []int{503, http.StatusTeapot}; !slices.Equal(got, want) {
		t.Errorf("statuses = %v, want %v", got, want)
	}
}

func TestDialBehaviorRefusesOnlyTheFirstConnection(t *testing.T) {
	addr := tcpUpstream(t)
	rec := events.NewRecorder(10)
	rule := refuseRule("stripe-down")
	rule.Behavior = &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 1}}

	d := NewDialer(storeWith(t, rule), rec, NewGate())

	conn, err := d.Dial(context.Background(), addr)
	var refused *RefusedError
	if !errors.As(err, &refused) {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatalf("first dial err = %v, want a *RefusedError", err)
	}

	conn, err = d.Dial(context.Background(), addr)
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	_ = conn.Close()

	recorded := rec.Events()
	if len(recorded) != 2 {
		t.Fatalf("recorded %d events, want 2: %+v", len(recorded), recorded)
	}
	if !recorded[0].Faulted || recorded[0].RuleID != "stripe-down" {
		t.Errorf("first event = %+v, want faulted by the rule", recorded[0])
	}
	if recorded[1].Faulted {
		t.Errorf("second event = %+v, want no fault", recorded[1])
	}
}

func TestDialBehaviorPassingFallsThroughToTheNextRule(t *testing.T) {
	addr := tcpUpstream(t)
	rec := events.NewRecorder(10)
	first := refuseRule("stripe-down")
	first.Behavior = &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 1}}
	second := delayRule("stripe-slow", 100)

	d := NewDialer(storeWith(t, first, second), rec, NewGate())

	conn, err := d.Dial(context.Background(), addr)
	var refused *RefusedError
	if !errors.As(err, &refused) {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatalf("first dial err = %v, want a *RefusedError", err)
	}

	conn, err = d.Dial(context.Background(), addr)
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	_ = conn.Close()

	recorded := rec.Events()
	if len(recorded) != 2 {
		t.Fatalf("recorded %d events, want 2: %+v", len(recorded), recorded)
	}
	if recorded[0].RuleID != "stripe-down" {
		t.Errorf("first event = %+v, want the refuse rule", recorded[0])
	}
	if !recorded[1].Faulted || recorded[1].RuleID != "stripe-slow" || recorded[1].DurationMS < 100 {
		t.Errorf("second event = %+v, want the delay rule after at least 100ms", recorded[1])
	}
}
