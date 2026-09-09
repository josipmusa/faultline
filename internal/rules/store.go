package rules

import (
	"errors"
	"slices"
	"sync"
)

// Errors returned by Store. Callers map these onto HTTP status codes at the
// API boundary: ErrNotFound to 404, ErrExists to 409, ErrEmptyID to 400.
var (
	ErrNotFound = errors.New("rule not found")
	ErrExists   = errors.New("rule already exists")
	ErrEmptyID  = errors.New("rule id is empty")
)

// Store holds the active rules. It is safe for concurrent use.
//
// Rules keep the order they were added in, because that is the order the fault
// pipeline consults them in. The store holds copies, so nothing a caller does
// to a rule before or after handing it over can reach inside.
type Store struct {
	mu      sync.RWMutex
	rules   []Rule
	written uint64 // the last revision handed out, see Rule.Revision
	changes chan struct{}
}

// New returns an empty store.
func New() *Store {
	return &Store{changes: make(chan struct{}, 1)}
}

// Changes returns the channel signalled after every mutation that changed
// something. Signals coalesce: a slow reader sees one pending signal however
// many mutations happened, so it must re-read the store rather than count
// signals. Sending never blocks, so an unread channel cannot stall a writer.
func (s *Store) Changes() <-chan struct{} {
	return s.changes
}

// List returns every rule, in insertion order.
func (s *Store) List() []Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Rule, len(s.rules))
	for i, r := range s.rules {
		out[i] = r.Clone()
	}
	return out
}

// Get returns the rule with the given id, or ErrNotFound.
func (s *Store) Get(id string) (Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	i := s.indexOf(id)
	if i < 0 {
		return Rule{}, ErrNotFound
	}
	return s.rules[i].Clone(), nil
}

// Add appends a rule. It fails with ErrEmptyID if the rule has no id, or
// ErrExists if that id is taken.
func (s *Store) Add(r Rule) error {
	if r.ID == "" {
		return ErrEmptyID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.indexOf(r.ID) >= 0 {
		return ErrExists
	}
	s.rules = append(s.rules, s.stamp(r))
	s.notify()
	return nil
}

// Update replaces the rule with the same id, keeping its position, or returns
// ErrNotFound.
func (s *Store) Update(r Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.indexOf(r.ID)
	if i < 0 {
		return ErrNotFound
	}
	s.rules[i] = s.stamp(r)
	s.notify()
	return nil
}

// Delete removes the rule with the given id, or returns ErrNotFound.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.indexOf(id)
	if i < 0 {
		return ErrNotFound
	}
	s.rules = slices.Delete(s.rules, i, i+1)
	s.notify()
	return nil
}

// Enable turns the rule on, or returns ErrNotFound.
func (s *Store) Enable(id string) error { return s.setEnabled(id, true) }

// Disable turns the rule off without removing it, or returns ErrNotFound.
func (s *Store) Disable(id string) error { return s.setEnabled(id, false) }

func (s *Store) setEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.indexOf(id)
	if i < 0 {
		return ErrNotFound
	}
	s.rules[i] = s.stamp(s.rules[i].WithEnabled(enabled))
	s.notify()
	return nil
}

// stamp gives a rule the next revision, so anything keeping state for it can
// tell one write from the next. Callers hold the lock.
func (s *Store) stamp(r Rule) Rule {
	s.written++
	return r.WithRevision(s.written)
}

// indexOf reports the position of id, or -1. Callers hold the lock.
func (s *Store) indexOf(id string) int {
	return slices.IndexFunc(s.rules, func(r Rule) bool { return r.ID == id })
}

// notify signals a change without blocking. Callers hold the lock.
func (s *Store) notify() {
	select {
	case s.changes <- struct{}{}:
	default:
	}
}
