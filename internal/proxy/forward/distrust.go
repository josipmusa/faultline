package forward

import (
	"strings"

	"github.com/josipmusa/faultline/internal/events"
)

// distrustHint is what a rejected handshake means in practice: the client saw
// a certificate signed by a CA it does not trust, or it pinned the real one
// and would reject any substitute. Both are trust problems on the client's
// side, and the document covers both.
const distrustHint = "client did not trust the Faultline CA; see docs/trust.md"

// Distrusted reports whether an event is a handshake the client walked away
// from. Those events carry no status and no path, so without a hint there is
// nothing in them that says what to do next.
func Distrusted(e events.Event) bool { return e.Error == ErrClientRejectedCertificate }

// DistrustHint is the advice for a host whose client rejected the interception
// certificate. trustVars are the variables Faultline set for the child, named
// in the hint so the reader does not start by pointing the runtime at the CA
// that it was already pointed at: a runtime that ignores them, like Go on
// macOS, needs the CA in the system trust store instead.
func DistrustHint(trustVars []string) string {
	if len(trustVars) == 0 {
		return distrustHint
	}
	return distrustHint + " (Faultline set " + strings.Join(trustVars, ", ") + ")"
}
