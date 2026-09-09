package faults

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

var behaviourCases = map[string]struct {
	good []rules.Params
	bad  []badParams
}{
	"percent": {
		good: []rules.Params{{"percent": 1}, {"percent": 100}, {"percent": float64(30)}},
		bad: []badParams{
			{"no share", rules.Params{}, "percent"},
			{"a share of zero", rules.Params{"percent": 0}, "percent"},
			{"a share above everything", rules.Params{"percent": 101}, "percent"},
			{"a fractional share", rules.Params{"percent": 1.5}, "percent"},
			{"a share as text", rules.Params{"percent": "half"}, "percent"},
			{"a count", rules.Params{"percent": 50, "n": 2}, "n"},
		},
	},
	"first_n": {
		good: []rules.Params{{"n": 1}, {"n": 20}, {"n": float64(2)}},
		bad: []badParams{
			{"no count", rules.Params{}, "n"},
			{"a count of zero", rules.Params{"n": 0}, "n"},
			{"a negative count", rules.Params{"n": -1}, "n"},
			{"a fractional count", rules.Params{"n": 1.5}, "n"},
			{"a count as text", rules.Params{"n": "two"}, "n"},
			{"a share", rules.Params{"n": 2, "percent": 50}, "percent"},
		},
	},
	"for_duration": {
		good: []rules.Params{{"sec": 1}, {"sec": 30}, {"sec": float64(30)}},
		bad: []badParams{
			{"no duration", rules.Params{}, "sec"},
			{"a duration of zero", rules.Params{"sec": 0}, "sec"},
			{"a negative duration", rules.Params{"sec": -1}, "sec"},
			{"a fractional duration", rules.Params{"sec": 1.5}, "sec"},
			{"a duration as text", rules.Params{"sec": "a while"}, "sec"},
			{"milliseconds", rules.Params{"ms": 1000}, "ms"},
		},
	},
	"pattern": {
		good: []rules.Params{{"pattern": "F"}, {"pattern": "FFP"}, {"pattern": "fp"}},
		bad: []badParams{
			{"no pattern", rules.Params{}, "pattern"},
			{"an empty pattern", rules.Params{"pattern": ""}, "pattern"},
			{"a step that is neither", rules.Params{"pattern": "FFX"}, "pattern"},
			{"a pattern that is not text", rules.Params{"pattern": 2}, "pattern"},
			{"a count", rules.Params{"pattern": "FP", "n": 2}, "n"},
		},
	},
}

func TestEveryBehaviorIsCovered(t *testing.T) {
	for _, b := range AllBehaviors() {
		if _, ok := behaviourCases[b.Name()]; !ok {
			t.Errorf("behavior %q is registered but has no entry in behaviourCases", b.Name())
		}
	}
	for name := range behaviourCases {
		if _, ok := GetBehavior(name); !ok {
			t.Errorf("behaviourCases covers %q, which is not registered", name)
		}
	}
}

func TestBehaviorsAcceptTheirGoodParams(t *testing.T) {
	for name, tc := range behaviourCases {
		t.Run(name, func(t *testing.T) {
			for _, params := range tc.good {
				behavior := rules.Behavior{Type: rules.BehaviorType(name), Params: params}
				if err := ValidateBehavior(behavior); err != nil {
					t.Errorf("ValidateBehavior(%v): %v", params, err)
					continue
				}
				if _, err := BuildBehavior(behavior); err != nil {
					t.Errorf("BuildBehavior(%v): %v", params, err)
				}
			}
		})
	}
}

func TestBehaviorsRejectTheirBadParamsNamingTheField(t *testing.T) {
	for name, tc := range behaviourCases {
		for _, bad := range tc.bad {
			t.Run(name+"/"+bad.name, func(t *testing.T) {
				behavior := rules.Behavior{Type: rules.BehaviorType(name), Params: bad.params}

				err := ValidateBehavior(behavior)
				var pe *ParamError
				if !errors.As(err, &pe) {
					t.Fatalf("ValidateBehavior = %v, want a *ParamError", err)
				}
				if pe.Field != bad.field {
					t.Errorf("field = %q, want %q", pe.Field, bad.field)
				}
				if got, want := pe.Path(), "behavior."+bad.field; got != want {
					t.Errorf("path = %q, want %q", got, want)
				}
				if _, err := BuildBehavior(behavior); err == nil {
					t.Error("BuildBehavior accepted parameters ValidateBehavior rejected")
				}
			})
		}
	}
}

func TestBehaviorSchemaMatchesTheConfigStruct(t *testing.T) {
	for name, tc := range behaviourCases {
		t.Run(name, func(t *testing.T) {
			behavior, ok := GetBehavior(name)
			if !ok {
				t.Fatalf("behavior %q is not registered", name)
			}
			decider, err := BuildBehavior(rules.Behavior{Type: rules.BehaviorType(name), Params: tc.good[0]})
			if err != nil {
				t.Fatalf("BuildBehavior: %v", err)
			}

			got := jsonTagsOf(reflect.TypeOf(decider).Elem())
			want := behavior.Schema().Names()
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("decider struct tags %v, schema fields %v", got, want)
			}
		})
	}
}

