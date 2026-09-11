package admin

import (
	"strings"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
)

// ruleResponse is a rule as an endpoint about one rule returns it: the rule
// itself, plus anything worth saying about it that is not part of it. The rule's
// own fields stay at the top level, so the response is still a rule object.
type ruleResponse struct {
	rules.Rule
	Warnings []string `json:"warnings,omitempty"`
}

// respond returns the rule with whatever the caller should know about it.
func (s *Server) respond(r rules.Rule) ruleResponse {
	return ruleResponse{Rule: r, Warnings: s.warningsFor(r)}
}

// reportResponse is the session report plus anything standing in the way of the
// rules it was measured under. An empty faulted count next to a warning means
// the fault never applied, not that the application coped with it.
type reportResponse struct {
	events.Report
	Warnings []string `json:"warnings,omitempty"`
}

// reportWarnings names every enabled rule that cannot do anything as things
// stand. It is recomputed at read time because the answer changes with the
// traffic: a host nothing had been seen of when the rule was written may by now
// have been seen, and only encrypted.
func (s *Server) reportWarnings() []string {
	var warnings []string
	for _, r := range s.rules.List() {
		if !r.Enabled {
			continue
		}
		for _, warning := range s.warningsFor(r) {
			warnings = append(warnings, "rule "+r.ID+": "+warning)
		}
	}
	return warnings
}

// warningsFor names what stands in a rule's way without making it invalid. A
// response-tier fault on a host Faultline has only ever seen encrypted does
// nothing today, and will start working the moment the client trusts the CA, so
// it is worth saying and wrong to refuse.
func (s *Server) warningsFor(r rules.Rule) []string {
	fault, ok := faults.Get(string(r.Fault.Type))
	if !ok || fault.Tier() != faults.TierResponse {
		return nil
	}
	if r.Match.Host == "" || !s.onlyEncrypted(r.Match.Host) {
		return nil
	}
	return []string{
		r.Match.Host + " has only been seen encrypted, where a " + fault.Name() +
			" fault cannot apply; trust the Faultline CA so its traffic can be intercepted, see docs/trust.md",
	}
}

// onlyEncrypted reports whether a host has been seen, and never as anything but
// encrypted. A host nothing is known about yet gets no warning: the next request
// may well be intercepted.
func (s *Server) onlyEncrypted(host string) bool {
	if s.events == nil {
		return false
	}
	var seen bool
	for _, e := range s.events.Events() {
		if !strings.EqualFold(e.Host, host) {
			continue
		}
		if e.Tier != events.TierEncrypted {
			return false
		}
		seen = true
	}
	return seen
}
