package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
)

// SchemaID is where the published schema lives, and what the
// `# yaml-language-server` line at the top of a configuration file points at so
// an editor validates the file while it is being written.
const SchemaID = "https://raw.githubusercontent.com/josipmusa/faultline/main/schema/faultline.schema.json"

type object = map[string]any

// JSONSchema describes faultline.yaml for editors and other tools. The parts
// that say what a fault or a behavior takes are built from the catalogue in
// internal/faults, so a new fault appears in the published schema without
// anyone writing it out a second time. The checked in copy at
// schema/faultline.schema.json is this, and a test keeps the two the same.
func JSONSchema() ([]byte, error) {
	doc := object{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  SchemaID,
		"title":                "Faultline configuration",
		"description":          "The routes, bypass list, rules, and scenarios of one Faultline setup.",
		"type":                 "object",
		"additionalProperties": false,
		"properties": object{
			"routes": object{
				"description": "Explicit routes: an upstream reachable on a local port, for clients that ignore proxy settings.",
				"type":        "array",
				"items":       ref("route"),
			},
			"bypass": object{
				"description": "Hosts the forward proxy passes through untouched: no rules, no events, no interception.",
				"type":        "array",
				"items": object{
					"type":        "string",
					"description": "A host (api.stripe.com), a host and port (localhost:9000), or a domain suffix (*.internal).",
					"minLength":   1,
				},
			},
			"rules": object{
				"description": "Rules applied to matching traffic. A rule at the top level is on unless it says otherwise.",
				"type":        "array",
				"items":       ref("rule"),
			},
			"scenarios": object{
				"description": "Named situations: sets of rules that go on and off together.",
				"type":        "array",
				"items":       ref("scenario"),
			},
		},
		"$defs": object{
			"route":    routeSchema(),
			"rule":     ruleSchema(),
			"match":    matchSchema(),
			"scenario": scenarioSchema(),
			"fault":    catalogueSchema("fault", faultVariants()),
			"behavior": catalogueSchema("behavior", behaviorVariants()),
		},
	}

	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("config: writing the JSON Schema: %w", err)
	}
	return out.Bytes(), nil
}

func ref(name string) object { return object{"$ref": "#/$defs/" + name} }

func routeSchema() object {
	return object{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"name", "upstream"},
		"properties": object{
			"name":     object{"type": "string", "minLength": 1, "description": "What this route is called."},
			"upstream": object{"type": "string", "pattern": `^https?://`, "description": "The upstream, as an absolute URL: https://api.stripe.com."},
			"port": object{
				"type":        "integer",
				"minimum":     1,
				"maximum":     65535,
				"description": "The local port to listen on. Left out, Faultline assigns one from 9100 up.",
			},
		},
	}
}

func ruleSchema() object {
	return object{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"id", "name", "fault"},
		"properties": object{
			"id": object{
				"type":        "string",
				"pattern":     `^[A-Za-z0-9._-]+$`,
				"maxLength":   rules.MaxIDLen,
				"description": "How everything else names this rule.",
			},
			"name":     object{"type": "string", "minLength": 1, "description": "What this rule is for, in a few words."},
			"enabled":  object{"type": "boolean", "description": "Whether the rule applies. Rules written inside a scenario default to false."},
			"match":    ref("match"),
			"fault":    ref("fault"),
			"behavior": ref("behavior"),
		},
	}
}

func matchSchema() object {
	return object{
		"description":          "Which requests the rule applies to. Everything left out matches everything.",
		"type":                 "object",
		"additionalProperties": false,
		"properties": object{
			"host":   object{"type": "string", "description": "A hostname like api.stripe.com, not a URL."},
			"method": object{"type": "string", "pattern": `^[A-Za-z]+$`, "description": "An HTTP method like GET or POST."},
			"path":   object{"type": "string", "pattern": `^[/*]`, "description": "A path, with * standing in for the rest: /v1/charges/*."},
			"header": object{
				"type":                 "object",
				"additionalProperties": object{"type": "string"},
				"minProperties":        1,
				"description":          "Header names and the values they must have.",
			},
		},
	}
}

