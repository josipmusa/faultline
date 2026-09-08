package events

import (
	"fmt"
	"testing"
)

func ev(id string) Event {
	return Event{ID: id, Host: "api.stripe.com", Method: "GET", Path: "/v1/charges"}
}

func recordIDs(r *ringBuffer, ids ...string) {
	for _, id := range ids {
		r.add(ev(id))
	}
}

func gotIDs(es []Event) string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return fmt.Sprint(out)
}

func TestRingEmpty(t *testing.T) {
	r := newRingBuffer(3)

	got := r.all()
	if len(got) != 0 {
		t.Errorf("all() of an empty ring = %v, want empty", got)
	}
	if got == nil {
		t.Error("all() should return an empty slice, not nil, so it encodes as []")
	}
}

func TestRingBelowCapacity(t *testing.T) {
	r := newRingBuffer(3)
	recordIDs(r, "a", "b")

	if got := gotIDs(r.all()); got != "[a b]" {
		t.Errorf("all() = %v, want [a b]", got)
	}
}

func TestRingExactlyCapacity(t *testing.T) {
	r := newRingBuffer(3)
	recordIDs(r, "a", "b", "c")

	if got := gotIDs(r.all()); got != "[a b c]" {
		t.Errorf("all() = %v, want [a b c]", got)
	}
}

func TestRingWrapsAroundKeepingNewest(t *testing.T) {
	r := newRingBuffer(3)
	recordIDs(r, "a", "b", "c", "d", "e")

	if got := gotIDs(r.all()); got != "[c d e]" {
		t.Errorf("all() = %v, want [c d e] oldest first", got)
	}
}

func TestRingWrapsManyTimes(t *testing.T) {
	r := newRingBuffer(2)
	for i := range 100 {
		r.add(ev(fmt.Sprint(i)))
	}

	if got := gotIDs(r.all()); got != "[98 99]" {
		t.Errorf("all() = %v, want [98 99]", got)
	}
}

func TestRingSizeOne(t *testing.T) {
	r := newRingBuffer(1)
	recordIDs(r, "a", "b")

	if got := gotIDs(r.all()); got != "[b]" {
		t.Errorf("all() = %v, want [b]", got)
	}
}

func TestRingNonPositiveSizeFallsBackToDefault(t *testing.T) {
	for _, size := range []int{0, -1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			r := newRingBuffer(size)

			r.add(ev("a")) // the predecessor's buffer divided by zero here
			if got := gotIDs(r.all()); got != "[a]" {
				t.Errorf("all() = %v, want [a]", got)
			}
			if r.size != DefaultSize {
				t.Errorf("size = %d, want the default %d", r.size, DefaultSize)
			}
		})
	}
}

func TestRingAllReturnsAFreshSlice(t *testing.T) {
	r := newRingBuffer(3)
	recordIDs(r, "a", "b")

	first := r.all()
	first[0].ID = "tampered"

	if got := gotIDs(r.all()); got != "[a b]" {
		t.Errorf("all() = %v, want [a b]; the caller must not be able to write through", got)
	}
}
