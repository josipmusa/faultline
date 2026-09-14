package config

import (
	"fmt"
	"maps"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
)

// ruleNode writes one rule as the YAML mapping the loader reads back. Keys are
// written in the order a person writes them, and enabled is always written out:
// the default depends on where the rule sits in the file, and a rule Faultline
// wrote should not change meaning if someone moves it.
func ruleNode(r rules.Rule) (*yaml.Node, error) {
	m := newMapping()

	m.add("id", scalarNode(r.ID))
	m.add("name", scalarNode(r.Name))

	enabled, err := valueNode(r.Enabled)
	if err != nil {
		return nil, err
	}
	m.add("enabled", enabled)

	if match, ok, matchErr := matchNode(r.Match); matchErr != nil {
		return nil, matchErr
	} else if ok {
		m.add("match", match)
	}

	fault, err := taggedNode("fault", string(r.Fault.Type), r.Fault.Params)
	if err != nil {
		return nil, err
	}
	m.add("fault", fault)

	if r.Behavior != nil {
		behavior, behaviorErr := taggedNode("behavior", string(r.Behavior.Type), r.Behavior.Params)
		if behaviorErr != nil {
			return nil, behaviorErr
		}
		m.add("behavior", behavior)
	}

	return m.node, nil
}

func matchNode(match rules.Match) (*yaml.Node, bool, error) {
	m := newMapping()
	for _, field := range []struct{ name, value string }{
		{"host", match.Host},
		{"method", match.Method},
		{"path", match.Path},
	} {
		if field.value != "" {
			m.add(field.name, scalarNode(field.value))
		}
	}
	if len(match.Header) > 0 {
		header, err := valueNode(match.Header)
		if err != nil {
			return nil, false, err
		}
		m.add("header", header)
	}
	if len(m.node.Content) == 0 {
		return nil, false, nil
	}
	return m.node, true, nil
}

// taggedNode writes a fault or a behavior: its type first, then its parameters
// in the order the catalogue declares them, which is the order the docs and
// the rule editor show them in and the order a person writes them - ms before
// jitter_ms, code before body. A parameter the catalogue does not know, which
// can only come from a newer binary, goes after them in name order. Either way
// the order is fixed, so the file does not churn between two saves.
func taggedNode(what, name string, params rules.Params) (*yaml.Node, error) {
	m := newMapping()
	m.add("type", scalarNode(name))

	for _, key := range paramOrder(what, name, params) {
		value, err := valueNode(params[key])
		if err != nil {
			return nil, fmt.Errorf("config: writing %s parameter %q: %w", what, key, err)
		}
		m.add(key, value)
	}
	return m.node, nil
}

// paramOrder is the order taggedNode writes params in: declaration order for
// the ones the catalogue declares, name order for the rest.
func paramOrder(what, name string, params rules.Params) []string {
	var declared []string
	switch what {
	case "fault":
		if f, ok := faults.Get(name); ok {
			declared = f.Schema().Names()
		}
	case "behavior":
		if b, ok := faults.GetBehavior(name); ok {
			declared = b.Schema().Names()
		}
	}

	order := make([]string, 0, len(params))
	for _, key := range declared {
		if _, set := params[key]; set {
			order = append(order, key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(params)) {
		if !slices.Contains(order, key) {
			order = append(order, key)
		}
	}
	return order
}

type mappingBuilder struct{ node *yaml.Node }

func newMapping() mappingBuilder {
	return mappingBuilder{node: &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}}
}

func (m mappingBuilder) add(key string, value *yaml.Node) {
	m.node.Content = append(m.node.Content, scalarNode(key), value)
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// valueNode leaves the shape of a value to the YAML encoder, which is what
// knows how to quote a string, write a number, or nest a map a fault parameter
// happens to hold.
func valueNode(value any) (*yaml.Node, error) {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return nil, fmt.Errorf("config: writing %v: %w", value, err)
	}
	return &node, nil
}
