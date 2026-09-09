package admin

import (
	"errors"
	"strings"

	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
)

// maxIDLen bounds a rule id, which is a URL path segment and a UI label.
const maxIDLen = 64

// validateRule checks a rule coming in from outside. The domain type carries no
// validation of its own: this is the boundary, and it names the bad field.
func validateRule(r rules.Rule) error {
	if err := validateID(r.ID); err != nil {
		return err
	}
	if strings.TrimSpace(r.Name) == "" {
		return invalid("name", "name is required")
	}
	if err := validateMatch(r.Match); err != nil {
		return err
	}
	if err := validateFault(r.Fault); err != nil {
		return err
	}
	return validateBehavior(r.Behavior)
}

// validateID accepts an empty id: the server mints one for a new rule.
func validateID(id string) error {
	if id == "" {
		return nil
	}
	if len(id) > maxIDLen {
		return invalid("id", "id is longer than %d characters", maxIDLen)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return invalid("id", "id may only contain letters, digits, '-', '_' and '.'")
		}
	}
	return nil
}

// validateMatch allows every field to be empty; an empty match matches
// everything. It only rejects values that could never match anything.
func validateMatch(m rules.Match) error {
	if strings.ContainsAny(m.Host, "/ ") {
		return invalid("match.host", "host is a hostname like api.stripe.com, not a URL")
	}
	for _, r := range m.Method {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return invalid("match.method", "method is an HTTP method like GET or POST")
		}
	}
	if m.Path != "" && !strings.HasPrefix(m.Path, "/") && !strings.HasPrefix(m.Path, "*") {
		return invalid("match.path", "path must start with / or a wildcard, like /v1/charges/*")
	}
	if _, empty := m.Header[""]; empty {
		return invalid("match.header", "header names cannot be empty")
	}
	return nil
}

// validateFault delegates to the fault named by the type: the catalogue in
// internal/faults owns what each fault accepts, so a new fault is validated
// here without this file changing.
func validateFault(f rules.Fault) error {
	return asAPIError(faults.Validate(f))
}

// validateBehavior delegates the same way. A rule without a behavior is the
// common case and is fine.
func validateBehavior(b *rules.Behavior) error {
	if b == nil {
		return nil
	}
	return asAPIError(faults.ValidateBehavior(*b))
}

// asAPIError turns a parameter the catalogue rejected into a 400 naming it,
// like fault.ms or behavior.n.
func asAPIError(err error) error {
	var pe *faults.ParamError
	if errors.As(err, &pe) {
		return invalid(pe.Path(), "%s", pe.Message)
	}
	return err
}
