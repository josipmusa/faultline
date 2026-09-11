// Package client is a thin client for the Faultline admin API. It is the one
// place that knows the wire shape: the CLI talks to a running instance through
// it, and tests can drive Faultline the same way an application's own test
// suite would.
//
// The types are the ones the server sends, so a rule read here is the rule the
// API described, not a copy of its shape that can drift from it.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/josipmusa/faultline/internal/admin"
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/rules"
)

// DefaultAddr is where `faultline serve` puts the admin API.
const DefaultAddr = "http://localhost:9000"

// requestTimeout bounds an ordinary API call. The event stream is not an
// ordinary call and is bounded by its context instead.
const requestTimeout = 30 * time.Second

// maxResponseBytes caps a response body. A full ring of events is a few hundred
// kilobytes, so this only ever stops something that has gone wrong.
const maxResponseBytes = 32 << 20

// The types below are the server's own wire types, aliased here so a caller
// outside the module can name them without reaching into internal packages.
type (
	// Rule is a match, a fault, and optionally a behavior that gates it.
	Rule = rules.Rule
	// Match is the traffic a rule applies to.
	Match = rules.Match
	// Fault is what goes wrong, named by type with an open parameter bag.
	Fault = rules.Fault
	// FaultType names one fault in the catalogue.
	FaultType = rules.FaultType
	// Behavior says when a rule's fault applies.
	Behavior = rules.Behavior
	// BehaviorType names one behavior in the catalogue.
	BehaviorType = rules.BehaviorType
	// Params are the parameters of a fault or a behavior.
	Params = rules.Params
	// Scenario is a named group of rules, and whether it is the active one.
	Scenario = admin.Scenario
	// Event is one call that went through Faultline.
	Event = events.Event
	// Report is the current session's counts.
	Report = events.Report
	// Upstream is a host Faultline has seen, with its counts and its tier.
	Upstream = admin.Upstream
	// Message is one envelope from the event stream.
	Message = admin.Message
)

// Client talks to one running Faultline.
type Client struct {
	base *url.URL
	http *http.Client
}

// New returns a client for the admin API at addr. A bare host and port is
// taken as http, so both "localhost:9000" and "http://localhost:9000" work.
func New(addr string, opts ...Option) (*Client, error) {
	base, err := parseAddr(addr)
	if err != nil {
		return nil, err
	}
	c := &Client{base: base, http: &http.Client{Timeout: requestTimeout}}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Addr is the address this client talks to, as it will appear in errors.
func (c *Client) Addr() string { return c.base.String() }

func parseAddr(addr string) (*url.URL, error) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return nil, fmt.Errorf("the address is empty, want something like %s", DefaultAddr)
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%q is not an address: %w", addr, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("address %q needs an http or https scheme", addr)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("address %q has no host", addr)
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: strings.TrimSuffix(u.Path, "/")}, nil
}

// APIError is a refusal from the admin API: the message it wrote for a person,
// and the field it named when the problem was one field's.
type APIError struct {
	Status  int
	Message string
	Field   string
}

func (e *APIError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Field)
	}
	return e.Message
}

// UnreachableError says nothing answered at the address. It is the common
// mistake - no instance running - and deserves a sentence rather than a dial
// error.
type UnreachableError struct {
	Addr string
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("no Faultline is listening at %s; start one with `faultline serve`", e.Addr)
}

// get reads a JSON endpoint into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// do makes one API call. body is encoded as JSON when it is not nil, and out is
// decoded from the response when it is not nil.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("client: encoding the %s %s body: %w", method, path, err)
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, payload)
	if err != nil {
		return fmt.Errorf("client: building the %s %s request: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return c.reachError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		return apiErrorFrom(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("client: reading the %s %s response: %w", method, path, err)
	}
	return nil
}

// apiErrorFrom turns a refusal into an *APIError, falling back to the status
// line for a body that is not the documented error shape.
func apiErrorFrom(resp *http.Response) error {
	var wire struct {
		Message string `json:"error"`
		Field   string `json:"field"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err == nil && json.Unmarshal(body, &wire) == nil && wire.Message != "" {
		return &APIError{Status: resp.StatusCode, Message: wire.Message, Field: wire.Field}
	}
	return &APIError{
		Status:  resp.StatusCode,
		Message: fmt.Sprintf("faultline answered %s", strings.ToLower(resp.Status)),
	}
}

// reachError keeps a transport failure to one line. A cancelled context is the
// caller's own doing and travels unchanged.
func (c *Client) reachError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, syscall.ECONNREFUSED):
		return &UnreachableError{Addr: c.base.String()}
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("cannot reach faultline at %s: %w", c.base, urlErr.Err)
	}
	return err
}
