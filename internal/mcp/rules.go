package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/faults"
)

type noInput struct{}

// matchInput is the traffic a rule applies to. Every field is optional and an
// empty one matches anything, which is why a rule with an empty match breaks
// every call the application makes.
type matchInput struct {
	Host   string            `json:"host,omitempty" jsonschema:"the upstream host, such as api.stripe.com; matched case-insensitively, and an empty host matches every host"`
	Method string            `json:"method,omitempty" jsonschema:"the HTTP method, such as POST; empty matches any method"`
	Path   string            `json:"path,omitempty" jsonschema:"a glob over the path, such as /v1/charges/* for one segment or /v1/** for any depth; empty matches any path"`
	Header map[string]string `json:"header,omitempty" jsonschema:"header names and the exact values they must have"`
}

type addRuleInput struct {
	Name     string         `json:"name" jsonschema:"a short name for the rule, which is also where its id comes from"`
	ID       string         `json:"id,omitempty" jsonschema:"an explicit id; leave it out and Faultline derives one from the name"`
	Enabled  *bool          `json:"enabled,omitempty" jsonschema:"whether the rule is in force; it is on unless you say otherwise"`
	Match    matchInput     `json:"match" jsonschema:"which traffic the fault applies to"`
	Fault    map[string]any `json:"fault" jsonschema:"the fault, as a type and its parameters together, such as {\"type\":\"delay\",\"ms\":2000}"`
	Behavior map[string]any `json:"behavior,omitempty" jsonschema:"an optional gate on when the fault applies, such as {\"type\":\"first_n\",\"n\":2}; without one the fault applies to everything the rule matches"`
}

type ruleIDInput struct {
	ID string `json:"id" jsonschema:"the id of the rule"`
}

type setRuleEnabledInput struct {
	ID      string `json:"id" jsonschema:"the id of the rule"`
	Enabled bool   `json:"enabled" jsonschema:"true to put the rule in force, false to take it out; either way the rule's behavior state starts over"`
}

type removedRule struct {
	Removed string `json:"removed" jsonschema:"the id of the rule that is gone"`
}

// rulesOutput wraps the rule list in an object: the wire format defines
// structuredContent as a record, and a bare array there fails a client that
// enforces that shape. It rides through list_rules' "any" output slot
// rather than becoming its declared Out type, for the same reason list_rules
// declares no output schema at all: a schema inferred from client.Rule
// cannot describe Fault and Behavior's flat, tagged wire shape.
type rulesOutput struct {
	Rules []client.Rule `json:"rules"`
}

// A rule's fault and behavior marshal to a flat `{"type":..., ...params}`
// object through their own MarshalJSON, which no schema inferred from the Go
// struct can describe. So the tools that answer with a rule declare no output
// schema, rather than this package carrying a second description of the rule's
// wire shape for one to be inferred from. The structured output is still the
// rule, exactly as the API wrote it.
func addRuleTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{
		Name: "list_rules",
		Description: "List the fault rules Faultline holds, in the order it applies them. " +
			"The first rule whose match and behavior both accept a request is the one that breaks it.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, any, error) {
		list, err := c.Rules(ctx)
		if err != nil {
			return nil, nil, err
		}
		return nil, rulesOutput{Rules: list}, nil
	})

	sdk.AddTool(s, &sdk.Tool{
		Name: "add_rule",
		Description: "Add a fault rule and put it in force. Returns the rule as stored, which is " +
			"where an id Faultline derived from the name appears.\n\n" +
			"A rule is a match, one fault, and optionally a behavior gating when that fault applies. " +
			"Narrow the match to the host you mean: an empty match breaks every call the application makes.\n\n" +
			faultCatalogue(),
		InputSchema: addRuleSchema(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in addRuleInput) (*sdk.CallToolResult, any, error) {
		rule, err := in.rule()
		if err != nil {
			return nil, nil, err
		}
		stored, err := c.AddRule(ctx, rule)
		if err != nil {
			return nil, nil, err
		}
		return nil, stored, nil
	})

	sdk.AddTool(s, &sdk.Tool{
		Name: "remove_rule",
		Description: "Delete a fault rule. Traffic it was breaking goes back to normal, and the rule " +
			"is dropped from any scenario that named it.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in ruleIDInput) (*sdk.CallToolResult, removedRule, error) {
		if err := c.DeleteRule(ctx, in.ID); err != nil {
			return nil, removedRule{}, err
		}
		return nil, removedRule{Removed: in.ID}, nil
	})

	sdk.AddTool(s, &sdk.Tool{
		Name: "set_rule_enabled",
		Description: "Turn a rule on or off without deleting it. This also starts the rule's behavior " +
			"state over, so a spent first_n applies again.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in setRuleEnabledInput) (*sdk.CallToolResult, any, error) {
		rule, err := c.SetRuleEnabled(ctx, in.ID, in.Enabled)
		if err != nil {
			return nil, nil, err
		}
		return nil, rule, nil
	})
}

