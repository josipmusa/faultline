package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// receive reads one event, failing if none arrives promptly.
func receive(t *testing.T, sub *Subscription) Event {
	t.Helper()
	select {
	case e, ok := <-sub.C:
		if !ok {
			t.Fatal("subscription channel closed unexpectedly")
		}
		return e
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for an event")
		return Event{}
	}
}

func expectQuiet(t *testing.T, sub *Subscription) {
	t.Helper()
	select {
	case e := <-sub.C:
		t.Errorf("unexpected event %q", e.ID)
	default:
	}
}

func TestRecordStoresAndReturnsInOrder(t *testing.T) {
	r := NewRecorder(10)

	r.Record(ev("a"))
	r.Record(ev("b"))

	if got := gotIDs(r.Events()); got != "[a b]" {
		t.Errorf("Events() = %v, want [a b]", got)
	}
}

func TestRecorderHonoursSize(t *testing.T) {
	r := NewRecorder(2)

	r.Record(ev("a"))
	r.Record(ev("b"))
	r.Record(ev("c"))

	if got := gotIDs(r.Events()); got != "[b c]" {
		t.Errorf("Events() = %v, want [b c]", got)
	}
}

func TestFanOutReachesEverySubscriber(t *testing.T) {
	r := NewRecorder(10)
	one, two := r.Subscribe(), r.Subscribe()

	r.Record(ev("a"))

	if got := receive(t, one).ID; got != "a" {
		t.Errorf("subscriber one got %q, want a", got)
	}
	if got := receive(t, two).ID; got != "a" {
		t.Errorf("subscriber two got %q, want a", got)
	}
}

func TestSubscriberSeesOnlyEventsAfterSubscribing(t *testing.T) {
	r := NewRecorder(10)
	r.Record(ev("before"))

	sub := r.Subscribe()
	r.Record(ev("after"))

	if got := receive(t, sub).ID; got != "after" {
		t.Errorf("got %q, want after; the stream is live, history comes from Events()", got)
	}
}

func TestCloseSubscriptionStopsDelivery(t *testing.T) {
	r := NewRecorder(10)
	kept, dropped := r.Subscribe(), r.Subscribe()

	dropped.Close()
	r.Record(ev("a"))

	if got := receive(t, kept).ID; got != "a" {
		t.Errorf("the remaining subscriber got %q, want a", got)
	}
	if _, ok := <-dropped.C; ok {
		t.Error("a closed subscription must not deliver events")
	}
}

func TestCloseSubscriptionTwiceIsSafe(t *testing.T) {
	r := NewRecorder(10)
	sub := r.Subscribe()

	sub.Close()
	sub.Close() // must not panic closing an already closed channel

	r.Record(ev("a"))

	if _, ok := <-sub.C; ok {
		t.Error("the subscription should still be closed")
	}
	if got := gotIDs(r.Events()); got != "[a]" {
		t.Errorf("Events() = %v, want [a]; the recorder should still work", got)
	}
}

func TestRecorderCloseClosesEverySubscription(t *testing.T) {
	r := NewRecorder(10)
	one, two := r.Subscribe(), r.Subscribe()

	r.Close()

	if _, ok := <-one.C; ok {
		t.Error("subscriber one should see a closed channel")
	}
	if _, ok := <-two.C; ok {
		t.Error("subscriber two should see a closed channel")
	}
}

func TestRecorderCloseTwiceIsSafe(t *testing.T) {
	r := NewRecorder(10)
	sub := r.Subscribe()

	r.Close()
	r.Close()   // the predecessor panicked here, its listener slice was never cleared
	sub.Close() // and closing the subscription afterwards must be safe too

	if _, ok := <-sub.C; ok {
		t.Error("the subscription should be closed")
	}
}

func TestRecordAfterCloseStillBuffers(t *testing.T) {
	r := NewRecorder(10)
	r.Subscribe()
	r.Close()

	r.Record(ev("a"))

	if got := gotIDs(r.Events()); got != "[a]" {
		t.Errorf("Events() = %v, want [a]", got)
	}
}

