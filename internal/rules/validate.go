package rules

import (
	"fmt"
	"strings"
)

// MaxIDLen is the longest a rule id may be. Ids are typed by hand, appear in
// URLs and in the Faultline-Fault header, and are the name a scenario uses to
// reach a rule, so they stay short.
const MaxIDLen = 64

// FieldError names the field a rule got wrong and says what is wrong with it,
// so every caller can point at the right place: the API answers with the field
// path, the config loader turns it into a file and a line.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

func fieldErr(field, format string, args ...any) *FieldError {
	return &FieldError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// Validate checks the parts of a rule this package owns: its id, its name, and
// what it matches. The fault and the behavior belong to the fault catalogue and
// are validated there, since only the catalogue knows what each one takes.
//
// An empty id passes. Where an id comes from is the caller's business: the API
// makes one up from the name, a config file insists on one.
func Validate(r Rule) error {
	if err := ValidateID(r.ID); err != nil {
		return err
	}
	if strings.TrimSpace(r.Name) == "" {
		return fieldErr("name", "name is required")
	}
	return ValidateMatch(r.Match)
}

// ValidateID checks an id a human wrote. The empty id means "choose one for me"
// and is accepted here.
func ValidateID(id string) error {
	if id == "" {
		return nil
	}
	if len(id) > MaxIDLen {
		return fieldErr("id", "id is longer than %d characters", MaxIDLen)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return fieldErr("id", "id may only contain letters, digits, '-', '_' and '.'")
		}
	}
	return nil
}

// ValidateMatch checks the request selector. Every part of it is optional; a
// match with nothing set matches everything, which is a legitimate thing to
// want and the reason none of these are required.
func ValidateMatch(m Match) error {
	if strings.ContainsAny(m.Host, "/ ") {
		return fieldErr("match.host", "host is a hostname like api.stripe.com, not a URL")
	}
	for _, r := range m.Method {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return fieldErr("match.method", "method is an HTTP method like GET or POST")
		}
	}
	if m.Path != "" && !strings.HasPrefix(m.Path, "/") && !strings.HasPrefix(m.Path, "*") {
		return fieldErr("match.path", "path must start with / or a wildcard, like /v1/charges/*")
	}
	if _, empty := m.Header[""]; empty {
		return fieldErr("match.header", "header names cannot be empty")
	}
	return nil
}
