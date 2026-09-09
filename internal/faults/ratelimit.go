package faults

import (
	"net/http"
	"strconv"

	"github.com/josipmusa/faultline/internal/rules"
)

// rateLimit answers a request the way a provider that has had enough does: 429
// with a Retry-After hint. It is deliberately narrower than status, because the
// point of the fault is the hint, which is what a client's backoff reads.
type rateLimit struct{}

func init() { Register(rateLimit{}) }

func (rateLimit) Name() string { return "rate_limit" }

func (rateLimit) Tier() Tier { return TierResponse }

func (rateLimit) Schema() Schema {
	return Schema{
		Int("retry_after_sec").Required().Min(0),
	}
}

func (rateLimit) New(params rules.Params) (Applier, error) {
	var f rateLimitFault
	if err := Decode(params, &f); err != nil {
		return nil, err
	}
	return f, nil
}

type rateLimitFault struct {
	RetryAfterSec int `json:"retry_after_sec"`
}

// Respond answers on its own, so the upstream never sees the request: a client
// being rate limited is a client whose call did not happen.
func (f rateLimitFault) Respond(ruleID string, req *http.Request, _ http.RoundTripper) (*http.Response, error) {
	resp := synthetic(req, ruleID, http.StatusTooManyRequests, "")
	resp.Header.Set("Retry-After", strconv.Itoa(f.RetryAfterSec))
	return resp, nil
}
