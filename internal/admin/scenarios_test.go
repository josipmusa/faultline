package admin

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/rules"
)

// twoScenarios seeds the server the way a configuration file would: three rules
// that are all off, and two scenarios that share the middle one.
func twoScenarios(t *testing.T, s *Server) {
	t.Helper()

	for _, id := range []string{"slow", "flaky", "down"} {
		rule := rules.Rule{ID: id, Name: id, Fault: rules.Fault{Type: "delay", Params: rules.Params{"ms": 10}}}
		if err := s.rules.Add(rule); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}
	s.scenarios.Replace([]rules.Scenario{
		{Name: "payments-down", Rules: []string{"slow", "flaky"}},
		{Name: "orders-flaky", Rules: []string{"flaky", "down"}},
	})
}

func enabledIDs(t *testing.T, s *Server) []string {
	t.Helper()
	var on []string
	for _, r := range s.rules.List() {
		if r.Enabled {
			on = append(on, r.ID)
		}
	}
	return on
}

func TestListScenariosIsEmptyWithoutAConfigFile(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodGet, "/api/scenarios", "")

	wantStatus(t, w, http.StatusOK)
	if got := w.Body.String(); got != "[]\n" {
		t.Errorf("list = %q, want an empty JSON array", got)
	}
}

func TestListScenariosKeepsFileOrderAndMarksTheActiveOne(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)

	got := decodeBody[[]Scenario](t, do(t, s, http.MethodGet, "/api/scenarios", ""))

	if len(got) != 2 || got[0].Name != "payments-down" || got[1].Name != "orders-flaky" {
		t.Fatalf("list = %+v, want the order the file wrote", got)
	}
	if !slices.Equal(got[0].Rules, []string{"slow", "flaky"}) {
		t.Errorf("rules = %v, want the ids the scenario names", got[0].Rules)
	}
	if got[0].Active || got[1].Active {
		t.Errorf("list = %+v, want nothing active before anything is activated", got)
	}

	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios/orders-flaky/activate", ""), http.StatusOK)

	got = decodeBody[[]Scenario](t, do(t, s, http.MethodGet, "/api/scenarios", ""))
	if got[0].Active || !got[1].Active {
		t.Errorf("list = %+v, want only orders-flaky active", got)
	}
}

func TestActivateEnablesTheScenariosRulesAndActivatingAnotherFlipsThem(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)

	w := do(t, s, http.MethodPost, "/api/scenarios/payments-down/activate", "")
	wantStatus(t, w, http.StatusOK)
	if got := decodeBody[Scenario](t, w); !got.Active || got.Name != "payments-down" {
		t.Errorf("response = %+v, want the scenario, active", got)
	}
	if got := enabledIDs(t, s); !slices.Equal(got, []string{"slow", "flaky"}) {
		t.Fatalf("enabled = %v, want the scenario's rules and nothing else", got)
	}

	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios/orders-flaky/activate", ""), http.StatusOK)

	if got := enabledIDs(t, s); !slices.Equal(got, []string{"flaky", "down"}) {
		t.Errorf("enabled = %v, want the flags flipped to the second scenario", got)
	}
}

func TestActivateRestartsTheBehaviorStateOfEveryRuleItNames(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)
	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios/payments-down/activate", ""), http.StatusOK)

	before, err := s.rules.Get("slow")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios/payments-down/activate", ""), http.StatusOK)

	after, err := s.rules.Get("slow")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Revision == before.Revision {
		t.Error("re-activating a scenario left the revision alone, so first_n would not start over")
	}
}

func TestDeactivateTurnsTheScenariosRulesOff(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)
	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios/payments-down/activate", ""), http.StatusOK)

	w := do(t, s, http.MethodPost, "/api/scenarios/payments-down/deactivate", "")

	wantStatus(t, w, http.StatusOK)
	if got := decodeBody[Scenario](t, w); got.Active {
		t.Errorf("response = %+v, want it reported as no longer active", got)
	}
	if got := enabledIDs(t, s); got != nil {
		t.Errorf("enabled = %v, want nothing left on", got)
	}
}

func TestDeactivateAScenarioThatIsNotActiveSucceeds(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)

	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios/orders-flaky/deactivate", ""), http.StatusOK)
	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios/orders-flaky/deactivate", ""), http.StatusOK)

	if got := enabledIDs(t, s); got != nil {
		t.Errorf("enabled = %v, want the rules off however often it is asked for", got)
	}
}

func TestUnknownScenarioIsNotFound(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)

	for _, target := range []string{"/api/scenarios/ghost/activate", "/api/scenarios/ghost/deactivate"} {
		w := do(t, s, http.MethodPost, target, "")
		wantStatus(t, w, http.StatusNotFound)
		if got := decodeBody[apiError](t, w); got.Message != `no scenario named "ghost"` {
			t.Errorf("%s said %q", target, got.Message)
		}
	}
}

