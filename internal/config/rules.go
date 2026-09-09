package config

import (
	"errors"

	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
)

var (
	ruleKeys  = []string{"id", "name", "enabled", "match", "fault", "behavior"}
	matchKeys = []string{"host", "method", "path", "header"}
)

// ruleSet collects the rules of a file as they are read, wherever they are
// written. It holds the ids so that two rules with one id are caught at the
// second one, which is the line the user has to change.
type ruleSet struct {
	rules []rules.Rule
	ids   map[string]bool
}

func (s *ruleSet) add(r rules.Rule, at cursor) error {
	if s.ids[r.ID] {
		return at.errf("duplicate rule id %q; ids name a rule everywhere else, so each one is used once", r.ID)
	}
	s.ids[r.ID] = true
	s.rules = append(s.rules, r)
	return nil
}

func (s *ruleSet) has(id string) bool { return s.ids[id] }

// decodeRules reads the top level rules. They are enabled unless the file says
// otherwise: a rule written at the top level is one the user wants applied,
// while a rule written inside a scenario waits for the scenario.
func decodeRules(c cursor, set *ruleSet) error {
	items, err := c.sequence("rules")
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := decodeRuleInto(item, set, true); err != nil {
			return err
		}
	}
	return nil
}

func decodeRuleInto(c cursor, set *ruleSet, enabled bool) error {
	rule, at, err := decodeRule(c, enabled)
	if err != nil {
		return err
	}
	return set.add(rule, at)
}

// decodeRule reads one rule and returns it with the cursor for its id, which is
// where a duplicate is reported.
func decodeRule(c cursor, enabled bool) (rules.Rule, cursor, error) {
	m, err := c.mapping("a rule")
	if err != nil {
		return rules.Rule{}, c, err
	}
	if err := m.only("a rule", ruleKeys...); err != nil {
		return rules.Rule{}, c, err
	}

	idAt, err := m.require("id", "an id")
	if err != nil {
		return rules.Rule{}, idAt, err
	}
	id, err := idAt.text("an id")
	if err != nil {
		return rules.Rule{}, idAt, err
	}
	if id == "" {
		return rules.Rule{}, idAt, idAt.errf("an id is required")
	}

	nameAt, err := m.require("name", "a name")
	if err != nil {
		return rules.Rule{}, idAt, err
	}
	name, err := nameAt.text("a name")
	if err != nil {
		return rules.Rule{}, idAt, err
	}

	rule := rules.Rule{ID: id, Name: name, Enabled: enabled}

	if enabledAt, ok := m.value("enabled"); ok {
		if rule.Enabled, err = enabledAt.boolean("enabled"); err != nil {
			return rules.Rule{}, idAt, err
		}
	}
	if matchAt, ok := m.value("match"); ok {
		if rule.Match, err = decodeMatch(matchAt); err != nil {
			return rules.Rule{}, idAt, err
		}
	}

	faultAt, err := m.require("fault", "a fault")
	if err != nil {
		return rules.Rule{}, idAt, err
	}
	faultType, params, err := decodeTagged(faultAt, "a fault")
	if err != nil {
		return rules.Rule{}, idAt, err
	}
	rule.Fault = rules.Fault{Type: rules.FaultType(faultType), Params: params}

	if behaviorAt, ok := m.value("behavior"); ok {
		behaviorType, behaviorParams, behaviorErr := decodeTagged(behaviorAt, "a behavior")
		if behaviorErr != nil {
			return rules.Rule{}, idAt, behaviorErr
		}
		rule.Behavior = &rules.Behavior{Type: rules.BehaviorType(behaviorType), Params: behaviorParams}
	}

	if err := validateRule(m, rule); err != nil {
		return rules.Rule{}, idAt, err
	}
	return rule, idAt, nil
}

func decodeMatch(c cursor) (rules.Match, error) {
	m, err := c.mapping("a match")
	if err != nil {
		return rules.Match{}, err
	}
	if err := m.only("a match", matchKeys...); err != nil {
		return rules.Match{}, err
	}

	var match rules.Match
	for _, field := range []struct {
		name string
		into *string
	}{
		{"host", &match.Host},
		{"method", &match.Method},
		{"path", &match.Path},
	} {
		at, ok := m.value(field.name)
		if !ok {
			continue
		}
		value, textErr := at.text(field.name)
		if textErr != nil {
			return rules.Match{}, textErr
		}
		*field.into = value
	}

	if at, ok := m.value("header"); ok {
		if match.Header, err = at.textMap("header"); err != nil {
			return rules.Match{}, err
		}
	}
	return match, nil
}

// decodeTagged reads a fault or a behavior: a type, and whatever parameters
// that type takes. The parameters are not checked here; the catalogue in
// internal/faults owns what each type accepts, and validateRule asks it.
func decodeTagged(c cursor, what string) (string, rules.Params, error) {
	m, err := c.mapping(what)
	if err != nil {
		return "", nil, err
	}
	typeAt, err := m.require("type", "a type")
	if err != nil {
		return "", nil, err
	}
	name, err := typeAt.text("a type")
	if err != nil {
		return "", nil, err
	}

	var params rules.Params
	for _, key := range m.names {
		if key == "type" {
			continue
		}
		value, valueErr := m.c.field(key, m.values[key]).plain(key)
		if valueErr != nil {
			return "", nil, valueErr
		}
		if params == nil {
			params = rules.Params{}
		}
		params[key] = value
	}
	return name, params, nil
}

// validateRule asks the owners of each part of a rule whether it is right, then
// turns the field they name back into the line it is written on. Nothing about
// what makes a rule valid lives here, so a new fault or a new match field needs
// no change in this file.
func validateRule(m *mapping, r rules.Rule) error {
	if err := located(m, rules.Validate(r)); err != nil {
		return err
	}
	if err := located(m, faults.Validate(r.Fault)); err != nil {
		return err
	}
	if r.Behavior == nil {
		return nil
	}
	return located(m, faults.ValidateBehavior(*r.Behavior))
}

func located(m *mapping, err error) error {
	var pe *faults.ParamError
	if errors.As(err, &pe) {
		return m.locate(pe.Path()).errf("%s", pe.Message)
	}
	var fe *rules.FieldError
	if errors.As(err, &fe) {
		return m.locate(fe.Field).errf("%s", fe.Message)
	}
	return err
}
