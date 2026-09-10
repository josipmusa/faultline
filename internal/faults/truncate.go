package faults

import (
	"io"
	"net/http"

	"github.com/josipmusa/faultline/internal/rules"
)

// truncate delivers the start of a real response and then stops, leaving the
// original Content-Length in place. The client is told how much to expect and
// gets less, which is what a dependency that dies mid-transfer looks like from
// the other end: a clean status line, a broken body.
type truncate struct{}

func init() { Register(truncate{}) }

func (truncate) Name() string { return "truncate" }

func (truncate) Tier() Tier { return TierResponse }

func (truncate) Schema() Schema {
	return Schema{
		Int("after_bytes").Min(0).Xor("percent").Desc("Cut the body off after this many bytes"),
		Int("percent").Min(1).Max(99).Desc("Cut the body off after this share of it, as a percentage"),
	}
}

func (truncate) New(params rules.Params) (Applier, error) {
	var f truncateFault
	if err := Decode(params, &f); err != nil {
		return nil, err
	}
	return f, nil
}

type truncateFault struct {
	AfterBytes int `json:"after_bytes"`
	Percent    int `json:"percent"`
}

func (f truncateFault) Respond(_ string, req *http.Request, next http.RoundTripper) (*http.Response, error) {
	resp, err := next.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// A share of the body is a share of a length, so an upstream that declared
	// none has to be measured before the cut can be placed.
	if f.Percent > 0 && resp.ContentLength < 0 {
		if err := measureBody(resp); err != nil {
			return nil, err
		}
	}

	resp.Body = &truncatedBody{ReadCloser: resp.Body, left: f.cut(resp.ContentLength)}
	return resp, nil
}

// cut is the number of bytes to deliver out of a body of the given length.
func (f truncateFault) cut(length int64) int64 {
	if f.Percent > 0 {
		return length * int64(f.Percent) / 100
	}
	return int64(f.AfterBytes)
}

// truncatedBody ends a body early. It reports the end as a plain EOF rather
// than an error, because the break the client sees comes from the bytes that
// never arrived against the length the response promised.
type truncatedBody struct {
	io.ReadCloser
	left int64
}

func (b *truncatedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.ReadCloser.Read(p)
	b.left -= int64(n)
	if b.left <= 0 {
		return n, io.EOF
	}
	return n, err
}
