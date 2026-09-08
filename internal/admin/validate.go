package admin

import (
	"strings"

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
	return validateFault(r.Fault)
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

func validateFault(f rules.Fault) error {
	switch f.Type {
	case "":
		return invalid("fault.type", "fault.type is required, use %q or %q", rules.FaultDelay, rules.FaultStatus)
	case rules.FaultDelay:
		return validateDelay(f)
	case rules.FaultStatus:
		return validateStatus(f)
	default:
		return invalid("fault.type", "unknown fault type %q, use %q or %q", f.Type, rules.FaultDelay, rules.FaultStatus)
	}
}

func validateDelay(f rules.Fault) error {
	if f.MS <= 0 {
		return invalid("fault.ms", "a delay fault needs ms above 0")
	}
	if f.JitterMS < 0 {
		return invalid("fault.jitter_ms", "jitter_ms cannot be negative")
	}
	if f.Code != 0 {
		return invalid("fault.code", "code belongs to a status fault, not a delay fault")
	}
	if f.Body != "" {
		return invalid("fault.body", "body belongs to a status fault, not a delay fault")
	}
	return nil
}

func validateStatus(f rules.Fault) error {
	if f.Code < 100 || f.Code > 599 {
		return invalid("fault.code", "a status fault needs code between 100 and 599")
	}
	if f.MS != 0 {
		return invalid("fault.ms", "ms belongs to a delay fault, not a status fault")
	}
	if f.JitterMS != 0 {
		return invalid("fault.jitter_ms", "jitter_ms belongs to a delay fault, not a status fault")
	}
	return nil
}
