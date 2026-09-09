// Package events records one event per proxied request and streams events to
// live subscribers.
package events

import "time"

// Tier says how much of a request Faultline could see, which decides what
// faults were even possible. It is carried on every event so a fault that
// could not apply can explain itself rather than silently doing nothing.
type Tier string

const (
	// TierPlain is unencrypted traffic, fully visible.
	TierPlain Tier = "plain"
	// TierIntercepted is TLS traffic Faultline terminated, fully visible.
	TierIntercepted Tier = "intercepted"
	// TierEncrypted is TLS traffic passed through: the host is known, the
	// request and response are not. Only connection faults apply.
	TierEncrypted Tier = "encrypted"
)

// Event is one proxied request, recorded whether or not a fault applied.
type Event struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`

	// Host is the upstream the request was addressed to.
	Host   string `json:"host"`
	Method string `json:"method"`
	Path   string `json:"path"`

	// Status is the response status code, or 0 if the request never got one.
	Status int `json:"status"`
	// DurationMS is the round trip in milliseconds, including any injected
	// delay. Sub-millisecond round trips record as 0.
	DurationMS int64 `json:"duration_ms"`
	// BytesIn counts request body bytes sent to the upstream, BytesOut counts
	// response body bytes returned to the client.
	BytesIn  int64 `json:"bytes_in"`
	BytesOut int64 `json:"bytes_out"`

	// Faulted says a rule applied, and RuleID names it.
	Faulted bool   `json:"faulted"`
	RuleID  string `json:"rule_id,omitempty"`

	// Error says why the request never reached the upstream when that is
	// worth explaining, such as a client refusing the interception certificate.
	// Empty when the request went through.
	Error string `json:"error,omitempty"`

	// RetryOf names the attempt this request repeats, when it looks like one.
	// The recorder fills it in; it is not something a caller sets.
	RetryOf string `json:"retry_of,omitempty"`

	Tier Tier `json:"tier"`
}
