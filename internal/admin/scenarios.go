package admin

import (
	"encoding/json"
	"errors"
	"io"
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

// createScenario declares a new scenario. It is the one way a scenario is
// authored other than the configuration file, and it exists so that what
// somebody arranged by hand in the browser can be named, written to the file,
// and rehearsed again later.
//
// The rules a scenario names have to be there already: a scenario is a name for
// a set of rules, not a way to declare them. Creating one turns nothing on,
// because activating is what puts a situation in force, and doing it here would
// start the behavior state of every rule named over as a side effect of saving.
func (s *Server) createScenario(w http.ResponseWriter, r *http.Request) {
	scenario, err := decodeScenario(w, r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.validateScenario(scenario); err != nil {
		s.fail(w, err)
		return
	}

	if err := s.change(func() error { return s.scenarios.Add(scenario) }); err != nil {
		if errors.Is(err, rules.ErrScenarioExists) {
			s.fail(w, conflict("name", "a scenario named %q already exists", scenario.Name))
			return
		}
		s.fail(w, s.saved(err))
		return
	}
	s.writeJSON(w, http.StatusCreated, scenarioView(scenario, false))
}

// decodeScenario reads a scenario from the request body. Only what the file can
// declare is accepted: which scenario is active is per run state, so it is not
// something a caller may post.
func decodeScenario(w http.ResponseWriter, r *http.Request) (rules.Scenario, error) {
	var body struct {
		Name  string   `json:"name"`
		Rules []string `json:"rules"`
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			return rules.Scenario{}, invalid("", "body is empty, want a scenario object")
		}
		return rules.Scenario{}, invalid("", "body is not a valid scenario: %v", err)
	}
	return rules.Scenario{Name: body.Name, Rules: body.Rules}, nil
}

func (s *Server) validateScenario(scenario rules.Scenario) error {
	if scenario.Name == "" {
		return invalid("name", "a scenario needs a name, which is how it is turned on and off")
	}
	for _, id := range scenario.Rules {
		if _, err := s.rules.Get(id); err != nil {
			return invalid("rules", "there is no rule with id %q; a scenario names rules that already exist", id)
		}
	}
	return nil
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
