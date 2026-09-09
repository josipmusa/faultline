package config

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/josipmusa/faultline/internal/rules"
)

var scenarioKeys = []string{"name", "rules"}

// decodeScenarios reads the named situations. A scenario lists rules either by
// id, when the rule is written at the top level and may belong to more than one
// scenario, or inline, when it exists only for this scenario. Inline rules
// start disabled: a scenario does nothing until it is activated.
func decodeScenarios(c cursor, set *ruleSet) ([]rules.Scenario, error) {
	items, err := c.sequence("scenarios")
	if err != nil {
		return nil, err
	}

	out := make([]rules.Scenario, 0, len(items))
	names := make(map[string]bool, len(items))

	for _, item := range items {
		m, mapErr := item.mapping("a scenario")
		if mapErr != nil {
			return nil, mapErr
		}
		if only := m.only("a scenario", scenarioKeys...); only != nil {
			return nil, only
		}

		nameAt, nameErr := m.require("name", "a scenario name")
		if nameErr != nil {
			return nil, nameErr
		}
		name, textErr := nameAt.text("a scenario name")
		if textErr != nil {
			return nil, textErr
		}
		if name == "" {
			return nil, nameAt.errf("a scenario name is required")
		}
		if names[name] {
			return nil, nameAt.errf("duplicate scenario name %q", name)
		}
		names[name] = true

		scenario := rules.Scenario{Name: name}
		if rulesAt, ok := m.value("rules"); ok {
			if scenario.Rules, err = decodeScenarioRules(rulesAt, set); err != nil {
				return nil, err
			}
		}
		out = append(out, scenario)
	}
	return out, nil
}

func decodeScenarioRules(c cursor, set *ruleSet) ([]string, error) {
	items, err := c.sequence("the rules of a scenario")
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(items))
	for _, item := range items {
		if item.node.Kind == yaml.MappingNode {
			rule, at, ruleErr := decodeRule(item, false)
			if ruleErr != nil {
				return nil, ruleErr
			}
			if addErr := set.add(rule, at); addErr != nil {
				return nil, addErr
			}
			out = append(out, rule.ID)
			continue
		}

		id, textErr := item.text("a rule id")
		if textErr != nil {
			return nil, textErr
		}
		if !set.has(id) {
			return nil, item.errf("there is no rule with id %q; write it under rules, or write it here in full", id)
		}
		out = append(out, id)
	}
	return out, nil
}
