package faults

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

// badParams is one set of parameters a fault must reject, and the field it must
// name when it does.
type badParams struct {
	name   string
	params rules.Params
	field  string
}

// catalogueCases is the table every registered fault is driven through. A fault
// registered without an entry here fails TestEveryFaultIsCovered, so adding a
// fault means saying what it accepts and what it refuses.
var catalogueCases = map[string]struct {
	tier Tier
	good []rules.Params
	bad  []badParams
}{
	"delay": {
		tier: TierConnection,
		good: []rules.Params{
			{"ms": 1},
			{"ms": 2000, "jitter_ms": 500},
			{"ms": float64(2000)},
		},
		bad: []badParams{
			{"no ms", rules.Params{}, "ms"},
			{"ms of zero", rules.Params{"ms": 0}, "ms"},
			{"negative ms", rules.Params{"ms": -1}, "ms"},
			{"fractional ms", rules.Params{"ms": 1.5}, "ms"},
			{"ms as text", rules.Params{"ms": "soon"}, "ms"},
			{"negative jitter", rules.Params{"ms": 10, "jitter_ms": -1}, "jitter_ms"},
			{"a status code", rules.Params{"ms": 10, "code": 503}, "code"},
		},
	},
	"status": {
		tier: TierResponse,
		good: []rules.Params{
			{"code": 503},
			{"code": 429, "body": "slow down"},
			{"code": 100},
			{"code": 599},
		},
		bad: []badParams{
			{"no code", rules.Params{}, "code"},
			{"code below the range", rules.Params{"code": 99}, "code"},
			{"code above the range", rules.Params{"code": 600}, "code"},
			{"body that is not text", rules.Params{"code": 503, "body": 1}, "body"},
			{"a delay", rules.Params{"code": 503, "ms": 10}, "ms"},
		},
	},
	"reset": {
		tier: TierConnection,
		good: []rules.Params{nil, {}, {"after_bytes": 0}, {"after_bytes": 4096}},
		bad: []badParams{
			{"negative bytes", rules.Params{"after_bytes": -1}, "after_bytes"},
			{"fractional bytes", rules.Params{"after_bytes": 1.5}, "after_bytes"},
			{"bytes as text", rules.Params{"after_bytes": "some"}, "after_bytes"},
			{"a status code", rules.Params{"code": 503}, "code"},
		},
	},
	"hang": {
		tier: TierConnection,
		good: []rules.Params{nil, {}, {"max_ms": 1}, {"max_ms": float64(30000)}},
		bad: []badParams{
			{"a cap of zero", rules.Params{"max_ms": 0}, "max_ms"},
			{"a negative cap", rules.Params{"max_ms": -1}, "max_ms"},
			{"a cap as text", rules.Params{"max_ms": "forever"}, "max_ms"},
			{"a delay", rules.Params{"ms": 10}, "ms"},
		},
	},
	"throttle": {
		tier: TierConnection,
		good: []rules.Params{{"bytes_per_sec": 1}, {"bytes_per_sec": 1024}, {"bytes_per_sec": float64(1024)}},
		bad: []badParams{
			{"no rate", rules.Params{}, "bytes_per_sec"},
			{"a rate of zero", rules.Params{"bytes_per_sec": 0}, "bytes_per_sec"},
			{"a negative rate", rules.Params{"bytes_per_sec": -1}, "bytes_per_sec"},
			{"a fractional rate", rules.Params{"bytes_per_sec": 1.5}, "bytes_per_sec"},
			{"a status code", rules.Params{"bytes_per_sec": 1024, "code": 503}, "code"},
		},
	},
	"refuse": {
		tier: TierConnection,
		good: []rules.Params{nil, {}},
		bad: []badParams{
			{"a delay", rules.Params{"ms": 10}, "ms"},
			{"jitter", rules.Params{"jitter_ms": 10}, "jitter_ms"},
			{"a status code", rules.Params{"code": 503}, "code"},
			{"a body", rules.Params{"body": "nope"}, "body"},
		},
	},
}

func TestEveryFaultIsCovered(t *testing.T) {
	for _, f := range All() {
		if _, ok := catalogueCases[f.Name()]; !ok {
			t.Errorf("fault %q is registered but has no entry in catalogueCases", f.Name())
		}
	}
	for name := range catalogueCases {
		if _, ok := Get(name); !ok {
			t.Errorf("catalogueCases covers %q, which is not registered", name)
		}
	}
}

