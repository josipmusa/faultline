package events

// DefaultSize is how many events are kept when no size is configured.
const DefaultSize = 1000

// ringBuffer keeps the newest events and overwrites the oldest. It carries no
// lock of its own; Recorder serialises access.
type ringBuffer struct {
	events []Event
	size   int
	cursor int // where the next event goes
	count  int // how many slots are filled, never above size
}

// newRingBuffer holds size events. A size below one falls back to DefaultSize.
func newRingBuffer(size int) *ringBuffer {
	if size < 1 {
		size = DefaultSize
	}
	return &ringBuffer{events: make([]Event, size), size: size}
}

func (r *ringBuffer) add(e Event) {
	r.events[r.cursor] = e
	r.cursor = (r.cursor + 1) % r.size
	if r.count < r.size {
		r.count++
	}
}

// all returns the buffered events, oldest first, in a slice the caller owns.
func (r *ringBuffer) all() []Event {
	out := make([]Event, 0, r.count)
	for i := range r.count {
		out = append(out, r.events[(r.cursor-r.count+i+r.size)%r.size])
	}
	return out
}

func (r *ringBuffer) reset() {
	clear(r.events)
	r.cursor, r.count = 0, 0
}