// rule turns the tool's input into the rule the API takes. The fault and the
// behavior are decoded by the domain's own UnmarshalJSON rather than picked
// apart here, so a fault reaches the API through this door in exactly the shape
// it reaches it through the others.
func (in addRuleInput) rule() (client.Rule, error) {
	rule := client.Rule{
		ID:      in.ID,
		Name:    in.Name,
		Enabled: in.Enabled == nil || *in.Enabled,
		Match: client.Match{
			Host:   in.Match.Host,
			Method: in.Match.Method,
			Path:   in.Match.Path,
			Header: in.Match.Header,
		},
	}

	if err := decodeTagged(in.Fault, &rule.Fault, "fault"); err != nil {
		return client.Rule{}, err
	}
	if len(in.Behavior) > 0 {
		var behavior client.Behavior
		if err := decodeTagged(in.Behavior, &behavior, "behavior"); err != nil {
			return client.Rule{}, err
		}
		rule.Behavior = &behavior
	}
	return rule, nil
}

// decodeTagged re-encodes one of the open parameter maps and reads it back
// through the domain type, which is the one place that knows the flat
// `{"type":..., ...params}` shape.
func decodeTagged(params map[string]any, into any, what string) error {
	if len(params) == 0 {
		return fmt.Errorf("%s is missing; give it as a type and its parameters together, "+
			"such as {\"type\":\"delay\",\"ms\":2000}", what)
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("%s cannot be read: %w", what, err)
	}
	if err := json.Unmarshal(encoded, into); err != nil {
		return fmt.Errorf("%s cannot be read: %w", what, err)
	}
	return nil
}

// addRuleSchema is the inferred schema for the tool's input, with the two open
// parameter bags narrowed to the types the registry actually has. A fault's
// parameters cannot go in the schema: which ones exist depends on the type, and
// a oneOf over every fault refuses a bad call without saying which fault or
// which field was wrong, which is worse for the agent than the catalogue in the
// description plus Faultline's own refusal. The type itself is a closed set,
// so that much belongs here, where it is checked before a round trip and the
// refusal names every alternative.
func addRuleSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[addRuleInput](nil)
	if err != nil {
		// Only a change to addRuleInput can cause this, so it is a mistake in
		// Faultline rather than in anyone's input.
		panic(fmt.Sprintf("mcp: describing add_rule's input: %v", err))
	}

	schema.Properties["fault"] = taggedSchema(schema.Properties["fault"], faults.Names(),
		"which fault this is; its other parameters depend on this, and the tool description lists them")
	schema.Properties["behavior"] = taggedSchema(schema.Properties["behavior"], faults.BehaviorNames(),
		"which behavior this is; its other parameters depend on this, and the tool description lists them")

	return schema
}

// taggedSchema narrows one open parameter bag to the types on offer, keeping
// the description the field already carried. The parameters beyond the type are
// left open, which is what makes the bag a bag.
func taggedSchema(open *jsonschema.Schema, names []string, about string) *jsonschema.Schema {
	allowed := make([]any, len(names))
	for i, name := range names {
		allowed[i] = name
	}

	return &jsonschema.Schema{
		Type:        "object",
		Description: open.Description,
		Required:    []string{"type"},
		Properties: map[string]*jsonschema.Schema{
			"type": {Type: "string", Enum: allowed, Description: about},
		},
	}
}
