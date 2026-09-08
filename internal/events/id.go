package events

import (
	"strconv"
	"sync/atomic"
)

// lastID is the source of event ids for this process.
var lastID atomic.Uint64

// NextID returns a unique id for a new event. Ids are decimal counters, so they
// sort in the order events were recorded and cannot collide the way a
// timestamp can under concurrency. Nothing persists them: they start again at 1
// on every run, which is fine because events live in memory only.
func NextID() string {
	return strconv.FormatUint(lastID.Add(1), 10)
}
