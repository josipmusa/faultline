package faults

import (
	"context"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

// delay is the fault that holds traffic back before it reaches the upstream. It
// needs nothing but a connection, so it applies to encrypted traffic too.
type delay struct{}

func init() { Register(delay{}) }

func (delay) Name() string { return "delay" }

func (delay) Tier() Tier { return TierConnection }

func (delay) Schema() Schema {
	return Schema{
		Int("ms").Required().Min(1).Desc("How long to hold the request back, in milliseconds"),
		Int("jitter_ms").Min(0).Desc("Random extra delay on top, up to this many milliseconds"),
	}
}

func (delay) New(params rules.Params) (Applier, error) {
	var f delayFault
	if err := Decode(params, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// delayFault is a configured delay.
type delayFault struct {
	MS       int `json:"ms"`
	JitterMS int `json:"jitter_ms"`
}

// Respond holds the request, then lets it go upstream. A cancelled wait returns
// the context error and the request is never sent.
func (f delayFault) Respond(_ string, req *http.Request, next http.RoundTripper) (*http.Response, error) {
	if err := f.wait(req.Context()); err != nil {
		return nil, err
	}
	return next.RoundTrip(req)
}

// Dial holds the connection back, then opens it.
func (f delayFault) Dial(ctx context.Context, _, addr string, next DialFunc) (net.Conn, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	return next(ctx, "tcp", addr)
}

// wait holds for the fault's duration, or gives up early if the caller goes
// away, returning the context error the pipeline records and passes on.
func (f delayFault) wait(ctx context.Context) error {
	d := f.duration()
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

// duration is the base delay plus a random slice of the jitter, in the
// half-open range [MS, MS+JitterMS). Nonsensical values yield no delay rather
// than a negative one; the API boundary rejects them properly.
func (f delayFault) duration() time.Duration {
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
