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

// syntheticResponse builds the response a status fault returns instead of
// calling upstream. It is not a mock: the rule that produced it is named in the
// header, and it only ever stands in for a call the user asked to break.
func syntheticResponse(req *http.Request, r rules.Rule) *http.Response {
	return synthetic(req, r.ID, r.Fault.Code, r.Fault.Body)
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
