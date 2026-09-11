package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/josipmusa/faultline/internal/faults"
)

// schemaOf reads a tool's input schema back as JSON, which is the form the
// agent sees it in.
func schemaOf(t *testing.T, h harness, tool string) map[string]any {
	t.Helper()

	listed, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var found *sdk.Tool
	for _, candidate := range listed.Tools {
		if candidate.Name == tool {
			found = candidate
		}
	}
	if found == nil {
		t.Fatalf("tool %s is not registered", tool)
	}

	encoded, err := json.Marshal(found.InputSchema)
	if err != nil {
		t.Fatalf("re-encoding the input schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatalf("decoding the input schema: %v", err)
	}
	return schema
}

// typeEnum digs out the values a tagged parameter's type may take.
func typeEnum(t *testing.T, schema map[string]any, param string) []string {
	t.Helper()

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the schema has no properties: %v", schema)
	}
	field, ok := properties[param].(map[string]any)
	if !ok {
		t.Fatalf("the schema has no %s property: %v", param, properties)
	}
	inner, ok := field["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s does not describe its properties: %v", param, field)
	}
	tagged, ok := inner["type"].(map[string]any)
	if !ok {
		t.Fatalf("%s does not describe its type: %v", param, inner)
	}
	listed, ok := tagged["enum"].([]any)
	if !ok {
		t.Fatalf("%s.type is not an enum: %v", param, tagged)
	}

	out := make([]string, 0, len(listed))
	for _, value := range listed {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("%s.type has a non-string in its enum: %v", param, value)
		}
		out = append(out, name)
	}
	return out
}

func TestAddRuleSchemaNamesEveryFault(t *testing.T) {
	h := newHarness(t)

	got := typeEnum(t, schemaOf(t, h, "add_rule"), "fault")

	if want := faults.Names(); !equalStrings(got, want) {
		t.Errorf("fault.type enum = %v, want the registry's %v", got, want)
	}
}

func TestAddRuleSchemaNamesEveryBehavior(t *testing.T) {
	h := newHarness(t)

	got := typeEnum(t, schemaOf(t, h, "add_rule"), "behavior")

	if want := faults.BehaviorNames(); !equalStrings(got, want) {
		t.Errorf("behavior.type enum = %v, want the registry's %v", got, want)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
