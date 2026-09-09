package events

import (
	"fmt"
	"testing"
	"time"
)

// origin is the start of the session every test below measures from.
var origin = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

// attempt builds a successful call to the shared upstream that began the given
// way into the session and took took. Events are recorded when a call ends, so
// the timestamp is the end.
func attempt(id string, start, took time.Duration) Event {
	return Event{
		ID:         id,
		Host:       "api.stripe.com",
		Method:     "GET",
		Path:       "/v1/charges",
		Status:     200,
		Tier:       TierIntercepted,
		Timestamp:  origin.Add(start + took),
		DurationMS: took.Milliseconds(),
	}
}

// at is attempt for a call quick enough that its duration does not matter.
func at(id string, start time.Duration) Event {
	return attempt(id, start, 10*time.Millisecond)
}

// faultedAt is at with a 503 that a rule caused.
func faultedAt(id string, start time.Duration) Event {
	e := at(id, start)
	e.Status, e.Faulted, e.RuleID = 503, true, "stripe-down"
	return e
}

// replay records a sequence in the order given and returns it with retry_of
// filled in.
func replay(t *testing.T, es ...Event) []Event {
	t.Helper()
	r := NewRecorder(DefaultSize)
	t.Cleanup(r.Close)
	for _, e := range es {
		r.Record(e)
	}
	return r.Events()
}

// links renders every event that is a retry as "retry/original", in order.
func links(es []Event) string {
	out := []string{}
	for _, e := range es {
		if e.RetryOf != "" {
			out = append(out, e.ID+"/"+e.RetryOf)
		}
	}
	return fmt.Sprint(out)
}

func TestRetryLinksARepeatedRequestAfterAFault(t *testing.T) {
	got := replay(t, faultedAt("a", 0), at("b", time.Second))

	if links(got) != "[b/a]" {
		t.Errorf("links = %s, want [b/a]", links(got))
	}
}

func TestRetryChainsThroughEveryAttempt(t *testing.T) {
	got := replay(t, faultedAt("a", 0), faultedAt("b", time.Second), at("c", 2*time.Second))

	if links(got) != "[b/a c/b]" {
		t.Errorf("links = %s, want [b/a c/b], each retry pointing at the attempt before it", links(got))
	}
}

func TestASucceededAttemptIsNotRetried(t *testing.T) {
	got := replay(t, at("a", 0), at("b", time.Second))

	if links(got) != "[]" {
		t.Errorf("links = %s, want none; nothing about a 200 asks the client to try again", links(got))
	}
}

func TestRetryNeedsTheSameUpstreamMethodAndPath(t *testing.T) {
	otherHost := at("b", time.Second)
	otherHost.Host = "api.github.com"
	otherMethod := at("b", time.Second)
	otherMethod.Method = "POST"
	otherPath := at("b", time.Second)
	otherPath.Path = "/v1/refunds"

	for name, e := range map[string]Event{"host": otherHost, "method": otherMethod, "path": otherPath} {
		t.Run(name, func(t *testing.T) {
			got := replay(t, faultedAt("a", 0), e)

			if links(got) != "[]" {
				t.Errorf("links = %s, want none; a different %s is a different call", links(got), name)
			}
		})
	}
}

func TestRetryOutsideTheWindowIsNotLinked(t *testing.T) {
	got := replay(t, faultedAt("a", 0), at("b", retryWindow+time.Second))

	if links(got) != "[]" {
		t.Errorf("links = %s, want none; %s later the two calls are unrelated", links(got), retryWindow+time.Second)
	}
}

func TestRetryOnTheEdgeOfTheWindowIsLinked(t *testing.T) {
	first := faultedAt("a", 0)
	got := replay(t, first, at("b", 10*time.Millisecond+retryWindow))

	if links(got) != "[b/a]" {
		t.Errorf("links = %s, want [b/a]; a wait of exactly %s is still inside the window", links(got), retryWindow)
	}
}

func TestConcurrentRetriesPairOffOneToOne(t *testing.T) {
	got := replay(t, faultedAt("a1", 0), faultedAt("a2", 0), at("b1", time.Second), at("b2", time.Second))

	if links(got) != "[b1/a2 b2/a1]" {
		t.Errorf("links = %s, want [b1/a2 b2/a1]; two retries must not claim one attempt", links(got))
	}
}

func TestAnOverlappingCallIsNotARetry(t *testing.T) {
	slow := faultedAt("a", 0)
	slow.Timestamp, slow.DurationMS = origin.Add(time.Second), time.Second.Milliseconds()

	got := replay(t, slow, at("b", 900*time.Millisecond))

	if links(got) != "[]" {
		t.Errorf("links = %s, want none; b was already in flight when a failed", links(got))
	}
}

func TestAnAttemptWithNoResponseIsWorthRetrying(t *testing.T) {
	broken := at("a", 0)
	broken.Status, broken.Error = 0, "faultline: upstream api.stripe.com: connection refused"

	got := replay(t, broken, at("b", time.Second))

	if links(got) != "[b/a]" {
		t.Errorf("links = %s, want [b/a]; a call that never got a response is the clearest retry there is", links(got))
	}
}

func TestARealUpstreamFailureIsWorthRetryingToo(t *testing.T) {
	for name, status := range map[string]int{"server error": 502, "rate limit": 429} {
		t.Run(name, func(t *testing.T) {
			failure := at("a", 0)
			failure.Status = status // no rule of ours caused this

			got := replay(t, failure, at("b", time.Second))
			if links(got) != "[b/a]" {
				t.Errorf("links = %s, want [b/a]; a %d is a failure whether or not Faultline caused it", links(got), status)
			}
		})
	}
}

func TestClearingEventsForgetsPendingAttempts(t *testing.T) {
	r := NewRecorder(DefaultSize)
	t.Cleanup(r.Close)

	r.Record(faultedAt("a", 0))
	r.Clear()
	r.Record(at("b", time.Second))

	if links(r.Events()) != "[]" {
		t.Errorf("links = %s, want none; the attempt b could repeat is no longer part of the session", links(r.Events()))
	}
}

func TestRecordedRetryReachesSubscribers(t *testing.T) {
	r := NewRecorder(DefaultSize)
	t.Cleanup(r.Close)
	r.Record(faultedAt("a", 0))

	sub := r.Subscribe()
	r.Record(at("b", time.Second))

	if got := receive(t, sub); got.RetryOf != "a" {
		t.Errorf("streamed retry_of = %q, want a", got.RetryOf)
	}
}
