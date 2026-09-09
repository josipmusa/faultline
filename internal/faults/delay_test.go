package faults

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDelayDurationWithoutJitterIsExact(t *testing.T) {
	got := (delayFault{MS: 250}).duration()
	if got != 250*time.Millisecond {
		t.Errorf("duration() = %v, want 250ms", got)
	}
}

func TestDelayDurationStaysWithinTheJitterWindow(t *testing.T) {
	f := delayFault{MS: 100, JitterMS: 50}
	var sawJitter bool
	for range 200 {
		got := f.duration()
		if got < 100*time.Millisecond || got >= 150*time.Millisecond {
			t.Fatalf("duration() = %v, want [100ms, 150ms)", got)
		}
		if got != 100*time.Millisecond {
			sawJitter = true
		}
	}
	if !sawJitter {
		t.Error("jitter was configured but every delay was the base duration")
	}
}

func TestDelayDurationOfANegativeFaultIsZero(t *testing.T) {
	// Validation lives at the API boundary; the pipeline must not turn a bad
	// value into a negative timer.
	if got := (delayFault{MS: -5}).duration(); got != 0 {
		t.Errorf("duration() = %v, want 0", got)
	}
	if got := (delayFault{MS: 10, JitterMS: -5}).duration(); got != 10*time.Millisecond {
		t.Errorf("duration() = %v, want 10ms", got)
	}
}

func TestDelayWaitWaits(t *testing.T) {
	start := time.Now()
	if err := (delayFault{MS: 40}).wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("waited %v, want at least 40ms", elapsed)
	}
}

func TestDelayWaitOfZeroReturnsImmediately(t *testing.T) {
	start := time.Now()
	if err := (delayFault{}).wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("waited %v for a zero delay", elapsed)
	}
}

func TestDelayWaitStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := (delayFault{MS: 30_000}).wait(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("returned after %v, the wait was not interrupted", elapsed)
	}
}

func TestDelayWaitOnAnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (delayFault{MS: 30_000}).wait(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
