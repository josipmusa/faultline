// Package rules defines the fault rules Faultline applies to observed traffic.
//
// Rules are immutable values: every field is set at construction and updates
// return a new copy. Mutable collections of rules live in a store, not here.
package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// FaultType names one kind of fault. The catalogue of types lives in
// internal/faults, not here: this package carries a rule around, it does not
// know what any fault does.
type FaultType string

// Params are a fault's parameters, keyed as they appear in the API. The domain
// type keeps them as they arrived; the fault named by Type is the only thing
// that knows which keys it wants, and the API boundary rejects the rest.
type Params map[string]any

// Fault is the single thing a rule does to traffic it matches.
type Fault struct {
	Type   FaultType
	Params Params
}

// faultTypeKey is the one key in a fault object that is not a parameter.
const faultTypeKey = "type"

// UnmarshalJSON reads the flat wire shape, `{"type":"delay","ms":2000}`, into a
// type and a bag of parameters.
func (f *Fault) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*f = Fault{}
	if t, ok := raw[faultTypeKey].(string); ok {
		f.Type = FaultType(t)
	}
	delete(raw, faultTypeKey)
	if len(raw) > 0 {
		f.Params = raw
	}
	return nil
}

// MarshalJSON writes the flat wire shape back, with the type first and the
// parameters after it in a stable order.
func (f Fault) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"` + faultTypeKey + `":`)

	name, err := json.Marshal(string(f.Type))
	if err != nil {
		return nil, fmt.Errorf("rules: fault type: %w", err)
	}
	b.Write(name)

	for _, key := range slices.Sorted(maps.Keys(f.Params)) {
		value, err := json.Marshal(f.Params[key])
		if err != nil {
			return nil, fmt.Errorf("rules: fault parameter %q: %w", key, err)
		}
		b.WriteString(",")
		b.Write(quoted(key))
		b.WriteString(":")
		b.Write(value)
	}

	b.WriteString("}")
	return b.Bytes(), nil
}

// quoted encodes a parameter name. Names come from a JSON object, so they are
// valid strings and cannot fail to encode.
func quoted(key string) []byte {
	out, err := json.Marshal(key)
	if err != nil {
		return []byte(`""`)
	}
	return out
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
// can observe the other's changes to the header map or the fault parameters.
func (r Rule) Clone() Rule {
	c := r
	if r.Match.Header != nil {
		c.Match.Header = maps.Clone(r.Match.Header)
	}
	if r.Fault.Params != nil {
		c.Fault.Params = maps.Clone(r.Fault.Params)
	}
	return c
}
