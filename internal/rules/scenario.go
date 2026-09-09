package rules

import "slices"

// Scenario is a named situation: the rules that go on and off together, named
// by the ids they are declared with, in the order the configuration file writes
// them.
//
// A scenario is an immutable value, like a rule. Which of them is on, and what
// activating one does to the rules, is the business of Scenarios.
type Scenario struct {
	Name  string
	Rules []string
}

// Clone returns a copy that shares no slice with the original.
func (s Scenario) Clone() Scenario {
	s.Rules = slices.Clone(s.Rules)
	return s
}
