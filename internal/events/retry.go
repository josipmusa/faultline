package events

import (
	"iter"
	"net/http"
	"time"
)

// retryWindow is how long after an attempt ends a matching request still counts
// as a retry of it. It covers the backoff of a typical HTTP client without
// being so generous that a slow polling loop reads as one.
const retryWindow = 5 * time.Second

// start is when the attempt began. An event is recorded once its call
// finishes, so the timestamp it carries is the end.
func (e Event) start() time.Time {
	return e.Timestamp.Add(-time.Duration(e.DurationMS) * time.Millisecond)
}

// retryable reports whether an attempt is one a resilient client would make
// again: Faultline broke it, it never got a response at all, or the response
// blamed the upstream rather than the request.
func (e Event) retryable() bool {
	return e.Faulted ||
		e.Error != "" ||
		e.Status == 0 ||
		e.Status >= http.StatusInternalServerError ||
		e.Status == http.StatusTooManyRequests
}

// retryOf returns the id of the attempt e repeats, or "" when e repeats
// nothing. prior yields the events already recorded, newest first.
//
// An attempt qualifies when it went to the same upstream with the same method
// and path, was worth retrying, ended before e began, and ended no longer than
// retryWindow before that. Scanning newest first means every retry of a
// candidate is seen before the candidate itself, so an attempt another retry
// has already claimed is known to be taken by the time we reach it. That keeps
// concurrent calls to one path pairing off one to one instead of all pointing
// at the same attempt.
func retryOf(prior iter.Seq[Event], e Event) string {
	begun := e.start()
	oldest := begun.Add(-retryWindow)

	var claimed map[string]bool
	for p := range prior {
		if p.Timestamp.Before(oldest) {
			break // everything older is outside the window too
		}
		if p.RetryOf != "" {
			if claimed == nil {
				claimed = make(map[string]bool)
			}
			claimed[p.RetryOf] = true
		}
		if claimed[p.ID] || !p.retryable() || p.Timestamp.After(begun) {
			continue // taken, not worth retrying, or still in flight when e began
		}
		if p.Host == e.Host && p.Method == e.Method && p.Path == e.Path {
			return p.ID
		}
	}
	return ""
}
