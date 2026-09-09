package faults

import (
	"fmt"
	"net/http"
)

// refusedMessage is what a client reads when a rule refused its connection. The
// same words go to a plain HTTP client and to one that asked for a tunnel.
func refusedMessage(host, ruleID string) string {
	return fmt.Sprintf("faultline: connection to %s refused by rule %s\n", host, ruleID)
}

// refusedResponse is what a refuse fault answers on the plain tier. The real
// thing, an upstream not listening, reaches a client through a proxy as a 502;
// this one differs only in carrying the rule that caused it.
func refusedResponse(req *http.Request, host, ruleID string) *http.Response {
	return synthetic(req, ruleID, http.StatusBadGateway, refusedMessage(host, ruleID))
}
