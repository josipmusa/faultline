package rules

import (
	"errors"
	"slices"
	"sync"
)

// ErrNoScenario is returned for a name no scenario has. The API boundary turns
// it into a 404, the same way ErrNotFound is turned into one for a rule.
var ErrNoScenario = errors.New("scenario not found")

// ErrScenarioExists is returned for a name a scenario already has. The API
// boundary turns it into a 409: a scenario is named once, and a second one
// under the same name would make the file ambiguous about which is which.
var ErrScenarioExists = errors.New("scenario already exists")

// Scenarios holds the scenarios a configuration file declared and remembers
// which one of them is on. It is safe for concurrent use.
//
// One scenario is active at a time in this release, so activating one is a
// single write to the rule store: the outgoing scenario's rules go off and the
// incoming one's go on together, and no request ever sees both. Nothing is
// reference counted. A rule's enabled flag is whatever the last write said,
// whoever wrote it, so a rule two scenarios name follows the newer of them and
// a rule turned on by hand is turned off by the scenario that names it.
//
// Which scenario is active lives here and nowhere else. It is per run state:
// the enabled flags an activation set are written to the configuration file
// like any other rule change, but the name of the scenario that set them is
// not, because the file is committed and a checkout should not arrive with a
// rehearsal already running.
type Scenarios struct {
	rules *Store

	mu     sync.Mutex
	list   []Scenario
	active string
}

// NewScenarios returns an empty scenario store over the rules it activates.
func NewScenarios(store *Store) *Scenarios {
	return &Scenarios{rules: store}
}

// List returns every scenario in the order the file declares them, together
// with the name of the active one, or "" when none is. Both come from under one
// lock, so the flag a caller derives cannot disagree with the list it came with.
func (s *Scenarios) List() ([]Scenario, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Scenario, len(s.list))
	for i, scenario := range s.list {
		out[i] = scenario.Clone()
	}
	return out, s.active
}

// Replace swaps the whole list, which is how the configuration file lands: once
// at startup and again on every reload. The active scenario stays active when
// the file still declares it; a file that no longer does drops it, and its name
// is returned so the caller can say so rather than leaving a name nothing
// answers to. The rules are not touched, because a reload has just applied the
// enabled flags the file itself carries.
func (s *Scenarios) Replace(list []Scenario) (dropped string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := make([]Scenario, len(list))
	for i, scenario := range list {
		next[i] = scenario.Clone()
	}
	s.list = next

	if s.active != "" && s.indexOf(s.active) < 0 {
		dropped, s.active = s.active, ""
	}
	return dropped
}

// Activate turns the scenario on: its rules are enabled and their behavior
// state starts over, so a rehearsal always begins at the first request. The
// scenario that was on, if it was another one, goes off in the same write.
func (s *Scenarios) Activate(name string) (Scenario, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.indexOf(name)
	if i < 0 {
		return Scenario{}, ErrNoScenario
	}

	var off []string
	if j := s.indexOf(s.active); s.active != name && j >= 0 {
		off = s.list[j].Rules
	}
	s.rules.Switch(s.list[i].Rules, off)
	s.active = name

	return s.list[i].Clone(), nil
}

// Deactivate turns the scenario's rules off. A scenario that was not the active
// one is not an error: its rules go off all the same, which is what was asked
// for, and whichever scenario is active stays active. Only one scenario can be
// on, so asking for one to be off twice does nothing the second time.
func (s *Scenarios) Deactivate(name string) (Scenario, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.indexOf(name)
	if i < 0 {
		return Scenario{}, ErrNoScenario
	}

	s.rules.Switch(nil, s.list[i].Rules)
	if s.active == name {
		s.active = ""
	}
	return s.list[i].Clone(), nil
}

// Add declares a new scenario, after the ones already declared. It touches no
// rule: creating a scenario says what a situation is, and turning it on is what
// puts it in force, so a scenario arrives inactive however its rules stand.
//
// Being written last is also what keeps it loadable. A rule may be written
// inside the scenario that names it, and the loader reads scenarios in order,
// so a scenario appended at the end can name any rule the file holds while one
// inserted before them could name a rule that does not exist yet.
func (s *Scenarios) Add(scenario Scenario) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.indexOf(scenario.Name) >= 0 {
		return ErrScenarioExists
	}
	s.list = append(s.list, scenario.Clone())
	return nil
}

// Forget drops a rule id from every scenario naming it. Deleting a rule through
// the API also removes it from the scenarios in the configuration file, or the
// file Faultline wrote would not load; what is in memory has to say the same.
func (s *Scenarios) Forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, scenario := range s.list {
		if !slices.Contains(scenario.Rules, id) {
			continue
		}
		kept := slices.Clone(scenario.Rules)
		s.list[i].Rules = slices.DeleteFunc(kept, func(r string) bool { return r == id })
	}
}

// indexOf reports the position of the scenario with that name, or -1. Callers
// hold the lock.
func (s *Scenarios) indexOf(name string) int {
	return slices.IndexFunc(s.list, func(sc Scenario) bool { return sc.Name == name })
}
