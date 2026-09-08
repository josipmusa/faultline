package faults

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

// applyDelay holds the request for the fault's duration, or gives up early if
// the caller goes away. A cancelled wait returns the context error, which the
// pipeline records and passes on: the request never reaches upstream.
func applyDelay(ctx context.Context, f rules.Fault) error {
	d := delayDuration(f)
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// delayDuration is the fault's base delay plus a random slice of its jitter, in
// the half-open range [ms, ms+jitter_ms). Nonsensical values yield no delay
// rather than a negative one; the API boundary rejects them properly.
func delayDuration(f rules.Fault) time.Duration {
	if f.MS <= 0 {
		return 0
	}
	d := time.Duration(f.MS) * time.Millisecond
	if f.JitterMS > 0 {
		// Jitter is timing variety, not entropy: a weak source is the right one.
		d += time.Duration(rand.IntN(f.JitterMS)) * time.Millisecond //nolint:gosec
	}
	return d
}
