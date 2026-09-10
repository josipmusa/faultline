package faults

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/josipmusa/faultline/internal/rules"
)

// FaultHeader marks a response Faultline made up. Every synthetic response
// carries it so nobody has to guess whether an upstream really returned a 503.
const FaultHeader = "Faultline-Fault"

// status is the fault that answers a request with a status code of its own
// instead of calling the upstream. It needs to see the request, so it does
// nothing to traffic that stays encrypted.
type status struct{}

func init() { Register(status{}) }

func (status) Name() string { return "status" }

func (status) Tier() Tier { return TierResponse }

func (status) Schema() Schema {
	return Schema{
		Int("code").Required().Min(100).Max(599).Desc("The status code to answer with instead of the upstream's"),
		Str("body").Desc("The body to answer with. Left out, the response has none"),
	}
}

func (status) New(params rules.Params) (Applier, error) {
	var f statusFault
	if err := Decode(params, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// statusFault is a configured status fault.
type statusFault struct {
	Code int    `json:"code"`
	Body string `json:"body"`
}

// Respond answers instead of calling upstream. It is not a mock: the rule that
// produced it is named in the header, and it only ever stands in for a call the
// user asked to break.
func (f statusFault) Respond(ruleID string, req *http.Request, _ http.RoundTripper) (*http.Response, error) {
	return synthetic(req, ruleID, f.Code, f.Body), nil
}

// synthetic is the one place a response is made up. The rule id always travels
// in FaultHeader, whatever the fault.
func synthetic(req *http.Request, ruleID string, code int, body string) *http.Response {
	header := http.Header{FaultHeader: []string{ruleID}}
	if body != "" {
		header.Set("Content-Type", "text/plain; charset=utf-8")
	}

	return &http.Response{
		Status:        fmt.Sprintf("%d %s", code, http.StatusText(code)),
		StatusCode:    code,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
