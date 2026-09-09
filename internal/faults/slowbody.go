package faults

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

// quantum is how long one piece of a slow body takes to arrive. It is short
// enough that the response visibly trickles and long enough that a duration of
// several seconds does not turn into thousands of writes.
const quantum = 50 * time.Millisecond

// slowBody delivers a correct response over a configured duration. Nothing
// about the response is wrong, which is the point: it is the fault for a client
// whose read timeout, or lack of one, only shows up when the bytes come slowly.
type slowBody struct{}

func init() { Register(slowBody{}) }

func (slowBody) Name() string { return "slow_body" }

func (slowBody) Tier() Tier { return TierResponse }

func (slowBody) Schema() Schema {
	return Schema{
		Int("ms").Required().Min(1),
	}
}

func (slowBody) New(params rules.Params) (Applier, error) {
	var f slowBodyFault
	if err := Decode(params, &f); err != nil {
		return nil, err
	}
	return f, nil
}

type slowBodyFault struct {
	MS int `json:"ms"`
}

func (f slowBodyFault) Respond(_ string, req *http.Request, next http.RoundTripper) (*http.Response, error) {
	resp, err := next.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Spreading a body over a duration means knowing how much body there is,
	// so an upstream that declared no length has to be measured first.
	if resp.ContentLength < 0 {
		if err := measureBody(resp); err != nil {
			return nil, err
		}
	}

	resp.Body = &slowedBody{
		ReadCloser: resp.Body,
		spread:     newSpread(req.Context(), resp.ContentLength, time.Duration(f.MS)*time.Millisecond),
	}
	return resp, nil
}

type slowedBody struct {
	io.ReadCloser
	spread *spread
}

func (b *slowedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(b.spread.slice(p))
	if waitErr := b.spread.wait(n); waitErr != nil && err == nil {
		err = waitErr
	}
	return n, err
}

// spread paces a body of a known size so its last byte arrives when the
// duration is up. Unlike a throttle, which fixes a rate and lets the size
// decide how long the transfer takes, a spread fixes the time and lets the size
// decide the rate.
type spread struct {
	ctx     context.Context //nolint:containedctx // the wait happens during a read, not at construction
	total   int64
	over    time.Duration
	chunk   int
	started time.Time
	read    int64
}

func newSpread(ctx context.Context, total int64, over time.Duration) *spread {
	steps := max(int64(over/quantum), 1)
	return &spread{
		ctx:   ctx,
		total: total,
		over:  over,
		chunk: int(max(total/steps, 1)),
	}
}

// slice shortens a read buffer to one piece of the body.
func (s *spread) slice(b []byte) []byte {
	if len(b) > s.chunk {
		return b[:s.chunk]
	}
	return b
}

// wait holds until the bytes just read were due, which is the point in the
// duration matching how much of the body they complete.
func (s *spread) wait(n int) error {
	if n <= 0 || s.total <= 0 {
		return nil
	}
	if s.started.IsZero() {
		s.started = time.Now() // the first read is what starts the clock
	}
	s.read += int64(n)

	done := float64(min(s.read, s.total)) / float64(s.total)
	return waitUntil(s.ctx, s.started.Add(time.Duration(done*float64(s.over))))
}
