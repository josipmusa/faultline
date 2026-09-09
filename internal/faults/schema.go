package faults

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/josipmusa/faultline/internal/rules"
)

// A schema validates the parameters of a fault or of a behavior. The scope is
// the word for which of the two, and the first half of the field path an error
// carries, so the API can point at fault.ms or behavior.n.
const (
	scopeFault    = "fault"
	scopeBehavior = "behavior"
)

// kind is the shape a parameter value must have.
type kind string

const (
	kindInt     kind = "integer"
	kindStr     kind = "string"
	kindStrMap  kind = "string map"
	kindStrList kind = "string list"
)

// Field is one parameter a fault accepts, with the constraints it must meet.
// Build one with Int, Str, StrMap or StrList and narrow it with the chained
// methods; a Field is a value, so each of them returns a new copy.
type Field struct {
	name     string
	kind     kind
	required bool
	min, max *int
	// chars, when set, is the only characters a string value may contain.
	chars string
	// partner names a sibling field this one is paired with: at least one of
	// the two must be present, and exactly one when exclusive. It is declared
	// on one side of the pair only, so a broken pair names one field to fix
	// rather than two.
	partner   string
	exclusive bool
}

// Int declares an integer parameter. A value arrives as a float64 from JSON and
// as an int from YAML; both are accepted as long as they are whole.
func Int(name string) Field { return Field{name: name, kind: kindInt} }

// Str declares a string parameter.
func Str(name string) Field { return Field{name: name, kind: kindStr} }

// StrMap declares a parameter holding names mapped to values, such as the
// response headers to set. It must not be empty: a fault with nothing to do is
// a rule that does not say what its author meant.
func StrMap(name string) Field { return Field{name: name, kind: kindStrMap} }

// StrList declares a parameter holding a list of names, such as the response
// headers to remove. It must not be empty, for the same reason as StrMap.
func StrList(name string) Field { return Field{name: name, kind: kindStrList} }

// Required says the parameter must be present. Everything else is optional and
// means the zero value of its kind.
func (f Field) Required() Field { f.required = true; return f }

// Min sets the smallest accepted value of an integer parameter.
func (f Field) Min(n int) Field { f.min = &n; return f }

// Max sets the largest accepted value of an integer parameter.
func (f Field) Max(n int) Field { f.max = &n; return f }

// Chars restricts a string parameter to the characters given, ignoring case,
// and rejects an empty one.
func (f Field) Chars(allowed string) Field { f.chars = allowed; return f }

// Or pairs the parameter with another, of which at least one is required.
func (f Field) Or(other string) Field { f.partner = other; return f }

// Xor pairs the parameter with another, of which exactly one is required.
func (f Field) Xor(other string) Field { f.partner, f.exclusive = other, true; return f }

// Name is the parameter's name as it appears in the API.
func (f Field) Name() string { return f.name }

// Schema is everything a fault accepts, in the order it is declared. It is the
// one description of a fault's parameters: validation reads it, and so will the
// published JSON Schema and the rule editor.
type Schema []Field

// Names lists the parameter names in declaration order.
func (s Schema) Names() []string {
	out := make([]string, 0, len(s))
	for _, f := range s {
		out = append(out, f.name)
	}
	return out
}

// ParamError says one parameter of a fault or a behavior is wrong and which.
// The API boundary turns it into the error body's field, which is what Path
// spells: `fault.ms`, `behavior.n`.
type ParamError struct {
	// Scope is fault or behavior. Empty reads as fault, so the faults that
	// raise most of these say nothing.
	Scope   string
	Field   string
	Message string
}

// Path names the parameter the way the API's error body does.
func (e *ParamError) Path() string {
	scope := e.Scope
	if scope == "" {
		scope = scopeFault
	}
	return scope + "." + e.Field
}

func (e *ParamError) Error() string { return e.Path() + ": " + e.Message }

func paramErr(field, format string, args ...any) *ParamError {
	return scopedErr(scopeFault, field, format, args...)
}

func scopedErr(scope, field, format string, args ...any) *ParamError {
	return &ParamError{Scope: scope, Field: field, Message: fmt.Sprintf(format, args...)}
}

// Validate checks a fault's params against the schema, naming the first field
// that is wrong. A parameter the schema does not declare is an error: it is the
// sign of a value meant for a different fault, or of a typo that would
// otherwise be silently ignored.
func (s Schema) Validate(params rules.Params) error {
	return s.validate(scopeFault, params)
}

// validate is Validate for either scope. A behavior's parameters are checked
// exactly like a fault's; only the words in the error differ.
func (s Schema) validate(scope string, params rules.Params) error {
	declared := make(map[string]Field, len(s))
	for _, f := range s {
		declared[f.name] = f
	}

	for name := range params {
		if _, ok := declared[name]; !ok {
			return scopedErr(scope, name, "unknown parameter %q; this %s takes %s", name, scope, s.describe())
		}
	}

	for _, f := range s {
		value, ok := params[f.name]
		if !ok {
			if f.required {
				return scopedErr(scope, f.name, "%s is required", f.name)
			}
			continue
		}
		if err := f.validate(scope, value); err != nil {
			return err
		}
	}

	for _, f := range s {
		if err := f.validatePair(scope, params); err != nil {
			return err
		}
	}
	return nil
}

