package faults

import (
	"sync"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

func behaviorRule(id string, behavior *rules.Behavior) rules.Rule {
	r := statusRule(id, 503, "")
	r.Behavior = behavior
	return r
}

func firstNRule(id string, n int) rules.Rule {
	return behaviorRule(id, &rules.Behavior{Type: "first_n", Params: rules.Params{"n": n}})
}

// gateDecisions asks the gate about the store's copy of a rule n times, so the
// rule carries whatever revision the store last gave it.
func gateDecisions(t *testing.T, g *Gate, store *rules.Store, id string, n int) string {
	t.Helper()
	out := make([]byte, 0, n)
	for range n {
		rule, err := store.Get(id)
		if err != nil {
			t.Fatalf("Get(%q): %v", id, err)
		}
		step := byte('P')
		if g.Applies(rule) {
			step = 'F'
		}
		out = append(out, step)
	}
	return string(out)
}

func TestGateAppliesARuleWithNoBehavior(t *testing.T) {
	store := storeWith(t, statusRule("plain", 503, ""))

	if got := gateDecisions(t, NewGate(), store, "plain", 3); got != "FFF" {
		t.Errorf("a rule with no behavior gave %q, want %q", got, "FFF")
	}
}

func TestGateCountsPerRule(t *testing.T) {
	store := storeWith(t, firstNRule("a", 1), firstNRule("b", 1))
	gate := NewGate()

	if got := gateDecisions(t, gate, store, "a", 2); got != "FP" {
		t.Errorf("rule a gave %q, want %q", got, "FP")
	}
	if got := gateDecisions(t, gate, store, "b", 2); got != "FP" {
		t.Errorf("rule b gave %q, want %q, so the two rules share one counter", got, "FP")
	}
}

func TestGateStartsOverWhenARuleIsEnabled(t *testing.T) {
	store := storeWith(t, firstNRule("a", 2))
	gate := NewGate()

	if got := gateDecisions(t, gate, store, "a", 3); got != "FFP" {
		t.Fatalf("first_n 2 gave %q, want %q", got, "FFP")
	}

	if err := store.Disable("a"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if err := store.Enable("a"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	if got := gateDecisions(t, gate, store, "a", 3); got != "FFP" {
		t.Errorf("after turning the rule off and on again it gave %q, want %q", got, "FFP")
	}
}

func TestGateStartsOverWhenARuleIsEdited(t *testing.T) {
	store := storeWith(t, firstNRule("a", 2))
	gate := NewGate()

	if got := gateDecisions(t, gate, store, "a", 3); got != "FFP" {
		t.Fatalf("first_n 2 gave %q, want %q", got, "FFP")
	}

	edited := firstNRule("a", 1)
	edited.Name = "renamed"
	if err := store.Update(edited); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got := gateDecisions(t, gate, store, "a", 3); got != "FPP" {
		t.Errorf("after editing the rule it gave %q, want %q", got, "FPP")
	}
}

func TestGateKeepsStateWhileTheRuleIsUntouched(t *testing.T) {
	store := storeWith(t, firstNRule("a", 2), firstNRule("b", 2))
	gate := NewGate()

	if got := gateDecisions(t, gate, store, "a", 1); got != "F" {
		t.Fatalf("rule a gave %q, want %q", got, "F")
	}
	if err := store.Disable("b"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if got := gateDecisions(t, gate, store, "a", 2); got != "FP" {
		t.Errorf("rule a gave %q after b changed, want %q", got, "FP")
	}
}

func TestGateAppliesARuleWhoseBehaviorCannotBeBuilt(t *testing.T) {
	store := storeWith(t, behaviorRule("broken", &rules.Behavior{Type: "sometimes"}))

	if got := gateDecisions(t, NewGate(), store, "broken", 2); got != "FF" {
		t.Errorf("a rule with an unusable behavior gave %q, want %q", got, "FF")
	}
}

func TestGateCountsOnceUnderConcurrentRequests(t *testing.T) {
	store := storeWith(t, firstNRule("a", 100))
	gate := NewGate()

	rule, err := store.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	var mu sync.Mutex
	applied := 0
	var wg sync.WaitGroup
	for range 400 {
		wg.Go(func() {
			if gate.Applies(rule) {
				mu.Lock()
				applied++
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	if applied != 100 {
		t.Errorf("first_n 100 applied to %d of 400 concurrent requests", applied)
	}
}

func TestGateResetRearmsASpentBehavior(t *testing.T) {
	store := storeWith(t, firstNRule("spent", 2))
	gate := NewGate()

	if got := gateDecisions(t, gate, store, "spent", 3); got != "FFP" {
		t.Fatalf("first_n 2 gave %q, want %q", got, "FFP")
	}

	gate.Reset()

	if got := gateDecisions(t, gate, store, "spent", 3); got != "FFP" {
		t.Errorf("after Reset, first_n 2 gave %q, want %q: the rule should count from the start again", got, "FFP")
	}
}

func TestGateResetIsSafeWithNothingToForget(t *testing.T) {
	gate := NewGate()
	gate.Reset()

	store := storeWith(t, firstNRule("fresh", 1))
	if got := gateDecisions(t, gate, store, "fresh", 2); got != "FP" {
		t.Errorf("after Reset on an empty gate, first_n 1 gave %q, want %q", got, "FP")
	}
}
