package faults

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/josipmusa/faultline/internal/rules"
)

// refuse is the fault that turns a connection away, as if the upstream were not
// listening. It needs nothing but a connection, so it applies to encrypted
// traffic too, and it takes no parameters.
type refuse struct{}

func init() { Register(refuse{}) }

func (refuse) Name() string { return "refuse" }

func (refuse) Tier() Tier { return TierConnection }

func (refuse) Schema() Schema { return Schema{} }

func (refuse) New(rules.Params) (Applier, error) { return refuseFault{}, nil }

// refuseFault is a configured refusal. It has nothing to configure.
type refuseFault struct{}

// RefusedError says a rule turned the connection away before it was dialed.
// The proxy turns it into the answer a client would get from an upstream that
// is not listening, plus the rule id.
type RefusedError struct {
	Host   string
	RuleID string
}

func (e *RefusedError) Error() string {
	return fmt.Sprintf("connection to %s refused by rule %s", e.Host, e.RuleID)
}

// Respond answers the way a proxy answers for an upstream that is not
// listening. The real thing reaches a client as a 502; this one differs only in
// carrying the rule that caused it.
func (refuseFault) Respond(ruleID string, req *http.Request, _ http.RoundTripper) (*http.Response, error) {
	host := hostOf(req)
	return synthetic(req, ruleID, http.StatusBadGateway, refusedMessage(host, ruleID)), nil
}

// Dial never opens the connection.
func (refuseFault) Dial(_ context.Context, ruleID, addr string, _ DialFunc) (net.Conn, error) {
	return nil, &RefusedError{Host: StripDefaultPort(addr), RuleID: ruleID}
}

// refusedMessage is what a client reads when a rule refused its connection. The
// same words go to a plain HTTP client and to one that asked for a tunnel.
func refusedMessage(host, ruleID string) string {
	return fmt.Sprintf("faultline: connection to %s refused by rule %s\n", host, ruleID)
}
