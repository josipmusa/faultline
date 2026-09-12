// Package mcp exposes Faultline to a coding agent over the Model Context
// Protocol.
//
// The tools are a translation layer and nothing more. Every one of them is a
// call through clients/go to the HTTP API, which is the same door the UI and the
// CLI go through, so there is one place that mints rule ids, validates a fault
// and filters events, and an agent cannot reach a state the other three faces
// cannot describe.
//
// Two ways in, one tool set:
//
//   - `faultline mcp` speaks stdio to a running instance at its admin address.
//   - `/mcp` on the admin port serves the same tools in process, over a client
//     that dispatches straight into the admin handler rather than dialing the
//     socket it is already answering on.
package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	client "github.com/josipmusa/faultline/clients/go"
)

// Name identifies this server to an agent.
const Name = "faultline"

// NewServer returns an MCP server whose tools act through c. The version is the
// binary's own, passed in because it is stamped into the command at link time
// and this package must report the same one `faultline version` does.
func NewServer(c *client.Client, version string, opts ...Option) *sdk.Server {
	var cfg options
	for _, opt := range opts {
		opt(&cfg)
	}

	s := sdk.NewServer(&sdk.Implementation{Name: Name, Version: version}, &sdk.ServerOptions{
		Instructions: instructions,
	})

	addRuleTools(s, c)
	addEventTools(s, c)
	addScenarioTools(s, c)
	addSessionTools(s, c)
	addWrapTools(s, c, cfg.mirror)

	return s
}

// Option configures a server.
type Option func(*options)

type options struct{ mirror io.Writer }

// Mirrors echoes a wrapped command's output to w as it arrives, for a human
// watching the terminal the agent started Faultline in. It is never stdout in
// a stdio session, which is the protocol itself.
func Mirrors(w io.Writer) Option { return func(o *options) { o.mirror = w } }

// Serve runs the server on t until the agent goes away or ctx is cancelled.
// `faultline mcp` serves stdio, which is a transport like any other, so a test
// can drive the same server over an in-memory pair.
//
// An agent closing the session, and a Ctrl-C, are how this normally ends, so
// neither is reported as a failure: a command that prints an error every time it
// is used correctly teaches its user to ignore its errors.
func Serve(ctx context.Context, c *client.Client, version string, t sdk.Transport, opts ...Option) error {
	if err := NewServer(c, version, opts...).Run(ctx, t); !endedCleanly(err) {
		return err
	}
	return nil
}

// endedCleanly reports whether err is one of the ordinary ends of a stdio
// session rather than something that went wrong.
func endedCleanly(err error) bool {
	return err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled)
}

// Handler serves the same tools over streamable HTTP, for mounting at /mcp on
// the admin port. One server answers every session: the tools hold no
// per-session state, since all the state is Faultline's own.
func Handler(c *client.Client, version string) http.Handler {
	server := NewServer(c, version)
	return sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)
}

// InProcess returns a client that reaches h without a socket. It is how the
// admin port serves MCP over its own API: the alternative is the admin server
// dialing the address it is listening on, which would make an in-process call
// depend on its own port being reachable.
func InProcess(h http.Handler) (*client.Client, error) {
	// The address is never dialed; it only names the requests in errors.
	return client.New("http://faultline.internal", client.WithTransport(client.HandlerTransport(h)))
}

const instructions = `Faultline is a fault-injection proxy sitting between an application and its
dependencies. Use it to prove how the application behaves when a dependency is slow, broken or
unreachable, on real traffic.

The loop is: read list_upstreams to see which hosts the application actually calls, add_rule to
break one of them, exercise the application, then get_report and get_events to see what it did.
Call reset_session between two runs of the same check: it clears what was observed and re-arms
every rule, so a first_n fault applies again without you rebuilding it.

That loop is a summary, and these instructions are present whether or not they are wanted. If a
resilience-check skill is available to you, it is the method these tools are for: invoke it before
running a check rather than working from this paragraph, because it carries what this cannot - how
to bracket a timeout rather than guess it, what the retry numbers do and do not measure, and why a
faulted count of zero can mean the fault never applied rather than that the application coped.

Faultline never invents a response for an unreachable upstream. Every fault is applied to real
traffic, and a synthetic response always carries a Faultline-Fault header naming the rule that
produced it.`
