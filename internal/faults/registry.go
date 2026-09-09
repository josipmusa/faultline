package faults

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"

	"github.com/josipmusa/faultline/internal/rules"
)

// Tier is how much of the traffic a fault needs to see. A connection-tier fault
// works on every tier, encrypted traffic included, because it needs nothing but
// the connection. A response-tier fault needs Faultline to see the request, so
// it does nothing to traffic that stays encrypted.
type Tier string

const (
	// TierConnection is a fault on the connection itself.
	TierConnection Tier = "connection"
	// TierResponse is a fault on a request or its response.
	TierResponse Tier = "response"
)

// DialFunc opens the upstream side of a connection.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Fault is one kind of fault in the catalogue: its name on the wire, how much
// of the traffic it needs to see, the parameters it accepts, and how to
// configure one from them.
type Fault interface {
	Name() string
	Tier() Tier
	Schema() Schema
	// New returns the fault configured by params, which Validate has accepted.
	New(params rules.Params) (Applier, error)
}

// Applier is a configured fault acting on a request in flight. It is middleware
// over the real round trip: next performs it, and a fault that answers on its
// own never calls next. Every fault can act here, whatever its tier, because a
// request Faultline can see is also a connection it can interfere with.
type Applier interface {
	Respond(ruleID string, req *http.Request, next http.RoundTripper) (*http.Response, error)
}

// Tunneler is a configured connection-tier fault acting on a connection before
// any request exists, which is all a CONNECT tunnel offers. next dials the
// upstream, and a fault that turns the connection away never calls it. A fault
// implements this exactly when it declares TierConnection.
type Tunneler interface {
	Applier
	Dial(ctx context.Context, ruleID, addr string, next DialFunc) (net.Conn, error)
}

// catalogue holds every registered fault by name. It is written only by
// Register, from the package initialisers of the faults themselves, so it needs
// no lock: nothing reads it before initialisation is over.
var catalogue = map[string]Fault{}

// Register adds a fault to the catalogue. It panics on a nameless or duplicate
// fault, which is a mistake in Faultline itself rather than in anyone's input.
func Register(f Fault) {
	name := f.Name()
	if name == "" {
		panic("faults: a fault needs a name")
	}
	if _, taken := catalogue[name]; taken {
		panic(fmt.Sprintf("faults: fault %q is registered twice", name))
	}
	catalogue[name] = f
}

// Get looks a fault up by the name it goes by on the wire.
func Get(name string) (Fault, bool) {
	f, ok := catalogue[name]
	return f, ok
}

// All is the catalogue, sorted by name.
func All() []Fault {
	out := make([]Fault, 0, len(catalogue))
	for _, f := range catalogue {
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b Fault) int { return cmp.Compare(a.Name(), b.Name()) })
	return out
}

// Names lists the registered fault names, sorted, for an error that has to say
// what is on offer.
func Names() []string {
	out := make([]string, 0, len(catalogue))
	for name := range catalogue {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// Validate checks a fault from outside: a type that names a registered fault,
// and parameters that fault accepts. The error is a *ParamError naming the
// field, so the API can report which one to fix.
func Validate(f rules.Fault) error {
	if f.Type == "" {
		return paramErr("type", "fault.type is required, use %s", listNames())
	}
	def, ok := Get(string(f.Type))
	if !ok {
		return paramErr("type", "unknown fault type %q, use %s", f.Type, listNames())
	}
	return def.Schema().Validate(f.Params)
}

// Build configures the fault a rule asks for. An unknown type or parameters the
// fault will not take are errors: the pipelines treat them as no fault at all,
// because a rule in that state can only have come from outside the API, which
// validates first.
func Build(f rules.Fault) (Applier, error) {
	def, ok := Get(string(f.Type))
	if !ok {
		return nil, fmt.Errorf("faults: unknown fault type %q", f.Type)
	}
	if err := def.Schema().Validate(f.Params); err != nil {
		return nil, fmt.Errorf("faults: fault %q: %w", f.Type, err)
	}
	return def.New(f.Params)
}

// listNames renders the registered names for an error message.
func listNames() string {
	names := Names()
	if len(names) == 0 {
		return "no fault type"
	}
	out := ""
	for i, n := range names {
		switch {
		case i == 0:
		case i == len(names)-1:
			out += " or "
		default:
			out += ", "
		}
		out += fmt.Sprintf("%q", n)
	}
	return out
}
