// Package rules defines the fault rules Faultline applies to observed traffic.
//
// Rules are immutable values: every field is set at construction and updates
// return a new copy. Mutable collections of rules live in a store, not here.
package rules

import (
	"maps"
)

// FaultType names one kind of fault. The catalogue of types lives in
// internal/faults, not here: this package carries a rule around, it does not
// know what any fault does.
type FaultType string

// Fault is the single thing a rule does to traffic it matches.
type Fault struct {
	Type   FaultType
	Params Params
}

// UnmarshalJSON reads the flat wire shape, `{"type":"delay","ms":2000}`, into a
// type and a bag of parameters.
func (f *Fault) UnmarshalJSON(data []byte) error {
	name, params, err := decodeTagged(data)
	if err != nil {
		return err
	}
	*f = Fault{Type: FaultType(name), Params: params}
	return nil
}

// MarshalJSON writes the flat wire shape back, with the type first and the
// parameters after it in a stable order.
func (f Fault) MarshalJSON() ([]byte, error) {
	return encodeTagged("fault", string(f.Type), f.Params)
}

// BehaviorType names one kind of behavior. As with a fault, the catalogue that
// knows what each type does lives in internal/faults.
type BehaviorType string

// Behavior makes a fault stateful over time. It decides, for each request the
// rule matches, whether the fault applies at all: a share of them, the first
// few, those inside a window, or a repeating pattern. A rule without a behavior
// applies its fault to everything it matches.
type Behavior struct {
	Type   BehaviorType
	Params Params
}

// UnmarshalJSON reads the flat wire shape, `{"type":"first_n","n":2}`.
func (b *Behavior) UnmarshalJSON(data []byte) error {
	name, params, err := decodeTagged(data)
	if err != nil {
		return err
	}
	*b = Behavior{Type: BehaviorType(name), Params: params}
	return nil
}

// MarshalJSON writes the flat wire shape back, parameters in a stable order.
func (b Behavior) MarshalJSON() ([]byte, error) {
	return encodeTagged("behavior", string(b.Type), b.Params)
}

// Clone returns a copy that shares no parameters with the original.
func (b Behavior) Clone() Behavior {
	c := b
	if b.Params != nil {
		c.Params = maps.Clone(b.Params)
	}
	return c
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

// Rule is a condition plus the fault applied to traffic meeting it, and
// optionally the behavior that decides when that fault applies.
type Rule struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Enabled  bool      `json:"enabled"`
	Match    Match     `json:"match"`
	Fault    Fault     `json:"fault"`
	Behavior *Behavior `json:"behavior,omitempty"`

	// Revision changes every time the store writes this rule and never
	// repeats. Whatever keeps state for a rule between requests watches it to
	// know the rule was edited, enabled or disabled, and starts that state
	// over. It is store bookkeeping, so it stays off the wire.
	Revision uint64 `json:"-"`
}

// WithEnabled returns a copy of the rule with Enabled set as given.
func (r Rule) WithEnabled(enabled bool) Rule {
	c := r.Clone()
	c.Enabled = enabled
	return c
}

// WithRevision returns a copy of the rule marked as the given write.
func (r Rule) WithRevision(revision uint64) Rule {
	c := r.Clone()
	c.Revision = revision
	return c
}

// Clone returns a copy that shares nothing with the original, so neither side
// can observe the other's changes to the header map, the fault parameters or
// the behavior.
func (r Rule) Clone() Rule {
	c := r
	if r.Match.Header != nil {
		c.Match.Header = maps.Clone(r.Match.Header)
	}
	if r.Fault.Params != nil {
		c.Fault.Params = maps.Clone(r.Fault.Params)
	}
	if r.Behavior != nil {
		behavior := r.Behavior.Clone()
		c.Behavior = &behavior
	}
	return c
}
