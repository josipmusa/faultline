package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/josipmusa/faultline/internal/rules"
)

const maxBodyBytes = 64 << 10

func (s *Server) listRules(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.rules.List())
}

func (s *Server) getRule(w http.ResponseWriter, r *http.Request) {
	rule, err := s.rule(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	rule, err := decodeRule(w, r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := validateRule(rule); err != nil {
		s.fail(w, err)
		return
	}

	if rule.ID == "" {
		rule.ID = s.freeID(rule.Name)
	}
	if err := s.rules.Add(rule); err != nil {
		if errors.Is(err, rules.ErrExists) {
			s.fail(w, conflict("id", "a rule with id %q already exists", rule.ID))
			return
		}
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	rule, err := decodeRule(w, r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if rule.ID != "" && rule.ID != id {
		s.fail(w, invalid("id", "body id %q does not match id %q in the path", rule.ID, id))
		return
	}
	rule.ID = id
	if err := validateRule(rule); err != nil {
		s.fail(w, err)
		return
	}

	if err := s.rules.Update(rule); err != nil {
		s.fail(w, s.storeError(id, err))
		return
	}
	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.rules.Delete(id); err != nil {
		s.fail(w, s.storeError(id, err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) enableRule(w http.ResponseWriter, r *http.Request) {
	s.setEnabled(w, r.PathValue("id"), true)
}

func (s *Server) disableRule(w http.ResponseWriter, r *http.Request) {
	s.setEnabled(w, r.PathValue("id"), false)
}

func (s *Server) setEnabled(w http.ResponseWriter, id string, enabled bool) {
	change := s.rules.Disable
	if enabled {
		change = s.rules.Enable
	}
	if err := change(id); err != nil {
		s.fail(w, s.storeError(id, err))
		return
	}

	rule, err := s.rule(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) rule(id string) (rules.Rule, error) {
	rule, err := s.rules.Get(id)
	if err != nil {
		return rules.Rule{}, s.storeError(id, err)
	}
	return rule, nil
}

// storeError turns a store failure into the response the API promises for it.
func (s *Server) storeError(id string, err error) error {
	if errors.Is(err, rules.ErrNotFound) {
		return missing("no rule with id %q", id)
	}
	return err
}

// decodeRule reads a rule from the request body. Enabled defaults to true, so a
// rule posted without the field starts working straight away.
func decodeRule(w http.ResponseWriter, r *http.Request) (rules.Rule, error) {
	rule := rules.Rule{Enabled: true}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&rule); err != nil {
		if errors.Is(err, io.EOF) {
			return rules.Rule{}, invalid("", "body is empty, want a rule object")
		}
		return rules.Rule{}, invalid("", "body is not a valid rule: %v", err)
	}
	return rule, nil
}

// freeID derives a readable id from the rule name, adding a numeric suffix
// until it finds one the store is not already using.
func (s *Server) freeID(name string) string {
	base := slugify(name)
	if base == "" {
		base = "rule"
	}

	taken := make(map[string]bool)
	for _, r := range s.rules.List() {
		taken[r.ID] = true
	}

	id := base
	for n := 2; taken[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	return id
}

func slugify(name string) string {
	var b strings.Builder
	dash := false

	for _, r := range strings.ToLower(name) {
		if b.Len() >= maxIDLen {
			break
		}
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
