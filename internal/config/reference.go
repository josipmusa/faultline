package config

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Reference renders docs/reference.md: every key faultline.yaml accepts and
// every fault and behavior with its parameters and bounds. It reads the same
// object tree JSONSchema encodes, so a fault added to the catalogue reaches the
// documentation, the editor schema and the binary in one change. A test keeps
// the checked in copy current and `make docs` writes it.
func Reference() ([]byte, error) {
	doc := schemaDocument()
	defs, ok := doc["$defs"].(object)
	if !ok {
		return nil, fmt.Errorf("config: the schema has no $defs")
	}

	var b bytes.Buffer
	b.WriteString("# Configuration reference\n\n")
	b.WriteString("<!-- Generated from the fault catalogue by `make docs`. Edit internal/config/reference.go, not this file. -->\n\n")
	b.WriteString("Every key `faultline.yaml` accepts, and every fault and behavior a rule can\n")
	b.WriteString("name, with the bounds Faultline checks them against. [config.md](config.md)\n")
	b.WriteString("explains how the file is read and written; [faults.md](faults.md) shows each\n")
	b.WriteString("fault in use. The same faults and parameters are what the API, the CLI and\n")
	b.WriteString("the MCP tools accept, in the same shape.\n\n")

	b.WriteString("## Top level\n\n")
	writeProperties(&b, doc, defs, false, "routes", "bypass", "rules", "scenarios")

	for _, name := range []string{"route", "rule", "match", "scenario"} {
		def, ok := defs[name].(object)
		if !ok {
			return nil, fmt.Errorf("config: the schema has no definition %q", name)
		}
		fmt.Fprintf(&b, "## `%s`\n\n", name)
		if text, _ := def["description"].(string); text != "" {
			b.WriteString(text + "\n\n")
		}
		writeProperties(&b, def, defs, true, "id", "name", "enabled", "upstream", "port", "host", "method", "path", "header", "match", "fault", "behavior", "rules")
	}

	b.WriteString("## Faults\n\n")
	b.WriteString("A fault is written as its `type` and that type's parameters, side by side.\n")
	b.WriteString("No other key is accepted. A connection tier fault works on every request,\n")
	b.WriteString("encrypted traffic included; a response tier fault needs Faultline to see the\n")
	b.WriteString("request, so it does nothing to a host that stays at tier `encrypted`.\n\n")
	if err := writeVariants(&b, defs, "fault"); err != nil {
		return nil, err
	}

	b.WriteString("## Behaviors\n\n")
	b.WriteString("A behavior decides when a rule's fault applies. It is written the same way, as\n")
	b.WriteString("a `type` and its parameters, and a rule with no behavior applies its fault to\n")
	b.WriteString("every matching request. Enabling a rule, or activating a scenario that names\n")
	b.WriteString("it, starts its behavior over.\n\n")
	if err := writeVariants(&b, defs, "behavior"); err != nil {
		return nil, err
	}

	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// writeProperties renders one object's properties as a table, in the preferred
// order for the keys named and alphabetically for any other. A property that is
// a reference to another definition borrows that definition's description.
func writeProperties(b *bytes.Buffer, def, defs object, withRequired bool, preferred ...string) {
	props, _ := def["properties"].(object)
	required := requiredSet(def)

	if withRequired {
		b.WriteString("| Key | Type | Required | Description |\n| --- | --- | --- | --- |\n")
	} else {
		b.WriteString("| Key | Type | Description |\n| --- | --- | --- |\n")
	}
	for _, key := range orderedKeys(props, preferred) {
		prop, _ := props[key].(object)
		desc, _ := prop["description"].(string)
		if ref, ok := prop["$ref"].(string); ok && desc == "" {
			target, _ := defs[strings.TrimPrefix(ref, "#/$defs/")].(object)
			desc, _ = target["description"].(string)
		}
		if withRequired {
			fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", key, typeOf(prop), yesNo(required[key]), desc)
		} else {
			fmt.Fprintf(b, "| `%s` | %s | %s |\n", key, typeOf(prop), desc)
		}
	}
	b.WriteString("\n")
}

// writeVariants renders every branch of a catalogue definition, one heading per
// fault or behavior, with its description, its parameters and its pairs.
func writeVariants(b *bytes.Buffer, defs object, what string) error {
	def, ok := defs[what].(object)
	if !ok {
		return fmt.Errorf("config: the schema has no definition %q", what)
	}
	variants, _ := def["oneOf"].([]any)
	for _, v := range variants {
		variant, _ := v.(object)
		title, _ := variant["title"].(string)
		fmt.Fprintf(b, "### `%s`\n\n", title)
		if text, _ := variant["description"].(string); text != "" {
			b.WriteString(text + "\n\n")
		}

		props, _ := variant["properties"].(object)
		required := requiredSet(variant)
		// The parameters a user has to write come first, then the rest by name.
		params := orderedKeys(props, requiredKeys(variant))
		params = slices.DeleteFunc(params, func(k string) bool { return k == "type" })
		if len(params) == 0 {
			b.WriteString("Takes no parameters.\n\n")
		} else {
			b.WriteString("| Parameter | Type | Required | Description |\n| --- | --- | --- | --- |\n")
			for _, key := range params {
				prop, _ := props[key].(object)
				desc, _ := prop["description"].(string)
				fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", key, typeOf(prop), yesNo(required[key]), desc)
			}
			b.WriteString("\n")
		}
		for _, line := range pairSentences(variant) {
			b.WriteString(line + "\n\n")
		}
	}
	return nil
}

// pairSentences puts the allOf pairs into words: which two parameters go
// together, and whether both may be written.
func pairSentences(variant object) []string {
	pairs, _ := variant["allOf"].([]any)
	var out []string
	for _, p := range pairs {
		pair, _ := p.(object)
		for kind, alternatives := range pair {
			names := make([]string, 0, 2)
			alts, _ := alternatives.([]any)
			for _, a := range alts {
				alt, _ := a.(object)
				req, _ := alt["required"].([]string)
				names = append(names, req...)
			}
			sort.Strings(names)
			if len(names) != 2 {
				continue
			}
			switch kind {
			case "oneOf":
				out = append(out, fmt.Sprintf("Write `%s` or `%s`, not both.", names[0], names[1]))
			case "anyOf":
				out = append(out, fmt.Sprintf("Write at least one of `%s` and `%s`.", names[0], names[1]))
			}
		}
	}
	return out
}

// typeOf puts a property's shape and bounds into one cell.
func typeOf(prop object) string {
	if ref, ok := prop["$ref"].(string); ok {
		return "`" + strings.TrimPrefix(ref, "#/$defs/") + "`"
	}
	if alternatives, ok := prop["oneOf"].([]any); ok {
		parts := make([]string, 0, len(alternatives))
		for _, a := range alternatives {
			alt, _ := a.(object)
			parts = append(parts, typeOf(alt))
		}
		return strings.Join(parts, " or ")
	}
	kind, _ := prop["type"].(string)
	switch kind {
	case "integer":
		low, hasLow := prop["minimum"].(int)
		high, hasHigh := prop["maximum"].(int)
		switch {
		case hasLow && hasHigh:
			return fmt.Sprintf("integer, %d to %d", low, high)
		case hasLow:
			return fmt.Sprintf("integer, %d or more", low)
		case hasHigh:
			return fmt.Sprintf("integer, %d or less", high)
		}
		return "integer"
	case "string":
		if pattern, ok := prop["pattern"].(string); ok {
			return "string, matching `" + pattern + "`"
		}
		return "string"
	case "array":
		items, _ := prop["items"].(object)
		return "list of " + typeOf(items)
	case "object":
		if values, ok := prop["additionalProperties"].(object); ok {
			return "map of string to " + typeOf(values)
		}
		return "object"
	case "":
		return "any"
	}
	return kind
}

// orderedKeys sorts an object's keys: the preferred ones first, in the order
// given, then the rest alphabetically.
func orderedKeys(props object, preferred []string) []string {
	rank := map[string]int{}
	for i, key := range preferred {
		rank[key] = i + 1
	}
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, rj := rank[keys[i]], rank[keys[j]]
		if (ri == 0) != (rj == 0) {
			return ri != 0
		}
		if ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	return keys
}

// requiredKeys is the required list in a stable order.
func requiredKeys(def object) []string {
	required, _ := def["required"].([]string)
	out := slices.Clone(required)
	sort.Strings(out)
	return out
}

func requiredSet(def object) map[string]bool {
	out := map[string]bool{}
	required, _ := def["required"].([]string)
	for _, key := range required {
		out[key] = true
	}
	return out
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