func scenarioSchema() object {
	return object{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"name"},
		"properties": object{
			"name": object{"type": "string", "minLength": 1, "description": "How the scenario is turned on and off."},
			"rules": object{
				"type":        "array",
				"description": "The rules this scenario turns on, named by id or written out in full.",
				"items": object{
					"oneOf": []any{
						object{"type": "string", "description": "The id of a rule declared under rules."},
						ref("rule"),
					},
				},
			},
		},
	}
}

// catalogueSchema turns the registered faults, or behaviors, into one choice
// per type. The type is a constant in each branch, so an editor can tell which
// branch a mapping was meant to be and complain about the right parameter.
func catalogueSchema(what string, variants []any) object {
	return object{
		"description": "The " + what + " and its parameters. The type says which parameters there are.",
		"type":        "object",
		"required":    []string{"type"},
		"oneOf":       variants,
	}
}

func faultVariants() []any {
	out := make([]any, 0, len(faults.All()))
	for _, f := range faults.All() {
		description := fmt.Sprintf("A %s tier fault.", f.Tier())
		out = append(out, variantSchema(f.Name(), description, f.Schema()))
	}
	return out
}

func behaviorVariants() []any {
	out := make([]any, 0, len(faults.AllBehaviors()))
	for _, b := range faults.AllBehaviors() {
		out = append(out, variantSchema(b.Name(), "", b.Schema()))
	}
	return out
}

func variantSchema(name, description string, schema faults.Schema) object {
	properties := object{"type": object{"const": name}}
	required := []string{"type"}

	for _, field := range schema {
		property := fieldSchema(field)
		if text := field.Description(); text != "" {
			property["description"] = text
		}
		properties[field.Name()] = property
		if field.IsRequired() {
			required = append(required, field.Name())
		}
	}

	variant := object{
		"title":                name,
		"type":                 "object",
		"additionalProperties": false,
		"required":             required,
		"properties":           properties,
	}
	if description != "" {
		variant["description"] = description
	}
	if pairs := pairSchemas(schema); len(pairs) > 0 {
		variant["allOf"] = pairs
	}
	return variant
}

// pairSchemas carries over the parameters that are written in pairs: either at
// least one of the two, or exactly one, depending on what the fault declared.
func pairSchemas(schema faults.Schema) []any {
	var out []any
	seen := map[string]bool{}

	for _, field := range schema {
		partner, exclusive := field.Partner()
		if partner == "" || seen[field.Name()] {
			continue
		}
		seen[field.Name()], seen[partner] = true, true

		choice := []any{
			object{"required": []string{field.Name()}},
			object{"required": []string{partner}},
		}
		branch := "anyOf"
		if exclusive {
			branch = "oneOf"
		}
		out = append(out, object{branch: choice})
	}
	return out
}

func fieldSchema(field faults.Field) object {
	switch field.Kind() {
	case faults.KindInt:
		out := object{"type": "integer"}
		if low, ok := field.MinValue(); ok {
			out["minimum"] = low
		}
		if high, ok := field.MaxValue(); ok {
			out["maximum"] = high
		}
		return out
	case faults.KindStr:
		out := object{"type": "string"}
		if chars := field.AllowedChars(); chars != "" {
			out["pattern"] = charPattern(chars)
		}
		return out
	case faults.KindStrMap:
		return object{"type": "object", "additionalProperties": object{"type": "string"}, "minProperties": 1}
	case faults.KindStrList:
		return object{"type": "array", "items": object{"type": "string", "minLength": 1}, "minItems": 1}
	default:
		return object{}
	}
}

// charPattern spells an alphabet as a regular expression. The catalogue
// compares letters case insensitively, so both cases are written out rather
// than relying on a flag JSON Schema does not have.
func charPattern(chars string) string {
	var b strings.Builder
	for _, r := range chars {
		b.WriteString(strings.ToUpper(string(r)))
		b.WriteString(strings.ToLower(string(r)))
	}
	return "^[" + b.String() + "]+$"
}
