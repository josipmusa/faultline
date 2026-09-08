package faults

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

func TestDelayDurationWithoutJitterIsExact(t *testing.T) {
	got := delayDuration(rules.Fault{MS: 250})
	if got != 250*time.Millisecond {
		t.Errorf("delayDuration = %v, want 250ms", got)
	}
}

func TestDelayDurationStaysWithinTheJitterWindow(t *testing.T) {
	f := rules.Fault{MS: 100, JitterMS: 50}
	var sawJitter bool
	for range 200 {
		got := delayDuration(f)
		if got < 100*time.Millisecond || got >= 150*time.Millisecond {
			t.Fatalf("delayDuration = %v, want [100ms, 150ms)", got)
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
	if got := delayDuration(rules.Fault{MS: -5}); got != 0 {
		t.Errorf("delayDuration = %v, want 0", got)
	}
	if got := delayDuration(rules.Fault{MS: 10, JitterMS: -5}); got != 10*time.Millisecond {
		t.Errorf("delayDuration = %v, want 10ms", got)
	}
}

func TestApplyDelayWaits(t *testing.T) {
	start := time.Now()
	if err := applyDelay(context.Background(), rules.Fault{MS: 40}); err != nil {
		t.Fatalf("applyDelay: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("waited %v, want at least 40ms", elapsed)
	}
}

func TestApplyDelayOfZeroReturnsImmediately(t *testing.T) {
	start := time.Now()
	if err := applyDelay(context.Background(), rules.Fault{}); err != nil {
		t.Fatalf("applyDelay: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("waited %v for a zero delay", elapsed)
	}
}

func TestApplyDelayStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := applyDelay(ctx, rules.Fault{MS: 30_000})
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("returned after %v, the wait was not interrupted", elapsed)
	}
}

func TestApplyDelayOnAnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := applyDelay(ctx, rules.Fault{MS: 30_000}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
