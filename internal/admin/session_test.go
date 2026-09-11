package admin

import (
	"net/http"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

// rearmCount records how many times something asked the rules to be re-armed,
// standing in for the fault pipeline's gate, which internal/admin does not
// import.
type rearmCount struct{ calls int }

func (r *rearmCount) Reset() { r.calls++ }

func reset(t *testing.T, s *Server) {
	t.Helper()
	wantStatus(t, do(t, s, http.MethodPost, "/api/sessions/current/reset", ""), http.StatusNoContent)
}

func TestResetSessionClearsTheEvents(t *testing.T) {
	s := newTestServer(t)
	seed(t, s)

	reset(t, s)

	if got := len(s.events.Events()); got != 0 {
		t.Errorf("%d events after a reset, want 0", got)
	}
}

func TestResetSessionRearmsTheRules(t *testing.T) {
	s := newTestServer(t)
	gate := &rearmCount{}
	s.Rearms(gate)

	reset(t, s)

	if gate.calls != 1 {
		t.Errorf("the rules were re-armed %d times, want 1", gate.calls)
	}
}

// A server built without a gate is the one clients/go's harness builds. A
// reset clears what was observed and reports success, rather than refusing
// because there is no behavior state to forget.
func TestResetSessionWorksWithNoGate(t *testing.T) {
	s := newTestServer(t)
	seed(t, s)

	reset(t, s)

	if got := len(s.events.Events()); got != 0 {
		t.Errorf("%d events after a reset, want 0", got)
	}
}

// Re-arming a rule is not changing it. A reset between two runs of the same
// test has to leave the rules the agent or the human set up exactly as they
// are, or the next run measures a different situation.
func TestResetSessionLeavesTheRulesAlone(t *testing.T) {
	s := newTestServer(t)
	wantStatus(t, do(t, s, http.MethodPost, "/api/rules",
		`{"name":"slow stripe","match":{"host":"api.stripe.com"},"fault":{"type":"delay","ms":10}}`),
		http.StatusCreated)

	reset(t, s)

	got := s.rules.List()
	if len(got) != 1 || got[0].ID != "slow-stripe" || !got[0].Enabled {
		t.Errorf("rules after a reset = %+v, want the one enabled rule untouched", got)
	}
}

// Activating is a rule change; re-arming is not. The scenario a rehearsal is
// running under survives a reset, so an agent can reset between attempts
// without taking the situation down.
func TestResetSessionLeavesTheActiveScenarioAlone(t *testing.T) {
	s := newTestServer(t)
	wantStatus(t, do(t, s, http.MethodPost, "/api/rules",
		`{"name":"boom","match":{"host":"api.stripe.com"},"fault":{"type":"status","code":503}}`),
		http.StatusCreated)
	if err := s.scenarios.Add(rules.Scenario{Name: "outage", Rules: []string{"boom"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s.scenarios.Activate("outage"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	reset(t, s)

	if _, active := s.scenarios.List(); active != "outage" {
		t.Errorf("active scenario after a reset = %q, want %q", active, "outage")
	}
}