func TestDeletingARuleDropsItFromEveryScenario(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)

	wantStatus(t, do(t, s, http.MethodDelete, "/api/rules/flaky", ""), http.StatusNoContent)

	got := decodeBody[[]Scenario](t, do(t, s, http.MethodGet, "/api/scenarios", ""))
	if !slices.Equal(got[0].Rules, []string{"slow"}) || !slices.Equal(got[1].Rules, []string{"down"}) {
		t.Errorf("scenarios = %+v, still name a rule that is gone", got)
	}
}

func TestActivationIsPersistedLikeAnyOtherRuleChange(t *testing.T) {
	s, c := persisting(t)
	twoScenarios(t, s)

	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios/payments-down/activate", ""), http.StatusOK)
	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios/payments-down/deactivate", ""), http.StatusOK)

	if c.changes != 2 {
		t.Errorf("%d of the 2 activations were written to the configuration file", c.changes)
	}
}

func TestReadingScenariosIsNotPersisted(t *testing.T) {
	s, c := persisting(t)
	twoScenarios(t, s)

	wantStatus(t, do(t, s, http.MethodGet, "/api/scenarios", ""), http.StatusOK)

	if c.changes != 0 {
		t.Errorf("listing the scenarios wrote the file: %d changes", c.changes)
	}
}

func TestActivationIsRefusedWhileTheConfigFileIsBroken(t *testing.T) {
	s, c := persisting(t)
	twoScenarios(t, s)
	c.fail = &config.Error{File: "faultline.yaml", Line: 9, Message: "this is not valid YAML"}

	w := do(t, s, http.MethodPost, "/api/scenarios/payments-down/activate", "")

	wantStatus(t, w, http.StatusConflict)
	if got := enabledIDs(t, s); got != nil {
		t.Errorf("enabled = %v, want nothing changed when the change cannot be saved", got)
	}
}

func TestCreateScenarioAppendsItInactive(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)

	w := do(t, s, http.MethodPost, "/api/scenarios", `{"name":"everything-slow","rules":["slow","down"]}`)

	wantStatus(t, w, http.StatusCreated)
	got := decodeBody[Scenario](t, w)
	if got.Name != "everything-slow" || !slices.Equal(got.Rules, []string{"slow", "down"}) {
		t.Errorf("created = %+v, want the scenario as posted", got)
	}
	if got.Active {
		t.Error("the new scenario is active; creating one says what a situation is, activating puts it in force")
	}
	if on := enabledIDs(t, s); on != nil {
		t.Errorf("enabled = %v, want no rule touched by creating a scenario", on)
	}

	list := decodeBody[[]Scenario](t, do(t, s, http.MethodGet, "/api/scenarios", ""))
	if len(list) != 3 || list[2].Name != "everything-slow" {
		t.Errorf("list = %+v, want the new scenario written last", list)
	}
}

func TestCreateScenarioRefusesADuplicateName(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)

	w := do(t, s, http.MethodPost, "/api/scenarios", `{"name":"orders-flaky","rules":["slow"]}`)

	wantError(t, w, http.StatusConflict, "name")
	if list := decodeBody[[]Scenario](t, do(t, s, http.MethodGet, "/api/scenarios", "")); len(list) != 2 {
		t.Errorf("list holds %d scenarios, want the two it had", len(list))
	}
}

func TestCreateScenarioRefusesAnEmptyName(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)

	wantError(t, do(t, s, http.MethodPost, "/api/scenarios", `{"rules":["slow"]}`), http.StatusBadRequest, "name")
}

func TestCreateScenarioRefusesARuleThatIsNotThere(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)

	w := do(t, s, http.MethodPost, "/api/scenarios", `{"name":"ghost","rules":["slow","nothing"]}`)

	wantError(t, w, http.StatusBadRequest, "rules")
	if got := decodeBody[apiError](t, w); !strings.Contains(got.Message, "nothing") {
		t.Errorf("error = %q, want it to name the rule that is not there", got.Message)
	}
}

func TestCreateScenarioRefusesAnUnknownField(t *testing.T) {
	s := newTestServer(t)
	twoScenarios(t, s)

	wantError(t, do(t, s, http.MethodPost, "/api/scenarios",
		`{"name":"typo","rules":["slow"],"active":true}`), http.StatusBadRequest, "")
}

func TestCreatingAScenarioIsPersistedLikeARuleChange(t *testing.T) {
	s, c := persisting(t)
	twoScenarios(t, s)

	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios",
		`{"name":"everything-slow","rules":["slow"]}`), http.StatusCreated)

	if c.changes != 1 {
		t.Errorf("%d changes went through the configuration file, want 1", c.changes)
	}
}

func TestCreatingAScenarioIsRefusedWhileTheConfigFileIsBroken(t *testing.T) {
	s, c := persisting(t)
	twoScenarios(t, s)
	c.fail = &config.Error{File: "faultline.yaml", Line: 9, Message: "this is not valid YAML"}

	w := do(t, s, http.MethodPost, "/api/scenarios", `{"name":"everything-slow","rules":["slow"]}`)

	wantStatus(t, w, http.StatusConflict)
	if list := decodeBody[[]Scenario](t, do(t, s, http.MethodGet, "/api/scenarios", "")); len(list) != 2 {
		t.Errorf("list = %+v, want nothing added when the change cannot be saved", list)
	}
}