// validatePair checks a field declared with Or or Xor against its partner.
func (f Field) validatePair(scope string, params rules.Params) error {
	if f.partner == "" {
		return nil
	}
	_, has := params[f.name]
	_, hasPartner := params[f.partner]
	switch {
	case f.exclusive && has && hasPartner:
		return scopedErr(scope, f.name, "%s and %s cannot both be set; pick one", f.name, f.partner)
	case !has && !hasPartner:
		return scopedErr(scope, f.name, "%s or %s is required", f.name, f.partner)
	default:
		return nil
	}
}

// describe names the parameters accepted, for an unknown parameter's error.
func (s Schema) describe() string {
	if len(s) == 0 {
		return "no parameters"
	}
	out := ""
	for i, f := range s {
		if i > 0 {
			out += ", "
		}
		out += f.name
	}
	return out
}

func (f Field) validate(scope string, value any) error {
	switch f.kind {
	case kindStr:
		text, ok := value.(string)
		if !ok {
			return scopedErr(scope, f.name, "%s must be a string", f.name)
		}
		return f.validateChars(scope, text)
	case kindStrMap:
		return f.validateStrMap(scope, value)
	case kindStrList:
		return f.validateStrList(scope, value)
	case kindInt:
		n, ok := wholeNumber(value)
		if !ok {
			return scopedErr(scope, f.name, "%s must be a whole number", f.name)
		}
		if f.min != nil && n < *f.min {
			return scopedErr(scope, f.name, "%s must be %d or more", f.name, *f.min)
		}
		if f.max != nil && n > *f.max {
			return scopedErr(scope, f.name, "%s must be %d or less", f.name, *f.max)
		}
		return nil
	default:
		return scopedErr(scope, f.name, "%s has an unknown kind %q", f.name, f.kind)
	}
}

// validateChars holds a string to the characters the field allows, ignoring
// case. A field that allows any character declares none.
func (f Field) validateChars(scope, text string) error {
	if f.chars == "" {
		return nil
	}
	allowed := strings.ToUpper(f.chars)
	if text == "" {
		return scopedErr(scope, f.name, "%s is empty; it is written as %s", f.name, listChars(f.chars))
	}
	for _, r := range text {
		if !strings.ContainsRune(allowed, unicode.ToUpper(r)) {
			return scopedErr(scope, f.name, "%s may only contain %s, not %q", f.name, listChars(f.chars), string(r))
		}
	}
	return nil
}

// listOf writes items as a human list: "a", "a or b", "a, b or c".
func listOf(items []string) string {
	out := ""
	for i, item := range items {
		switch {
		case i == 0:
		case i == len(items)-1:
			out += " or "
		default:
			out += ", "
		}
		out += item
	}
	return out
}

// quotedAll quotes each item, for a list of names in an error message.
func quotedAll(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprintf("%q", item))
	}
	return out
}

// listChars writes the characters a field allows as a human list.
func listChars(chars string) string {
	out := make([]string, 0, len(chars))
	for _, r := range chars {
		out = append(out, string(r))
	}
	return listOf(out)
}

// validateStrMap accepts a mapping of non-empty names to string values, as it
// arrives from JSON (map[string]any) or written in Go (map[string]string).
func (f Field) validateStrMap(scope string, value any) error {
	entries, ok := stringMap(value)
	if !ok {
		return scopedErr(scope, f.name, "%s must be a mapping of names to text values", f.name)
	}
	if len(entries) == 0 {
		return scopedErr(scope, f.name, "%s must name at least one entry", f.name)
	}
	for name := range entries {
		if name == "" {
			return scopedErr(scope, f.name, "%s has an entry with no name", f.name)
		}
	}
	return nil
}

// validateStrList accepts a list of non-empty names, as it arrives from JSON
// ([]any) or written in Go ([]string).
func (f Field) validateStrList(scope string, value any) error {
	names, ok := stringList(value)
	if !ok {
		return scopedErr(scope, f.name, "%s must be a list of names", f.name)
	}
	if len(names) == 0 {
		return scopedErr(scope, f.name, "%s must name at least one entry", f.name)
	}
	for _, name := range names {
		if name == "" {
			return scopedErr(scope, f.name, "%s has an entry with no name", f.name)
		}
	}
	return nil
}

// stringMap reads a mapping of strings out of a value decoded from JSON or
// YAML, or written in Go by a test or an embedded caller.
func stringMap(value any) (map[string]string, bool) {
	switch m := value.(type) {
	case map[string]string:
		return m, true
	case map[string]any:
		out := make(map[string]string, len(m))
		for name, v := range m {
			text, ok := v.(string)
			if !ok {
				return nil, false
			}
			out[name] = text
		}
		return out, true
	default:
		return nil, false
	}
}

// stringList reads a list of strings out of a value decoded from JSON or YAML,
// or written in Go by a test or an embedded caller.
func stringList(value any) ([]string, bool) {
	switch l := value.(type) {
	case []string:
		return l, true
	case []any:
		out := make([]string, 0, len(l))
		for _, v := range l {
			text, ok := v.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	default:
		return nil, false
	}
}

// wholeNumber reads an integer out of a value decoded from JSON or YAML. JSON
// numbers arrive as float64, YAML whole ones as int, and a fractional value is
// not an integer however it arrived.
func wholeNumber(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n != math.Trunc(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

// Decode fills a fault's own configuration struct from validated parameters,
// matching them to the struct's JSON tags. It is the single step from the open
// parameter bag to typed fields, so no fault reads the bag itself.
func Decode(params rules.Params, into any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("faults: encoding parameters: %w", err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("faults: reading parameters: %w", err)
	}
	return nil
}
