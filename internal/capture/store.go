// Package capture keeps the headers and bodies of recent requests, so a human
// can open one event and read what actually went over the wire.
//
// Captures live apart from the events themselves. An event is small and every
// one of them travels on the read-only stream; a capture is large and is
// wanted only when somebody asks to see it, so it is fetched by event id
// rather than carried along with the event.
package capture

import (
	"net/http"
	"sync"
)

// MaxBodyBytes is how much of one body is kept. A body longer than this is
// captured up to the cap and flagged truncated, so a large download costs a
// bounded amount and still shows its first page.
const MaxBodyBytes = 64 << 10

// DefaultBudget is how many bytes of captures a store holds before it starts
// dropping the oldest. The bound is on bytes rather than on a count of
// captures because bodies vary by orders of magnitude, and a count bounds
// memory only at its worst case.
const DefaultBudget = 32 << 20

// Side is one half of an exchange as Faultline saw it.
type Side struct {
	Headers http.Header `json:"headers"`
	// Body is what was read, up to MaxBodyBytes. Truncated says there was
	// more of it than that.
	Body      []byte `json:"body,omitempty"`
	Truncated bool   `json:"truncated"`
}

// Capture is one request and its response, keyed by the id of the event that
// recorded them.
type Capture struct {
	EventID  string `json:"event_id"`
	Request  Side   `json:"request"`
	Response Side   `json:"response"`
}

// Store holds recent captures under a total byte budget, dropping the oldest
// to stay inside it. It is safe for concurrent use.
type Store struct {
	mu     sync.Mutex
	budget int64
	used   int64
	byID   map[string]*Capture
	// order is the ids in the order they were put, oldest first: what
	// eviction consumes.
	order []string
}

// NewStore holds up to budget bytes of captures. A budget below one falls back
// to DefaultBudget.
func NewStore(budget int64) *Store {
	if budget < 1 {
		budget = DefaultBudget
	}
	return &Store{budget: budget, byID: make(map[string]*Capture)}
}

// Put files a capture, evicting the oldest ones if it does not fit. Putting an
// id twice replaces what was there.
func (s *Store) Put(c Capture) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dropLocked(c.EventID)
	s.byID[c.EventID] = &c
	s.order = append(s.order, c.EventID)
	s.used += sizeOf(c)
	s.evictLocked()
}

// Complete fills in a response body that only finished arriving after its
// event was recorded, which is every body: an event is recorded at the
// response headers and the body streams afterwards. An id the store no longer
// holds is ignored rather than resurrected.
func (s *Store) Complete(id string, body []byte, truncated bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.byID[id]
	if !ok {
		return
	}
	s.used += int64(len(body)) - int64(len(c.Response.Body))
	c.Response.Body, c.Response.Truncated = body, truncated
	s.evictLocked()
}

// Get returns the capture for an event id, if the store still holds one.
func (s *Store) Get(id string) (Capture, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.byID[id]
	if !ok {
		return Capture{}, false
	}
	return *c, true
}

// Clear drops every capture, releasing the whole budget.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.byID = make(map[string]*Capture)
	s.order, s.used = nil, 0
}

// evictLocked drops the oldest captures until the store is inside its budget.
// The newest capture is always kept, however large it is: a store that
// answered nothing would be worse than one over budget by a single entry.
func (s *Store) evictLocked() {
	for s.used > s.budget && len(s.order) > 1 {
		oldest := s.order[0]
		s.order = s.order[1:]
		s.forgetLocked(oldest)
	}
}

// dropLocked removes an id from the store, if it is there.
func (s *Store) dropLocked(id string) {
	if _, ok := s.byID[id]; !ok {
		return
	}
	for i, held := range s.order {
		if held == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.forgetLocked(id)
}

// forgetLocked deletes a capture and gives its bytes back. The caller has
// already taken the id out of order.
func (s *Store) forgetLocked(id string) {
	if c, ok := s.byID[id]; ok {
		s.used -= sizeOf(*c)
		delete(s.byID, id)
	}
}

// sizeOf is what a capture costs: its bodies, plus its headers, which for a
// request carrying no body at all are the whole of it.
func sizeOf(c Capture) int64 {
	return sideSize(c.Request) + sideSize(c.Response)
}

func sideSize(s Side) int64 {
	n := int64(len(s.Body))
	for name, values := range s.Headers {
		n += int64(len(name))
		for _, v := range values {
			n += int64(len(v))
		}
	}
	return n
}
