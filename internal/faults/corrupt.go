package faults

import (
	"io"
	"math/rand/v2"
	"net/http"

	"github.com/josipmusa/faultline/internal/rules"
)

// corrupt flips bits in a real response's body. The transfer stays valid down
// to its length, so nothing below the application notices: the fault lands on
// whatever parses the bytes, which is where a mangled payload has to be handled.
type corrupt struct{}

func init() { Register(corrupt{}) }

func (corrupt) Name() string { return "corrupt" }

func (corrupt) Tier() Tier { return TierResponse }

func (corrupt) Schema() Schema {
	return Schema{
		Int("percent").Required().Min(1).Max(100).Desc("How much of the body to mangle, as a percentage of its bytes"),
	}
}

func (corrupt) New(params rules.Params) (Applier, error) {
	var f corruptFault
	if err := Decode(params, &f); err != nil {
		return nil, err
	}
	return f, nil
}

type corruptFault struct {
	Percent int `json:"percent"`
}

func (f corruptFault) Respond(_ string, req *http.Request, next http.RoundTripper) (*http.Response, error) {
	resp, err := next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &corruptedBody{ReadCloser: resp.Body, percent: f.Percent}
	return resp, nil
}

// corruptedBody flips one bit in the given share of the bytes it passes on.
// Flipping rather than replacing keeps the body the length it claims to be.
type corruptedBody struct {
	io.ReadCloser
	percent int
}

func (b *corruptedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	for i := range p[:n] {
		if rand.IntN(100) < b.percent { //nolint:gosec // corruption wants to be arbitrary, not unguessable
			p[i] ^= 1 << rand.IntN(8) //nolint:gosec // same
		}
	}
	return n, err
}
