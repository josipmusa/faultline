package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	client "github.com/josipmusa/faultline/clients/go"
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
	if len(listed.Tools) != 13 {
		t.Errorf("/mcp offers %d tools, want 13", len(listed.Tools))
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

// newStdioSession runs the mcp command's own serving path over an in-memory
// transport pair, with nothing listening at the admin address, and returns the
// agent's end of it. The transport is the only thing stubbed: everything under
// it is the code `faultline mcp` runs.
func newStdioSession(ctx context.Context, t *testing.T, errOut io.Writer, adminPort, proxyPort int) (*sdk.ClientSession, chan error) {
	t.Helper()
	// A closed listener gives an address nothing is on, without guessing one.
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	return startAgent(ctx, t, errOut, closed.URL, adminPort, proxyPort)
}

// newStdioSessionAt is newStdioSession pointed at an instance that is running.
func newStdioSessionAt(ctx context.Context, t *testing.T, errOut io.Writer, adminAddr string) (*sdk.ClientSession, chan error) {
	t.Helper()
	return startAgent(ctx, t, errOut, adminAddr, 0, 0)
}

func startAgent(ctx context.Context, t *testing.T, errOut io.Writer, adminAddr string, adminPort, proxyPort int) (*sdk.ClientSession, chan error) {
	t.Helper()

	c, err := client.New(adminAddr)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	served := make(chan error, 1)
	go func() {
		served <- serveAgent(ctx, errOut, serverTransport, c, "", adminPort, proxyPort)
	}()

	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "v0"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting to the agent interface: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, served
}

// An agent should need no setup step of its own: with nothing running, the
// command brings an instance up, serves the tools over it, and takes it down
// again when the agent goes away.
func TestMCPStartsAnInstanceWhenThereIsNone(t *testing.T) {
	// The instance it starts creates the CA if there is none, so the test gets
	// a config directory of its own rather than the developer's.
	isolateConfigDir(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Port 0 everywhere, so the test never fights a real faultline; the banner
	// says where it actually landed.
	errOut := &syncWriter{}
	agent, served := newStdioSession(ctx, t, errOut, 0, 0)

	adminAddr := waitForAddr(t, errOut, "admin: http://")

	listed, err := agent.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) != 13 {
		t.Errorf("a standalone session offers %d tools, want 13", len(listed.Tools))
	}

	// The instance is a real one: the UI and the API are on the admin port
	// while the agent is attached, so a human can watch what it is doing.
	resp, err := http.Get("http://" + adminAddr + "/api/health")
	if err != nil {
		t.Fatalf("reading health from the instance the agent started: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/health = %d, want 200", resp.StatusCode)
	}

	// The agent going away takes the instance with it.
	cancel()
	if err := <-served; err != nil {
		t.Fatalf("mcp: %v", err)
	}
	gone, err := http.Get("http://" + adminAddr + "/api/health") //nolint:bodyclose // there is no body: the request is meant to fail
	if err == nil {
		_ = gone.Body.Close()
		t.Error("the admin port still answers after the agent disconnected")
	}
}

// With an instance already running the agent joins it rather than starting a
// second one, so its rules and the human's UI are the same instance.
func TestMCPAttachesToAnInstanceThatIsAlreadyRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	running := &syncWriter{}
	serveErr := make(chan error, 1)
	go func() { serveErr <- serve(ctx, running, nil, 0, 0, nil, nil, nil) }()
	adminAddr := waitForAddr(t, running, "admin: http://")

	errOut := &syncWriter{}
	agent, served := newStdioSessionAt(ctx, t, errOut, "http://"+adminAddr)

	// A rule added through the agent lands in the running instance, which is
	// the whole point of attaching rather than starting another one.
	if _, err := agent.CallTool(ctx, &sdk.CallToolParams{
		Name:      "add_rule",
		Arguments: map[string]any{"name": "attached", "match": map[string]any{"host": "api.stripe.com"}, "fault": map[string]any{"type": "delay", "ms": 5}},
	}); err != nil {
		t.Fatalf("add_rule: %v", err)
	}

	resp, err := http.Get("http://" + adminAddr + "/api/rules")
	if err != nil {
		t.Fatalf("reading rules: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), `"id":"attached"`) {
		t.Errorf("the running instance holds %s, want the rule the agent added", body)
	}

	// The agent leaving must not take an instance it did not start with it.
	cancel()
	if err := <-served; err != nil {
		t.Fatalf("mcp: %v", err)
	}
	if err := <-serveErr; err != nil {
		t.Fatalf("serve: %v", err)
	}
}
