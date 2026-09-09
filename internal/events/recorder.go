package events

import (
	"slices"
	"sync"
	"time"
)

// subscriberBacklog is how many events a subscription holds before the
// recorder starts dropping them. Deep enough to absorb a burst, shallow enough
// that a dead subscriber cannot hoard memory.
const subscriberBacklog = 100

// Recorder keeps the recent events and streams new ones to subscribers. It is
// safe for concurrent use.
//
// Recording never blocks the request path: a subscriber that falls behind
// loses events rather than slowing traffic down. The ring buffer is the source
// of truth, the stream is best effort.
type Recorder struct {
	mu   sync.Mutex
	ring *ringBuffer
	subs []*Subscription
}

// Subscription is a live feed of events. Read from C until it closes, and call
// Close when done.
type Subscription struct {
	C <-chan Event

	ch     chan Event
	once   sync.Once
	parent *Recorder
}

// NewRecorder keeps size events. A size below one falls back to DefaultSize.
func NewRecorder(size int) *Recorder {
	return &Recorder{ring: newRingBuffer(size)}
}

// Record buffers an event and offers it to every subscriber.
func (r *Recorder) Record(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e.RetryOf = retryOf(r.ring.backward(), e)

	r.ring.add(e)
	for _, sub := range r.subs {
		select {
		case sub.ch <- e:
		default: // the subscriber is behind, drop rather than block
		}
	}
}

// Events returns the buffered events, oldest first.
func (r *Recorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.ring.all()
}

// Report summarises the session so far.
func (r *Recorder) Report() Report {
	r.mu.Lock()
	defer r.mu.Unlock()

	return Reported(r.ring.all(), time.Now())
}

// Clear drops every recorded event. Subscribers are left alone: the stream is
// live traffic, the ring is history.
func (r *Recorder) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ring.reset()
}

// Subscribe returns a feed of events recorded from now on. History comes from
// Events, not from the feed.
func (r *Recorder) Subscribe() *Subscription {
	ch := make(chan Event, subscriberBacklog)
	sub := &Subscription{C: ch, ch: ch, parent: r}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.subs = append(r.subs, sub)
	return sub
}

// Close detaches the subscription and closes its channel. It is safe to call
// more than once, and safe to call after the recorder itself was closed.
func (s *Subscription) Close() {
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()

	s.parent.remove(s)
}

// Close detaches and closes every subscription. Recording afterwards still
// buffers, it just reaches nobody.
func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, sub := range slices.Clone(r.subs) {
		r.remove(sub)
	}
}

// remove detaches a subscription and closes its channel exactly once. Callers
// hold the lock, so no send can be in flight on the channel being closed.
func (r *Recorder) remove(s *Subscription) {
	if i := slices.Index(r.subs, s); i >= 0 {
		r.subs = slices.Delete(r.subs, i, i+1)
	}
	s.once.Do(func() { close(s.ch) })
}
