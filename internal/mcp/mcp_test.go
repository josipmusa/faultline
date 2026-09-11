package mcp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/josipmusa/faultline/internal/admin"
	"github.com/josipmusa/faultline/internal/events"
	faultmcp "github.com/josipmusa/faultline/internal/mcp"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/rules"
)

// harness is the agent's whole path: an SDK client session over the in-memory
// transport, a Faultline MCP server, and a real admin server under it reached
// the way the admin port reaches itself. Nothing here is a stand-in for the
// wire shape.
type harness struct {
	session   *sdk.ClientSession
	rules     *rules.Store
	scenarios *rules.Scenarios
	events    *events.Recorder
	gate      *rearmCount
}

type rearmCount struct{ calls int }

func (r *rearmCount) Reset() { r.calls++ }

func newHarness(t *testing.T) harness {
	t.Helper()
	ctx := context.Background()

	rec := events.NewRecorder(events.DefaultSize)
	t.Cleanup(rec.Close)

	store := rules.New()
	scenarios := rules.NewScenarios(store)

	bypass, err := forward.NewBypass(nil)
	if err != nil {
		t.Fatalf("bypass: %v", err)
	}

	api := admin.NewServer(store, scenarios, rec, bypass, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { _ = api.Shutdown(context.Background()) })
	gate := &rearmCount{}
	api.Rearms(gate)

	c, err := faultmcp.InProcess(api)
	if err != nil {
		t.Fatalf("InProcess: %v", err)
	}

	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	server := faultmcp.NewServer(c, "test")
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	sdkClient := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := sdkClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return harness{session: session, rules: store, scenarios: scenarios, events: rec, gate: gate}
}

// call invokes a tool and fails the test if the tool reported an error.
func (h harness) call(t *testing.T, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	res, err := h.session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) reported an error: %s", name, text(res))
	}
	return res
}

// callErr invokes a tool expecting it to refuse, and returns what it said.
func (h harness) callErr(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	res, err := h.session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("CallTool(%s) succeeded, want an error: %s", name, text(res))
	}
	return text(res)
}

// out decodes a tool's structured output.
func out[T any](t *testing.T, res *sdk.CallToolResult) T {
	t.Helper()
	encoded, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("re-encoding structured content: %v", err)
	}
	var v T
	if err := json.Unmarshal(encoded, &v); err != nil {
		t.Fatalf("decoding %s: %v", encoded, err)
	}
	return v
}

func text(res *sdk.CallToolResult) string {
	var s string
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			s += tc.Text
		}
	}
	return s
}

func TestEveryToolIsRegistered(t *testing.T) {
	h := newHarness(t)

	listed, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := make(map[string]bool, len(listed.Tools))
	for _, tool := range listed.Tools {
		got[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s has no input schema", tool.Name)
		}
	}

	for _, want := range []string{
		"list_upstreams", "list_rules", "add_rule", "remove_rule", "set_rule_enabled",
		"list_scenarios", "activate_scenario", "deactivate_scenario",
		"get_events", "wait_for_event", "get_report", "reset_session",
	} {
		if !got[want] {
			t.Errorf("tool %s is not registered", want)
		}
	}
	if len(listed.Tools) != 12 {
		t.Errorf("%d tools registered, want 12: %v", len(listed.Tools), got)
	}
}

// The wire format defines structuredContent as a record. A tool that answers
// with a bare JSON array there is valid against this SDK's own schema
// support, but fails a client that still enforces the object-only shape, so
// every list-shaped answer must come back wrapped in one.
func TestStructuredContentIsAlwaysAnObject(t *testing.T) {
	h := newHarness(t)
	h.call(t, "add_rule", delayRule("stripe down", "api.stripe.com", 10))
	h.record(t, "api.stripe.com", true)

	for _, tool := range []string{"list_rules", "list_upstreams", "get_events", "list_scenarios"} {
		res := h.call(t, tool, nil)
		encoded, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("%s: re-encoding structured content: %v", tool, err)
		}
		var obj map[string]any
		if err := json.Unmarshal(encoded, &obj); err != nil {
			t.Errorf("%s structuredContent = %s, want a JSON object: %v", tool, encoded, err)
		}
	}
}