func TestValidateBehaviorRejectsAnEmptyType(t *testing.T) {
	err := ValidateBehavior(rules.Behavior{})

	var pe *ParamError
	if !errors.As(err, &pe) || pe.Field != "type" {
		t.Fatalf("err = %v, want a *ParamError on the type field", err)
	}
	if pe.Path() != "behavior.type" {
		t.Errorf("path = %q, want %q", pe.Path(), "behavior.type")
	}
	for _, name := range BehaviorNames() {
		if !strings.Contains(pe.Message, name) {
			t.Errorf("message %q does not offer %q", pe.Message, name)
		}
	}
}

func TestValidateBehaviorRejectsAnUnknownType(t *testing.T) {
	err := ValidateBehavior(rules.Behavior{Type: "sometimes"})

	var pe *ParamError
	if !errors.As(err, &pe) || pe.Field != "type" {
		t.Fatalf("err = %v, want a *ParamError on the type field", err)
	}
}

func TestBuildBehaviorRejectsAnUnknownType(t *testing.T) {
	if _, err := BuildBehavior(rules.Behavior{Type: "sometimes"}); err == nil {
		t.Error("BuildBehavior accepted an unregistered behavior type")
	}
}

func TestRegisterBehaviorRejectsADuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a behavior twice should panic")
		}
	}()
	RegisterBehavior(firstN{})
}

func TestFirstNAppliesToTheFirstRequestsOnly(t *testing.T) {
	decider := mustDecider(t, "first_n", rules.Params{"n": 2})

	if got := decisions(decider, 4); got != "FFPP" {
		t.Errorf("first_n 2 gave %q, want %q", got, "FFPP")
	}
}

func TestPatternRepeats(t *testing.T) {
	decider := mustDecider(t, "pattern", rules.Params{"pattern": "FFP"})

	if got := decisions(decider, 7); got != "FFPFFPF" {
		t.Errorf("pattern FFP gave %q, want %q", got, "FFPFFPF")
	}
}

func TestPatternIgnoresCase(t *testing.T) {
	decider := mustDecider(t, "pattern", rules.Params{"pattern": "fp"})

	if got := decisions(decider, 4); got != "FPFP" {
		t.Errorf("pattern fp gave %q, want %q", got, "FPFP")
	}
}

func TestForDurationStopsWhenTheWindowRunsOut(t *testing.T) {
	now := time.Now()
	decider := &forDurationDecider{Sec: 2, now: func() time.Time { return now }}

	if !decider.Applies() {
		t.Fatal("the first request did not open the window")
	}
	now = now.Add(1990 * time.Millisecond)
	if !decider.Applies() {
		t.Error("a request inside the window did not apply")
	}
	now = now.Add(20 * time.Millisecond)
	if decider.Applies() {
		t.Error("a request past the window still applied")
	}
	now = now.Add(time.Hour)
	if decider.Applies() {
		t.Error("the window reopened")
	}
}

func TestForDurationStartsAtTheFirstMatchingRequest(t *testing.T) {
	now := time.Now()
	decider := &forDurationDecider{Sec: 1, now: func() time.Time { return now }}

	now = now.Add(time.Hour) // the rule sat unused for an hour
	if !decider.Applies() {
		t.Error("the window had run out before any request matched")
	}
}

func TestPercentAppliesToEverythingAtAHundred(t *testing.T) {
	decider := mustDecider(t, "percent", rules.Params{"percent": 100})

	for i := range 200 {
		if !decider.Applies() {
			t.Fatalf("request %d did not apply at 100 percent", i)
		}
	}
}

// TestPercentAppliesToRoughlyItsShare uses bounds wide enough that a passing
// build is not a matter of luck: at 50 percent over 2000 requests, landing
// outside 40 to 60 percent is far beyond anything randomness produces.
func TestPercentAppliesToRoughlyItsShare(t *testing.T) {
	decider := mustDecider(t, "percent", rules.Params{"percent": 50})

	applied := 0
	for range 2000 {
		if decider.Applies() {
			applied++
		}
	}
	if applied < 800 || applied > 1200 {
		t.Errorf("50 percent applied to %d of 2000 requests", applied)
	}
}

func mustDecider(t *testing.T, name string, params rules.Params) Decider {
	t.Helper()
	decider, err := BuildBehavior(rules.Behavior{Type: rules.BehaviorType(name), Params: params})
	if err != nil {
		t.Fatalf("BuildBehavior(%s, %v): %v", name, params, err)
	}
	return decider
}

// decisions runs a decider n times and writes what it decided as F for a fault
// applied and P for one passed through, so a whole sequence reads at a glance.
func decisions(d Decider, n int) string {
	var b strings.Builder
	for range n {
		if d.Applies() {
			b.WriteByte('F')
		} else {
			b.WriteByte('P')
		}
	}
	return b.String()
}
