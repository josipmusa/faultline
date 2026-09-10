package capture

import (
	"net/http"
	"strconv"
	"testing"
)

// sideWithBody carries no headers, so a test that budgets in whole bodies
// gets the arithmetic it expects. Header accounting has a test of its own.
func sideWithBody(n int) Side {
	return Side{Body: make([]byte, n)}
}

func TestStoreReturnsWhatItWasGiven(t *testing.T) {
	s := NewStore(0)
	s.Put(Capture{EventID: "1", Request: Side{
		Headers: http.Header{"X-Test": []string{"1"}},
		Body:    []byte("hello"),
	}})

	got, ok := s.Get("1")
	if !ok {
		t.Fatal("Get(1) found nothing, want the capture just put")
	}
	if string(got.Request.Body) != "hello" {
		t.Errorf("request body = %q, want %q", got.Request.Body, "hello")
	}
	if got.Request.Headers.Get("X-Test") != "1" {
		t.Errorf("X-Test = %q, want 1", got.Request.Headers.Get("X-Test"))
	}
}

func TestStoreReportsAnIdItNeverHeld(t *testing.T) {
	if _, ok := NewStore(0).Get("nope"); ok {
		t.Error("Get on an empty store found something")
	}
}

func TestStoreEvictsOldestOnceTheBudgetIsSpent(t *testing.T) {
	// A budget of three bodies, filled by four.
	body := 1 << 10
	s := NewStore(int64(3 * body))

	for i := 1; i <= 4; i++ {
		s.Put(Capture{EventID: strconv.Itoa(i), Response: sideWithBody(body)})
	}

	if _, ok := s.Get("1"); ok {
		t.Error("the oldest capture survived a full budget, want it evicted")
	}
	for _, id := range []string{"2", "3", "4"} {
		if _, ok := s.Get(id); !ok {
			t.Errorf("capture %s was evicted, want it held", id)
		}
	}
}

func TestStoreCompletesTheResponseBodyAfterThePut(t *testing.T) {
	s := NewStore(0)
	s.Put(Capture{EventID: "7", Response: Side{Headers: http.Header{}}})
	s.Complete("7", []byte("late"), true)

	got, ok := s.Get("7")
	if !ok {
		t.Fatal("Get(7) found nothing")
	}
	if string(got.Response.Body) != "late" {
		t.Errorf("response body = %q, want %q", got.Response.Body, "late")
	}
	if !got.Response.Truncated {
		t.Error("response truncated = false, want true")
	}
}

func TestCompleteOnAnEvictedIdDoesNothing(t *testing.T) {
	s := NewStore(1 << 10)
	s.Put(Capture{EventID: "1", Response: sideWithBody(1 << 10)})
	s.Put(Capture{EventID: "2", Response: sideWithBody(1 << 10)})

	s.Complete("1", []byte("late"), false) // 1 is gone; this must not resurrect it

	if _, ok := s.Get("1"); ok {
		t.Error("Complete resurrected an evicted capture")
	}
}

func TestCompleteKeepsTheBudgetHonest(t *testing.T) {
	body := 1 << 10
	s := NewStore(int64(2 * body))

	s.Put(Capture{EventID: "1", Response: Side{Headers: http.Header{}}})
	s.Put(Capture{EventID: "2", Response: Side{Headers: http.Header{}}})
	// Both bodies arrive late and together they overrun the budget.
	s.Complete("1", make([]byte, body), false)
	s.Complete("2", make([]byte, 2*body), false)

	if _, ok := s.Get("1"); ok {
		t.Error("a late body overran the budget without evicting the oldest capture")
	}
	if _, ok := s.Get("2"); !ok {
		t.Error("the newest capture was evicted, want it held")
	}
}

func TestClearDropsEverything(t *testing.T) {
	s := NewStore(0)
	s.Put(Capture{EventID: "1", Response: sideWithBody(1 << 10)})
	s.Clear()

	if _, ok := s.Get("1"); ok {
		t.Error("Get found a capture after Clear")
	}
	// The budget must come back with it, or a cleared store slowly starves.
	for i := range 100 {
		s.Put(Capture{EventID: strconv.Itoa(100 + i), Response: sideWithBody(1 << 10)})
	}
	if _, ok := s.Get("199"); !ok {
		t.Error("the store stopped holding captures after Clear, want the budget released")
	}
}

func TestHeadersCountAgainstTheBudget(t *testing.T) {
	// Two captures with no body at all, whose headers alone overrun a tiny
	// budget: a flood of header-only exchanges must not grow without bound.
	s := NewStore(16)
	for _, id := range []string{"1", "2"} {
		s.Put(Capture{EventID: id, Request: Side{Headers: http.Header{
			"Authorization": []string{"Bearer aaaaaaaaaaaaaaaa"},
		}}})
	}

	if _, ok := s.Get("1"); ok {
		t.Error("a header-only capture stayed inside a budget its headers overrun")
	}
	if _, ok := s.Get("2"); !ok {
		t.Error("the newest capture was evicted, want it held")
	}
}
