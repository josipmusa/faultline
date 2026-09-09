// Package config reads faultline.yaml: the file that declares the routes, the
// bypass list, the rules, and the scenarios a project wants, so a team can
// commit its outage rehearsals the way it commits CI configuration.
//
// Reading is strict. An unknown key, a fault parameter that does not exist, or
// two rules with one id are all errors, and every error names the file, the
// line, and the field, because a configuration file nobody can debug is worse
// than no configuration file. Nothing here touches the running system: Load
// returns a value, and the caller decides what to do with it.
package config

import (
	"github.com/josipmusa/faultline/internal/proxy/reverse"
	"github.com/josipmusa/faultline/internal/rules"
)

// Config is one configuration file, read and checked.
//
// Rules holds every rule in the file, including the ones written inline inside
// a scenario: a rule is a rule wherever it is declared, and a scenario names
// the ones it turns on. Ports left out of a route are zero here; assigning them
// is the caller's job, since only the caller knows what else is listening.
type Config struct {
	Path      string
	Routes    []reverse.Route
	Bypass    []string
	Rules     []rules.Rule
	Scenarios []Scenario

	// stamp is the file as it was when Load read it, which is how a later
	// change to it is noticed. It is empty for a configuration parsed from
	// bytes that never came from a file.
	stamp stamp
}

// Scenario is a named situation: a set of rules that go on and off together.
type Scenario struct {
	Name  string
	Rules []string // rule ids, in the order they are written
}