func TestSlowSubscriberDropsEventsButBufferKeepsThem(t *testing.T) {
	r := NewRecorder(1000)
	sub := r.Subscribe() // nobody reads from it

	const extra = 10
	for i := range subscriberBacklog + extra {
		r.Record(ev(fmt.Sprint(i)))
	}

	// The channel holds the oldest events it could fit; the rest were dropped
	// rather than blocking the request path.
	for i := range subscriberBacklog {
		if got := receive(t, sub).ID; got != fmt.Sprint(i) {
			t.Fatalf("event %d in the stream is %q, want %d", i, got, i)
		}
	}
	expectQuiet(t, sub)

	// The ring buffer is the source of truth and lost nothing.
	if n := len(r.Events()); n != subscriberBacklog+extra {
		t.Errorf("buffer holds %d events, want %d", n, subscriberBacklog+extra)
	}
}

func TestRecordNeverBlocksOnASlowSubscriber(t *testing.T) {
	r := NewRecorder(10)
	r.Subscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range subscriberBacklog * 3 {
			r.Record(ev(fmt.Sprint(i)))
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Record blocked on a subscriber that never reads")
	}
}

func TestConcurrentRecordSubscribeAndRead(t *testing.T) {
	r := NewRecorder(100)
	const writers, perWriter = 8, 50

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perWriter {
				r.Record(ev(fmt.Sprintf("w%d-%d", w, i)))
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub := r.Subscribe()
			defer sub.Close()
			for range perWriter {
				_ = r.Events()
				select {
				case <-sub.C:
				default:
				}
			}
		}()
	}
	wg.Wait()

	if n := len(r.Events()); n != 100 {
		t.Errorf("buffer holds %d events, want it full at 100", n)
	}
}

func TestEventJSONShape(t *testing.T) {
	e := Event{
		ID:         "e1",
		Timestamp:  time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
		Host:       "api.stripe.com",
		Method:     "POST",
		Path:       "/v1/charges",
		Status:     503,
		DurationMS: 2001,
		BytesIn:    120,
		BytesOut:   45,
		Faulted:    true,
		RuleID:     "slow-stripe",
		Tier:       TierIntercepted,
	}

	out, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"e1","timestamp":"2026-09-08T12:00:00Z","host":"api.stripe.com",` +
		`"method":"POST","path":"/v1/charges","status":503,"duration_ms":2001,` +
		`"bytes_in":120,"bytes_out":45,"faulted":true,"rule_id":"slow-stripe","tier":"intercepted"}`
	if string(out) != want {
		t.Errorf("marshal:\n got %s\nwant %s", out, want)
	}
}

func TestUnfaultedEventOmitsRuleID(t *testing.T) {
	e := Event{ID: "e1", Tier: TierEncrypted}

	out, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(out); strings.Contains(got, "rule_id") {
		t.Errorf("an unfaulted event should carry no rule_id: %s", got)
	}
}

func TestRecorderClearDropsTheHistory(t *testing.T) {
	r := NewRecorder(4)
	defer r.Close()

	sub := r.Subscribe()
	r.Record(Event{ID: "1"})
	r.Record(Event{ID: "2"})

	r.Clear()

	if got := r.Events(); len(got) != 0 {
		t.Fatalf("events after clear = %+v, want none", got)
	}

	r.Record(Event{ID: "3"})
	got := r.Events()
	if len(got) != 1 || got[0].ID != "3" {
		t.Errorf("events = %+v, want only the one recorded after the clear", got)
	}

	// Clearing the history does not disturb a live subscriber.
	for _, want := range []string{"1", "2", "3"} {
		select {
		case e := <-sub.C:
			if e.ID != want {
				t.Errorf("subscriber saw %q, want %q", e.ID, want)
			}
		default:
			t.Errorf("subscriber missed event %q", want)
		}
	}
}
