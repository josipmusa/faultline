package faults

import (
	"log/slog"
	"sync"

	"github.com/josipmusa/faultline/internal/rules"
)

// Gate holds the state behaviors keep between requests, one decider per rule,
// and answers whether a rule that matched should have its fault applied.
//
// One gate serves every pipeline reading the same rule store, so a rule that
// fails the first two requests fails two in total rather than two per tier. The
// gate takes its lock for each decision, which makes the deciders it holds
// single-threaded and keeps their state out of the race detector's way.
type Gate struct {
	mu    sync.Mutex
	state map[string]*gated
}

// gated is one rule's behavior state, remembered against the revision of the
// rule it was built from.
type gated struct {
	revision uint64
	decider  Decider
}

// NewGate returns an empty gate. Pass one to every Transport and Dialer sharing
// a rule store.
func NewGate() *Gate {
	return &Gate{state: map[string]*gated{}}
}

// Applies reports whether the fault of a rule that just matched applies to this
// request, and advances that rule's behavior state. A rule with no behavior
// always applies.
//
// State belongs to a revision of a rule, so editing a rule, enabling it or
// disabling it starts its behavior over: the next request is the first one
// again.
func (g *Gate) Applies(r rules.Rule) bool {
	if r.Behavior == nil {
		return true
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	entry, ok := g.state[r.ID]
	if !ok || entry.revision != r.Revision {
		decider, err := BuildBehavior(*r.Behavior)
		if err != nil {
			// The rule reached the pipeline without passing validation. Its
			// fault still applies; nothing here decides otherwise.
			slog.Default().Warn("rule keeps no behavior state", "rule", r.ID, "err", err)
			return true
		}
		entry = &gated{revision: r.Revision, decider: decider}
		g.state[r.ID] = entry
	}
	return entry.decider.Applies()
}

// Reset forgets the behavior state of every rule, so a spent first_n or a
// percent that has been rolling since startup counts from the beginning again.
// The rules themselves are untouched: this is the effect editing every one of
// them would have, without changing any of them.
//
// It is what an agent or a person does between two runs of the same test, and
// it is the only way to re-arm a rule without disturbing it.
func (g *Gate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.state = map[string]*gated{}
}
