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

// Warnings names every enabled rule that cannot do anything as things stand,
// each line leading with the rule's id. It is what the report carries, offered
// to whoever brings the instance up so a rule loaded from a file that can never
// fire is said at startup rather than discovered at the end of a run.
func (s *Server) Warnings() []string { return s.reportWarnings() }

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

// warningsFor names what stands in a rule's way without making it invalid.
// Both are things the rule cannot know about itself: a response-tier fault on
// a host Faultline has only ever seen encrypted does nothing today, and will
// start working the moment the client trusts the CA; a rule written below one
// that already takes every request it would match never gets a turn. Either
// way a faulted count of zero would read as an application that coped, so they
// are worth saying and wrong to refuse.
func (s *Server) warningsFor(r rules.Rule) []string {
	var warnings []string
	if shadow, ok := s.shadowedBy(r); ok {
		warnings = append(warnings, "never fires: rule "+shadow.ID+" comes before it, matches everything it matches "+
			"and has no behavior, so it decides every one of those requests first; give "+shadow.ID+
			" a behavior, narrow its match, or move this rule above it")
	}
	if warning, ok := s.encryptedOnly(r); ok {
		warnings = append(warnings, warning)
	}
	return warnings
}

// shadowedBy finds the rule that keeps r from ever firing: an enabled rule
// earlier in the order whose match covers r's and which has no behavior, so it
// never declines a request and never hands one on. A rule with a behavior can
// shadow r on some requests, and that is the point of rule order rather than a
// mistake, so it is not named.
func (s *Server) shadowedBy(r rules.Rule) (rules.Rule, bool) {
	for _, p := range s.rules.List() {
		if p.ID == r.ID {
			break
		}
		if p.Enabled && p.Behavior == nil && p.Match.Covers(r.Match) {
			return p, true
		}
	}
	return rules.Rule{}, false
}

// encryptedOnly is the warning for a response-tier fault on a host only ever
// seen encrypted, where the fault cannot apply until the client trusts the CA.
func (s *Server) encryptedOnly(r rules.Rule) (string, bool) {
	fault, ok := faults.Get(string(r.Fault.Type))
	if !ok || fault.Tier() != faults.TierResponse {
		return "", false
	}
	if r.Match.Host == "" || !s.onlyEncrypted(r.Match.Host) {
		return "", false
	}
	return r.Match.Host + " has only been seen encrypted, where a " + fault.Name() +
		" fault cannot apply; trust the Faultline CA so its traffic can be intercepted, see docs/trust.md", true
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
