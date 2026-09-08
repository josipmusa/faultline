// Package rules defines the fault rules Faultline applies to observed traffic.
//
// Rules are immutable values: every field is set at construction and updates
// return a new copy. Mutable collections of rules live in a store, not here.
package rules

import "maps"

// FaultType names one kind of fault. More types arrive in later stages.
type FaultType string

const (
	// FaultDelay holds the request for a while before it reaches the upstream.
	FaultDelay FaultType = "delay"
	// FaultStatus short-circuits the request with a synthetic status code.
	FaultStatus FaultType = "status"
)

// Fault is the single thing a rule does to traffic it matches. Which fields
// carry meaning depends on Type: MS and JitterMS for FaultDelay, Code and Body
// for FaultStatus.
type Fault struct {
	Type FaultType `json:"type"`

	// MS is the delay in milliseconds, for FaultDelay.
	MS int `json:"ms,omitempty"`
	// JitterMS spreads the delay over [MS, MS+JitterMS), for FaultDelay.
	JitterMS int `json:"jitter_ms,omitempty"`

	// Code is the synthetic status code, for FaultStatus.
	Code int `json:"code,omitempty"`
	// Body is the synthetic response body, for FaultStatus.
	Body string `json:"body,omitempty"`
}

// Match decides which traffic a rule affects. Every field is optional and an
// empty one matches anything; a request must satisfy all the non-empty ones.
type Match struct {
	// Host is the upstream host, compared case-insensitively.
	Host string `json:"host,omitempty"`
	// Method is the HTTP method, compared case-insensitively.
	Method string `json:"method,omitempty"`
	// Path is a glob over the request path. See MatchPath for the syntax.
	Path string `json:"path,omitempty"`
	// Header maps header names to the exact values a request must carry.
	// Names are case-insensitive, values are not.
	Header map[string]string `json:"header,omitempty"`
}

// Rule is a condition plus the fault applied to traffic meeting it.
type Rule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Match   Match  `json:"match"`
	Fault   Fault  `json:"fault"`
}

// WithEnabled returns a copy of the rule with Enabled set as given.
func (r Rule) WithEnabled(enabled bool) Rule {
	c := r.Clone()
	c.Enabled = enabled
	return c
}

// Clone returns a copy that shares nothing with the original, so neither side
// can observe the other's changes to the header map.
func (r Rule) Clone() Rule {
	c := r
	if r.Match.Header != nil {
		c.Match.Header = maps.Clone(r.Match.Header)
	}
	return c
}
