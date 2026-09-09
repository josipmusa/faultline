package rules

import (
	"errors"
	"slices"
	"sync"
	"testing"
)

// scenarios builds a store holding rules a, b and c, all off, and a scenario
// store over it holding two scenarios that share rule b.
func scenarios(t *testing.T) (*Store, *Scenarios) {
	t.Helper()

	store := New()
	for _, id := range []string{"a", "b", "c"} {
		mustAdd(t, store, rule(id).WithEnabled(false))
	}
	sc := NewScenarios(store)
	sc.Replace([]Scenario{
		{Name: "first", Rules: []string{"a", "b"}},
		{Name: "second", Rules: []string{"b", "c"}},
	})
	return store, sc
}

func enabled(t *testing.T, s *Store) []string {
	t.Helper()
	var on []string
	for _, r := range s.List() {
		if r.Enabled {
			on = append(on, r.ID)
		}
	}
	return on
}

func revisionOf(t *testing.T, s *Store, id string) uint64 {
	t.Helper()
	r, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get(%q): %v", id, err)
	}
	return r.Revision
}

func TestScenariosListKeepsFileOrderAndNamesTheActiveOne(t *testing.T) {
	_, sc := scenarios(t)

	list, active := sc.List()
	if got := len(list); got != 2 {
		t.Fatalf("list holds %d scenarios, want 2", got)
	}
	if list[0].Name != "first" || list[1].Name != "second" {
		t.Errorf("list = %+v, want the order the file wrote", list)
	}
	if !slices.Equal(list[0].Rules, []string{"a", "b"}) {
		t.Errorf("rules = %v, want the ids the scenario names", list[0].Rules)
	}
	if active != "" {
		t.Errorf("active = %q, want nothing active before anything is activated", active)
	}

	if _, err := sc.Activate("second"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if _, active = sc.List(); active != "second" {
		t.Errorf("active = %q, want second", active)
	}
}

func TestActivateEnablesOnlyTheScenariosRules(t *testing.T) {
	store, sc := scenarios(t)

	if _, err := sc.Activate("first"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if got := enabled(t, store); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("enabled = %v, want the scenario's rules and nothing else", got)
	}
}

func TestActivateRestartsBehaviorStateEvenForARuleAlreadyOn(t *testing.T) {
	store, sc := scenarios(t)
	if err := store.Enable("a"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	before := revisionOf(t, store, "a")

	if _, err := sc.Activate("first"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if after := revisionOf(t, store, "a"); after == before {
		t.Errorf("revision stayed %d, so the rule's behavior state would carry over", after)
	}
}

func TestActivateAnotherScenarioTurnsTheFirstOneOff(t *testing.T) {
	store, sc := scenarios(t)
	if _, err := sc.Activate("first"); err != nil {
		t.Fatalf("Activate first: %v", err)
	}

	if _, err := sc.Activate("second"); err != nil {
		t.Fatalf("Activate second: %v", err)
	}

	if got := enabled(t, store); !slices.Equal(got, []string{"b", "c"}) {
		t.Errorf("enabled = %v, want the second scenario's rules; b belongs to both and stays on", got)
	}
	if _, active := sc.List(); active != "second" {
		t.Errorf("active = %q, want second", active)
	}
}

func TestActivateIsOneWriteToTheRuleStore(t *testing.T) {
	store, sc := scenarios(t)
	if _, err := sc.Activate("first"); err != nil {
		t.Fatalf("Activate first: %v", err)
	}

	// A reader looking between the two scenarios must never see a rule of each,
	// so the swap happens under one lock: read the store while it happens.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	bad := make(chan []string, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			var on []string
			for _, r := range store.List() {
				if r.Enabled {
					on = append(on, r.ID)
				}
			}
			if slices.Equal(on, []string{"a", "b"}) || slices.Equal(on, []string{"b", "c"}) {
				continue
			}
			select {
			case bad <- on:
			default:
			}
			return
		}
	}()

	for range 50 {
		if _, err := sc.Activate("second"); err != nil {
			t.Fatalf("Activate: %v", err)
		}
		if _, err := sc.Activate("first"); err != nil {
			t.Fatalf("Activate: %v", err)
		}
	}
	close(stop)
	wg.Wait()

	select {
	case on := <-bad:
		t.Errorf("a reader saw %v, which is neither scenario", on)
	default:
	}
}

func TestDeactivateTurnsTheScenariosRulesOff(t *testing.T) {
	store, sc := scenarios(t)
	if _, err := sc.Activate("first"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if _, err := sc.Deactivate("first"); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	if got := enabled(t, store); got != nil {
		t.Errorf("enabled = %v, want nothing left on", got)
	}
	if _, active := sc.List(); active != "" {
		t.Errorf("active = %q, want nothing active", active)
	}
}

func TestDeactivateAScenarioThatIsNotActiveIsNotAnError(t *testing.T) {
	store, sc := scenarios(t)
	if _, err := sc.Activate("first"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if _, err := sc.Deactivate("second"); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	if got := enabled(t, store); !slices.Equal(got, []string{"a"}) {
		t.Errorf("enabled = %v, want b off with the scenario naming it and a left alone", got)
	}
	if _, active := sc.List(); active != "first" {
		t.Errorf("active = %q, want first, which nobody deactivated", active)
	}
}

func TestUnknownScenarioName(t *testing.T) {
	store, sc := scenarios(t)

	if _, err := sc.Activate("ghost"); !errors.Is(err, ErrNoScenario) {
		t.Errorf("Activate ghost = %v, want ErrNoScenario", err)
	}
	if _, err := sc.Deactivate("ghost"); !errors.Is(err, ErrNoScenario) {
		t.Errorf("Deactivate ghost = %v, want ErrNoScenario", err)
	}
	if got := enabled(t, store); got != nil {
		t.Errorf("enabled = %v, want an unknown name to change nothing", got)
	}
}

func TestReplaceKeepsTheActiveScenarioTheFileStillDeclares(t *testing.T) {
	_, sc := scenarios(t)
	if _, err := sc.Activate("first"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	dropped := sc.Replace([]Scenario{
		{Name: "first", Rules: []string{"a"}},
		{Name: "third", Rules: []string{"c"}},
	})

	if dropped != "" {
		t.Errorf("dropped = %q, want nothing dropped", dropped)
	}
	list, active := sc.List()
	if active != "first" {
		t.Errorf("active = %q, want first, which the file still declares", active)
	}
	if len(list) != 2 || !slices.Equal(list[0].Rules, []string{"a"}) {
		t.Errorf("list = %+v, want the file's scenarios", list)
	}
}

func TestReplaceDropsAnActiveScenarioTheFileNoLongerDeclares(t *testing.T) {
	_, sc := scenarios(t)
	if _, err := sc.Activate("first"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	dropped := sc.Replace([]Scenario{{Name: "second", Rules: []string{"b"}}})

	if dropped != "first" {
		t.Errorf("dropped = %q, want first, the scenario that went away", dropped)
	}
	if _, active := sc.List(); active != "" {
		t.Errorf("active = %q, want nothing active", active)
	}
}

func TestForgetDropsADeletedRuleFromEveryScenario(t *testing.T) {
	_, sc := scenarios(t)

	sc.Forget("b")

	list, _ := sc.List()
	for _, s := range list {
		if slices.Contains(s.Rules, "b") {
			t.Errorf("scenario %q still names the deleted rule: %v", s.Name, s.Rules)
		}
	}
	if !slices.Equal(list[0].Rules, []string{"a"}) || !slices.Equal(list[1].Rules, []string{"c"}) {
		t.Errorf("list = %+v, want the other ids left alone", list)
	}
}

func TestScenariosKeepCopies(t *testing.T) {
	_, sc := scenarios(t)

	given := []Scenario{{Name: "only", Rules: []string{"a"}}}
	sc.Replace(given)
	given[0].Rules[0] = "c"

	list, _ := sc.List()
	if list[0].Rules[0] != "a" {
		t.Errorf("rules = %v, want the store to hold its own copy", list[0].Rules)
	}

	list[0].Rules[0] = "c"
	again, _ := sc.List()
	if again[0].Rules[0] != "a" {
		t.Errorf("rules = %v, want List to hand out copies", again[0].Rules)
	}
}

func TestActivateOnARuleTheStoreNoLongerHolds(t *testing.T) {
	store, sc := scenarios(t)
	if err := store.Delete("b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := sc.Activate("first"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if got := enabled(t, store); !slices.Equal(got, []string{"a"}) {
		t.Errorf("enabled = %v, want the rules that are still there", got)
	}
}

func TestSwitchTurnsRulesOnAndOffTogether(t *testing.T) {
	st := New()
	mustAdd(t, st, rule("a"), rule("b").WithEnabled(false), rule("c"))

	st.Switch([]string{"b"}, []string{"a", "c"})

	for _, want := range []struct {
		id string
		on bool
	}{{"a", false}, {"b", true}, {"c", false}} {
		got, err := st.Get(want.id)
		if err != nil {
			t.Fatalf("Get(%q): %v", want.id, err)
		}
		if got.Enabled != want.on {
			t.Errorf("%q enabled = %v, want %v", want.id, got.Enabled, want.on)
		}
	}
}

func TestSwitchStampsEveryRuleItTurnsOn(t *testing.T) {
	st := New()
	mustAdd(t, st, rule("a"))
	before, err := st.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	st.Switch([]string{"a"}, nil)

	after, err := st.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Revision == before.Revision {
		t.Errorf("revision stayed %d, so behavior state would carry over an activation", after.Revision)
	}
}

func TestSwitchLeavesARuleAlreadyOffAlone(t *testing.T) {
	st := New()
	mustAdd(t, st, rule("a").WithEnabled(false))
	before, err := st.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	st.Switch(nil, []string{"a"})

	after, err := st.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Revision != before.Revision {
		t.Errorf("revision moved to %d, want a rule that did not change left alone", after.Revision)
	}
}

func TestSwitchTurnsOnARuleNamedByBothLists(t *testing.T) {
	st := New()
	mustAdd(t, st, rule("a").WithEnabled(false))

	st.Switch([]string{"a"}, []string{"a"})

	got, err := st.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enabled {
		t.Error("a rule the incoming scenario names should be on, whatever the outgoing one said")
	}
}

func TestSwitchNotifiesOnlyWhenSomethingChanged(t *testing.T) {
	st := New()
	mustAdd(t, st, rule("a").WithEnabled(false))
	changes := st.Changes()
	drain(changes)

	st.Switch(nil, []string{"a", "nope"})
	select {
	case <-changes:
		t.Error("a switch that changed nothing notified")
	default:
	}

	st.Switch([]string{"a"}, nil)
	select {
	case <-changes:
	default:
		t.Error("a switch that turned a rule on did not notify")
	}
}
