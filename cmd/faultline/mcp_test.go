package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestServeMountsTheAgentInterface proves /mcp answers a real MCP session over
// the admin port, over the in-process client rather than a second socket.
func TestServeMountsTheAgentInterface(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncWriter{}
	served := make(chan error, 1)
	go func() {
		served <- serve(ctx, out, nil, 0, 0, nil, nil, nil)
	}()

	adminAddr := waitForAddr(t, out, "admin: http://")

	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).
		Connect(ctx, &sdk.StreamableClientTransport{Endpoint: "http://" + adminAddr + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connecting to /mcp: %v", err)
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) != 12 {
		t.Errorf("/mcp offers %d tools, want 12", len(listed.Tools))
	}

	// A tool call has to reach the same instance the API does, so add one rule
	// through MCP and read it back over HTTP.
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "add_rule",
		Arguments: map[string]any{
			"name":  "through mcp",
			"match": map[string]any{"host": "api.stripe.com"},
			"fault": map[string]any{"type": "delay", "ms": 10},
		},
	}); err != nil {
		t.Fatalf("add_rule over /mcp: %v", err)
	}

	resp, err := http.Get("http://" + adminAddr + "/api/rules")
	if err != nil {
		t.Fatalf("reading rules: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), `"id":"through-mcp"`) {
		t.Errorf("the rule added over MCP is not in the API's rules:\n%s", body)
	}

	// The session goes before the shutdown: an agent that is still connected is
	// a connection the admin server waits for, which is what the shutdown
	// timeout is for.
	if err := session.Close(); err != nil {
		t.Errorf("closing the session: %v", err)
	}

	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// TestServeShutsDownPromptlyWithAnAgentAttached pins the cost of a Ctrl-C while
// an agent is connected to /mcp. A streamable session holds a stream open that
// never goes idle, so without the drain cancellation this took the whole ten
// second shutdown deadline and then returned an error instead of exiting
// cleanly.
func TestServeShutsDownPromptlyWithAnAgentAttached(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncWriter{}
	served := make(chan error, 1)
	go func() {
		served <- serve(ctx, out, nil, 0, 0, nil, nil, nil)
	}()

	adminAddr := waitForAddr(t, out, "admin: http://")

	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).
		Connect(ctx, &sdk.StreamableClientTransport{Endpoint: "http://" + adminAddr + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connecting to /mcp: %v", err)
	}
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	// The agent stays attached, the way it would when the user hits Ctrl-C.
	started := time.Now()
	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serve: %v, want a clean exit with an agent still attached", err)
	}
	if took := time.Since(started); took > 3*time.Second {
		t.Errorf("shutdown took %v with an agent attached, want it prompt", took)
	}
}