func TestFaultsAcceptTheirGoodParams(t *testing.T) {
	for name, tc := range catalogueCases {
		t.Run(name, func(t *testing.T) {
			for _, params := range tc.good {
				fault := rules.Fault{Type: rules.FaultType(name), Params: params}
				if err := Validate(fault); err != nil {
					t.Errorf("Validate(%v): %v", params, err)
					continue
				}
				if _, err := Build(fault); err != nil {
					t.Errorf("Build(%v): %v", params, err)
				}
			}
		})
	}
}

func TestFaultsRejectTheirBadParamsNamingTheField(t *testing.T) {
	for name, tc := range catalogueCases {
		for _, bad := range tc.bad {
			t.Run(name+"/"+bad.name, func(t *testing.T) {
				fault := rules.Fault{Type: rules.FaultType(name), Params: bad.params}

				err := Validate(fault)
				var pe *ParamError
				if !errors.As(err, &pe) {
					t.Fatalf("Validate = %v, want a *ParamError", err)
				}
				if pe.Field != bad.field {
					t.Errorf("field = %q, want %q", pe.Field, bad.field)
				}
				if _, err := Build(fault); err == nil {
					t.Error("Build accepted parameters Validate rejected")
				}
			})
		}
	}
}

func TestDeclaredTierMatchesWhatTheFaultCanDo(t *testing.T) {
	for name, tc := range catalogueCases {
		t.Run(name, func(t *testing.T) {
			fault, ok := Get(name)
			if !ok {
				t.Fatalf("fault %q is not registered", name)
			}
			if fault.Tier() != tc.tier {
				t.Errorf("tier = %q, want %q", fault.Tier(), tc.tier)
			}

			applier, err := Build(rules.Fault{Type: rules.FaultType(name), Params: tc.good[0]})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			// A connection-tier fault must be able to act on a bare connection,
			// and a response-tier fault must not claim it can.
			_, tunnels := applier.(Tunneler)
			if want := fault.Tier() == TierConnection; tunnels != want {
				t.Errorf("implements Tunneler = %v, want %v for tier %q", tunnels, want, fault.Tier())
			}
		})
	}
}

func TestSchemaMatchesTheConfigStruct(t *testing.T) {
	for name, tc := range catalogueCases {
		t.Run(name, func(t *testing.T) {
			fault, ok := Get(name)
			if !ok {
				t.Fatalf("fault %q is not registered", name)
			}
			applier, err := Build(rules.Fault{Type: rules.FaultType(name), Params: tc.good[0]})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			// The schema and the configured fault's JSON tags are two lists of
			// the same parameters. Drifting apart means a parameter that
			// validates and is then ignored, or the reverse.
			got := jsonTagsOf(reflect.TypeOf(applier))
			want := fault.Schema().Names()
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("config struct tags %v, schema fields %v", got, want)
			}
		})
	}
}

func jsonTagsOf(t reflect.Type) []string {
	out := []string{}
	for i := range t.NumField() {
		if tag := t.Field(i).Tag.Get("json"); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

func TestValidateRejectsAnEmptyType(t *testing.T) {
	err := Validate(rules.Fault{})

	var pe *ParamError
	if !errors.As(err, &pe) || pe.Field != "type" {
		t.Fatalf("err = %v, want a *ParamError on the type field", err)
	}
	for _, name := range Names() {
		if !strings.Contains(pe.Message, name) {
			t.Errorf("message %q does not offer %q", pe.Message, name)
		}
	}
}

func TestValidateRejectsAnUnknownType(t *testing.T) {
	err := Validate(rules.Fault{Type: "teleport"})

	var pe *ParamError
	if !errors.As(err, &pe) || pe.Field != "type" {
		t.Fatalf("err = %v, want a *ParamError on the type field", err)
	}
}

func TestBuildRejectsAnUnknownType(t *testing.T) {
	if _, err := Build(rules.Fault{Type: "teleport"}); err == nil {
		t.Error("Build accepted an unregistered fault type")
	}
}

func TestRegisterRejectsADuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a fault twice should panic")
		}
	}()
	Register(delay{})
}
