package faults

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/josipmusa/faultline/internal/rules"
)

// kind is the shape a parameter value must have.
type kind string

const (
	kindInt kind = "integer"
	kindStr kind = "string"
)

// Field is one parameter a fault accepts, with the constraints it must meet.
// Build one with Int or Str and narrow it with the chained methods; a Field is
// a value, so each of them returns a new copy.
type Field struct {
	name     string
	kind     kind
	required bool
	min, max *int
}

// Int declares an integer parameter. A value arrives as a float64 from JSON and
// as an int from YAML; both are accepted as long as they are whole.
func Int(name string) Field { return Field{name: name, kind: kindInt} }

// Str declares a string parameter.
func Str(name string) Field { return Field{name: name, kind: kindStr} }

// Required says the parameter must be present. Everything else is optional and
// means the zero value of its kind.
func (f Field) Required() Field { f.required = true; return f }

// Min sets the smallest accepted value of an integer parameter.
func (f Field) Min(n int) Field { f.min = &n; return f }

// Max sets the largest accepted value of an integer parameter.
func (f Field) Max(n int) Field { f.max = &n; return f }

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

// ParamError says one fault parameter is wrong and which. The API boundary
// turns it into the error body's field, prefixed with `fault.`.
type ParamError struct {
	Field   string
	Message string
}

func (e *ParamError) Error() string { return fmt.Sprintf("fault.%s: %s", e.Field, e.Message) }

func paramErr(field, format string, args ...any) *ParamError {
	return &ParamError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// Validate checks params against the schema, naming the first field that is
// wrong. A parameter the schema does not declare is an error: it is the sign of
// a value meant for a different fault, or of a typo that would otherwise be
// silently ignored.
func (s Schema) Validate(params rules.Params) error {
	declared := make(map[string]Field, len(s))
	for _, f := range s {
		declared[f.name] = f
	}

	for name := range params {
		if _, ok := declared[name]; !ok {
			return paramErr(name, "unknown parameter %q; this fault takes %s", name, s.describe())
		}
	}

	for _, f := range s {
		value, ok := params[f.name]
		if !ok {
			if f.required {
				return paramErr(f.name, "%s is required", f.name)
			}
			continue
		}
		if err := f.validate(value); err != nil {
			return err
		}
	}
	return nil
}

// describe names the parameters a fault takes, for an unknown parameter's error.
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

func (f Field) validate(value any) error {
	switch f.kind {
	case kindStr:
		if _, ok := value.(string); !ok {
			return paramErr(f.name, "%s must be a string", f.name)
		}
		return nil
	case kindInt:
		n, ok := wholeNumber(value)
		if !ok {
			return paramErr(f.name, "%s must be a whole number", f.name)
		}
		if f.min != nil && n < *f.min {
			return paramErr(f.name, "%s must be %d or more", f.name, *f.min)
		}
		if f.max != nil && n > *f.max {
			return paramErr(f.name, "%s must be %d or less", f.name, *f.max)
		}
		return nil
	default:
		return paramErr(f.name, "%s has an unknown kind %q", f.name, f.kind)
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
