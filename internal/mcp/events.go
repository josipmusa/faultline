package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	client "github.com/josipmusa/faultline/clients/go"
)

// defaultWaitMS and maxWaitMS bound how long a wait may run. An agent that
// forgets the timeout gets a wait that ends, and one that asks for an hour is
// told no rather than holding the session open.
const (
	defaultWaitMS = 10_000
	maxWaitMS     = 300_000
)

type getEventsInput struct {
	Limit   int    `json:"limit,omitempty" jsonschema:"keep only the most recent N; leave it out for everything Faultline still holds"`
	Host    string `json:"host,omitempty" jsonschema:"keep only calls to this upstream host"`
	Faulted *bool  `json:"faulted,omitempty" jsonschema:"true for only the calls a rule broke, false for only the untouched ones"`
}

type waitForEventInput struct {
	Host      string `json:"host,omitempty" jsonschema:"wait for a call to this upstream host"`
	Faulted   *bool  `json:"faulted,omitempty" jsonschema:"true to wait for a call a rule broke"`
	TimeoutMS int    `json:"timeout_ms,omitempty" jsonschema:"how long to wait in milliseconds, 10000 by default and 300000 at most"`
}

// waitResult says whether the call arrived, so "the application never made it"
// is an answer the agent can read rather than an error it has to interpret.
type waitResult struct {
	Matched  bool          `json:"matched" jsonschema:"whether a matching call arrived before the timeout"`
	Event    *client.Event `json:"event,omitempty" jsonschema:"the call that arrived, when one did"`
	WaitedMS int64         `json:"waited_ms" jsonschema:"how long the wait took in milliseconds"`
}

// upstreamsOutput and eventsOutput wrap their lists in an object: the wire
// format defines structuredContent as a record, and a bare array there fails
// a client that enforces that shape.
type upstreamsOutput struct {
	Upstreams []client.Upstream `json:"upstreams" jsonschema:"the upstream hosts Faultline has seen, with their counts and tier"`
}

type eventsOutput struct {
	Events []client.Event `json:"events" jsonschema:"the recorded calls, oldest first"`
}

func addEventTools(s *sdk.Server, c *client.Client) {
	sdk.AddTool(s, &sdk.Tool{
		Name: "list_upstreams",
		Description: "List the upstream hosts Faultline has seen the application call, with how many " +
			"requests each got, how many were faulted, how many failed, and the tier.\n\n" +
			"The tier says how much Faultline could see: plain is unencrypted HTTP, intercepted is " +
			"HTTPS it can read because the client trusts its CA, and encrypted is HTTPS it can only " +
			"tunnel. A response fault cannot apply to an encrypted host.\n\n" +
			"Start here: it tells you what the application actually calls, which is usually not what " +
			"its configuration suggests.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, upstreamsOutput, error) {
		list, err := c.Upstreams(ctx)
		return nil, upstreamsOutput{Upstreams: list}, err
	})

	sdk.AddTool(s, &sdk.Tool{
		Name: "get_events",
		Description: "Read the calls Faultline has recorded, oldest first. Each one carries the host, " +
			"method, path, status, duration, tier, whether a rule broke it and which rule did.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in getEventsInput) (*sdk.CallToolResult, eventsOutput, error) {
		list, err := c.Events(ctx, client.EventQuery{Limit: in.Limit, Host: in.Host, Faulted: in.Faulted})
		return nil, eventsOutput{Events: list}, err
	})

	sdk.AddTool(s, &sdk.Tool{
		Name: "wait_for_event",
		Description: "Wait for the application's next matching call and return it. Only calls recorded " +
			"after the wait begins count, so this answers \"did it call, now that I have broken it\" " +
			"rather than reading history.\n\n" +
			"A timeout is an answer, not a failure: matched comes back false, which usually means the " +
			"application made no such call.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in waitForEventInput) (*sdk.CallToolResult, waitResult, error) {
		timeout, err := in.timeout()
		if err != nil {
			return nil, waitResult{}, err
		}

		started := time.Now()
		event, err := c.WaitForEvent(ctx, client.EventQuery{Host: in.Host, Faulted: in.Faulted}, timeout)
		waited := time.Since(started).Milliseconds()

		switch {
		case errors.Is(err, client.ErrWaitTimeout):
			return nil, waitResult{Matched: false, WaitedMS: waited}, nil
		case err != nil:
			return nil, waitResult{}, err
		}
		return nil, waitResult{Matched: true, Event: &event, WaitedMS: waited}, nil
	})

	sdk.AddTool(s, &sdk.Tool{
		Name: "get_report",
		Description: "Summarise what the application did under the faults it has been given: how many " +
			"calls there were, how many Faultline broke, how many were retried, the longest a retry " +
			"waited, and how many attempts worth retrying were abandoned.\n\n" +
			"max_retry_wait_ms is the backoff the application actually used, which is the number to " +
			"quote when asked how long a user would have waited.\n\n" +
			"A warning names a rule that cannot do anything as things stand, such as a response-tier " +
			"fault on a host only ever seen encrypted. Read one beside a faulted count of zero as the " +
			"fault never having applied, not as the application coping with it.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ noInput) (*sdk.CallToolResult, client.ReportResult, error) {
		report, err := c.Report(ctx)
		return nil, report, err
	})
}

func (in waitForEventInput) timeout() (time.Duration, error) {
	ms := in.TimeoutMS
	switch {
	case ms == 0:
		ms = defaultWaitMS
	case ms < 0:
		return 0, fmt.Errorf("timeout_ms must be positive, not %d", ms)
	case ms > maxWaitMS:
		return 0, fmt.Errorf("timeout_ms must be %d or less, not %d; wait again if the call may take longer",
			maxWaitMS, ms)
	}
	return time.Duration(ms) * time.Millisecond, nil
}
