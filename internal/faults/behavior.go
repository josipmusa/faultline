package faults

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

// Behavior is one kind of behavior in the catalogue: its name on the wire, the
// parameters it accepts, and how to make a decider from them. A behavior does
// nothing to traffic itself; it only says whether the rule's fault applies to a
// request the rule already matched.
type Behavior interface {
	Name() string
	Schema() Schema
	// New returns the decider configured by params, which ValidateBehavior has
	// accepted.
	New(params rules.Params) (Decider, error)
}

// Decider answers, for one request a rule matched, whether the rule's fault
// applies to it. Deciders carry the state that makes a behavior stateful and
// are called one request at a time by the Gate that holds them, so they need no
// locking of their own.
type Decider interface {
	Applies() bool
}

var behaviours = map[string]Behavior{}

// RegisterBehavior adds a behavior to the catalogue. Behaviors register
// themselves from an init function, so importing this package is enough.
func RegisterBehavior(b Behavior) {
	name := b.Name()
	if name == "" {
		panic("faults: a behavior needs a name")
	}
	if _, taken := behaviours[name]; taken {
		panic(fmt.Sprintf("faults: behavior %q is registered twice", name))
	}
	behaviours[name] = b
}

// GetBehavior looks a behavior up by the name a rule uses.
func GetBehavior(name string) (Behavior, bool) {
	b, ok := behaviours[name]
	return b, ok
}

// AllBehaviors returns the catalogue in name order.
func AllBehaviors() []Behavior {
	out := make([]Behavior, 0, len(behaviours))
	for _, b := range behaviours {
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b Behavior) int { return cmp.Compare(a.Name(), b.Name()) })
	return out
}

// BehaviorNames returns the registered names in order.
func BehaviorNames() []string {
	out := make([]string, 0, len(behaviours))
	for name := range behaviours {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// ValidateBehavior checks a behavior from outside: a type that names a
// registered behavior, and parameters it accepts. The error is a *ParamError
// scoped to the behavior, so the API can report behavior.n rather than fault.n.
func ValidateBehavior(b rules.Behavior) error {
	if b.Type == "" {
		return scopedErr(scopeBehavior, "type", "behavior.type is required, use %s", listBehaviorNames())
	}
	def, ok := GetBehavior(string(b.Type))
	if !ok {
		return scopedErr(scopeBehavior, "type", "unknown behavior type %q, use %s", b.Type, listBehaviorNames())
	}
	return def.Schema().validate(scopeBehavior, b.Params)
}

// BuildBehavior configures the decider a rule asks for. As with Build, an
// unknown type or parameters the behavior will not take are errors: a rule in
// that state can only have come from outside the API, which validates first.
func BuildBehavior(b rules.Behavior) (Decider, error) {
	def, ok := GetBehavior(string(b.Type))
	if !ok {
		return nil, fmt.Errorf("faults: unknown behavior type %q", b.Type)
	}
	if err := def.Schema().validate(scopeBehavior, b.Params); err != nil {
		return nil, fmt.Errorf("faults: behavior %q: %w", b.Type, err)
	}
	return def.New(b.Params)
}

// listBehaviorNames renders the registered behavior names for an error message.
func listBehaviorNames() string {
	names := BehaviorNames()
	if len(names) == 0 {
		return "no behavior type"
	}
	return listOf(quotedAll(names))
}

// percent applies the fault to a share of the matching requests, chosen
// independently each time. It is the flaky dependency: no pattern to lock onto,
// which is the point.
type percent struct{}

func init() { RegisterBehavior(percent{}) }

func (percent) Name() string { return "percent" }

func (percent) Schema() Schema {
	return Schema{
		Int("percent").Required().Min(1).Max(100).Desc("What share of matching requests the fault applies to, as a percentage"),
	}
}

func (percent) New(params rules.Params) (Decider, error) {
	var d percentDecider
	if err := Decode(params, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

type percentDecider struct {
	Percent int `json:"percent"`
}

func (d *percentDecider) Applies() bool {
	return rand.IntN(100) < d.Percent //nolint:gosec // a share of requests wants to be arbitrary, not unguessable
}

// firstN applies the fault to the first few matching requests and then gets out
// of the way, which is how a retry that eventually succeeds is rehearsed.
type firstN struct{}

func init() { RegisterBehavior(firstN{}) }

func (firstN) Name() string { return "first_n" }

func (firstN) Schema() Schema {
	return Schema{
		Int("n").Required().Min(1).Desc("How many matching requests the fault applies to before the rule lets the rest through"),
	}
}

func (firstN) New(params rules.Params) (Decider, error) {
	var d firstNDecider
	if err := Decode(params, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

type firstNDecider struct {
	N    int `json:"n"`
	seen int
}

func (d *firstNDecider) Applies() bool {
	if d.seen >= d.N {
		return false
	}
	d.seen++
	return true
}

// forDuration applies the fault for a window and then recovers, which is how an
// outage that ends on its own is rehearsed. The window opens at the first
// request the rule matches rather than when the rule was written: an outage
// nobody has driven traffic through yet has not started.
type forDuration struct{}

func init() { RegisterBehavior(forDuration{}) }

func (forDuration) Name() string { return "for_duration" }

func (forDuration) Schema() Schema {
	return Schema{
		Int("sec").Required().Min(1).Desc("How many seconds after the rule is switched on the fault keeps applying"),
	}
}

func (forDuration) New(params rules.Params) (Decider, error) {
	d := forDurationDecider{now: time.Now}
	if err := Decode(params, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

type forDurationDecider struct {
	Sec int `json:"sec"`

	now   func() time.Time
	start time.Time
}

func (d *forDurationDecider) Applies() bool {
	now := d.now()
	if d.start.IsZero() {
		d.start = now
	}
	return now.Sub(d.start) < time.Duration(d.Sec)*time.Second
}

// pattern applies the fault by a repeating script of steps, F for a request
// that gets the fault and P for one that passes, so a sequence a test can
// predict exactly can be written down.
type pattern struct{}

func init() { RegisterBehavior(pattern{}) }

func (pattern) Name() string { return "pattern" }

// patternSteps are the characters a pattern is written with.
const patternSteps = "FP"

func (pattern) Schema() Schema {
	return Schema{
		Str("pattern").Required().Chars(patternSteps).Desc("The cycle to repeat, one letter per matching request: F applies the fault, P lets it through"),
	}
}

func (pattern) New(params rules.Params) (Decider, error) {
	var d patternDecider
	if err := Decode(params, &d); err != nil {
		return nil, err
	}
	d.steps = strings.ToUpper(d.Pattern)
	return &d, nil
}

type patternDecider struct {
	Pattern string `json:"pattern"`

	steps string
	at    int
}

func (d *patternDecider) Applies() bool {
	if d.steps == "" {
		return true // a pattern with no steps cannot say no; the schema rules it out
	}
	step := d.steps[d.at]
	d.at = (d.at + 1) % len(d.steps)
	return step == 'F'
}
