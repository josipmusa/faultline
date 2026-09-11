package client_test

import (
	"context"
	"errors"
	"testing"
	"time"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/events"
)

// TestWaitForEventReturnsOneThatArrivesDuringTheWait is the case the tool
// exists for: the agent starts waiting, the application then makes the call.
func TestWaitForEventReturnsOneThatArrivesDuringTheWait(t *testing.T) {
	h := newHarness(t)

	go func() {
		time.Sleep(150 * time.Millisecond)
		record(t, h, events.NextID(), "api.stripe.com", true)
	}()

	got, err := h.client.WaitForEvent(context.Background(),
		client.EventQuery{Host: "api.stripe.com"}, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForEvent: %v", err)
	}
	if got.Host != "api.stripe.com" {
		t.Errorf("WaitForEvent() host = %q, want api.stripe.com", got.Host)
	}
}

// An event already in the ring is not what the caller asked about. Waiting is
// about what happens next, or an agent that polls twice gets the same old
// request and concludes the application called twice.
func TestWaitForEventIgnoresWhatWasAlreadyRecorded(t *testing.T) {
	h := newHarness(t)
	record(t, h, events.NextID(), "api.stripe.com", true)

	_, err := h.client.WaitForEvent(context.Background(),
		client.EventQuery{Host: "api.stripe.com"}, 300*time.Millisecond)

	if !errors.Is(err, client.ErrWaitTimeout) {
		t.Errorf("WaitForEvent() error = %v, want ErrWaitTimeout", err)
	}
}

// Nothing matching within the window is an answer, not a failure: it means the
// application made no such call, which is often what the agent is checking.
func TestWaitForEventTimesOutCleanly(t *testing.T) {
	h := newHarness(t)

	start := time.Now()
	_, err := h.client.WaitForEvent(context.Background(),
		client.EventQuery{Host: "nothing.example.com"}, 300*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, client.ErrWaitTimeout) {
		t.Fatalf("WaitForEvent() error = %v, want ErrWaitTimeout", err)
	}
	if elapsed < 300*time.Millisecond {
		t.Errorf("waited %v, want at least the 300ms timeout", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("waited %v, want the timeout to end it promptly", elapsed)
	}
}

func TestWaitForEventAppliesTheFilter(t *testing.T) {
	h := newHarness(t)
	faulted := true

	go func() {
		time.Sleep(100 * time.Millisecond)
		record(t, h, events.NextID(), "api.stripe.com", false)
		time.Sleep(100 * time.Millisecond)
		record(t, h, events.NextID(), "api.stripe.com", true)
	}()

	got, err := h.client.WaitForEvent(context.Background(),
		client.EventQuery{Host: "api.stripe.com", Faulted: &faulted}, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForEvent: %v", err)
	}
	if !got.Faulted {
		t.Errorf("WaitForEvent() returned an unfaulted event, want the faulted one")
	}
}

func TestWaitForEventStopsWhenTheContextIsCancelled(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := h.client.WaitForEvent(ctx, client.EventQuery{}, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("WaitForEvent() error = %v, want context.Canceled", err)
	}
}
