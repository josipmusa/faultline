package admin

import (
	"errors"
	"net/http"

	"github.com/josipmusa/faultline/internal/rules"
)

// Scenario is a scenario on the wire: what the configuration file declared,
// plus whether this is the one that is on.
type Scenario struct {
	Name   string   `json:"name"`
	Rules  []string `json:"rules"`
	Active bool     `json:"active"`
}

func scenarioView(s rules.Scenario, active bool) Scenario {
	ids := s.Rules
	if ids == nil {
		ids = []string{} // a scenario with no rules is an empty list, not null
	}
	return Scenario{Name: s.Name, Rules: ids, Active: active}
}

func (s *Server) listScenarios(w http.ResponseWriter, _ *http.Request) {
	list, active := s.scenarios.List()

	out := make([]Scenario, 0, len(list))
	for _, scenario := range list {
		out = append(out, scenarioView(scenario, scenario.Name == active))
	}
	s.writeJSON(w, http.StatusOK, out)
}

func (s *Server) activateScenario(w http.ResponseWriter, r *http.Request) {
	s.switchScenario(w, r.PathValue("name"), true)
}

func (s *Server) deactivateScenario(w http.ResponseWriter, r *http.Request) {
	s.switchScenario(w, r.PathValue("name"), false)
}

// switchScenario turns a scenario on or off. Both are rule changes, so both go
// through the configuration file the way enabling a single rule does: the
// enabled flags an activation sets have to survive the next reload, or the next
// save of the file would quietly put the rehearsal back the way it was.
func (s *Server) switchScenario(w http.ResponseWriter, name string, on bool) {
	var scenario rules.Scenario

	err := s.change(func() error {
		var err error
		if on {
			scenario, err = s.scenarios.Activate(name)
		} else {
			scenario, err = s.scenarios.Deactivate(name)
		}
		return err
	})
	if err != nil {
		if errors.Is(err, rules.ErrNoScenario) {
			s.fail(w, missing("no scenario named %q", name))
			return
		}
		s.fail(w, s.saved(err))
		return
	}

	s.writeJSON(w, http.StatusOK, scenarioView(scenario, on))
}
