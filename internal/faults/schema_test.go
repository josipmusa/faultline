package faults

import (
	"errors"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

func TestSchemaValidateAcceptsGoodParams(t *testing.T) {
	s := Schema{Int("ms").Required().Min(1), Int("jitter_ms").Min(0), Str("body")}

	if err := s.Validate(rules.Params{"ms": 100, "jitter_ms": 0, "body": "gone"}); err != nil {
		t.Errorf("Validate: %v", err)
	}
	// An optional field may simply be absent.
	if err := s.Validate(rules.Params{"ms": 1}); err != nil {
		t.Errorf("Validate without the optional fields: %v", err)
	}
	// JSON delivers every number as a float64; YAML delivers whole ones as int.
	if err := s.Validate(rules.Params{"ms": float64(100)}); err != nil {
		t.Errorf("Validate with a float64: %v", err)
	}
}

func TestSchemaValidateNamesTheBadField(t *testing.T) {
	s := Schema{Int("ms").Required().Min(1).Max(600), Str("body")}

	tests := []struct {
		name   string
		params rules.Params
		field  string
	}{
		{"missing required", rules.Params{}, "ms"},
		{"below minimum", rules.Params{"ms": 0}, "ms"},
		{"above maximum", rules.Params{"ms": 601}, "ms"},
		{"not a number", rules.Params{"ms": "soon"}, "ms"},
		{"not a whole number", rules.Params{"ms": 1.5}, "ms"},
		{"not a string", rules.Params{"ms": 1, "body": 3}, "body"},
		{"unknown field", rules.Params{"ms": 1, "nonsense": true}, "nonsense"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Validate(tt.params)
			var pe *ParamError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a *ParamError", err)
			}
			if pe.Field != tt.field {
				t.Errorf("field = %q, want %q", pe.Field, tt.field)
			}
			if pe.Message == "" {
				t.Error("a ParamError needs a message a user can act on")
			}
		})
	}
}

func TestDecodeFillsTypedConfig(t *testing.T) {
	type config struct {
		MS       int    `json:"ms"`
		JitterMS int    `json:"jitter_ms"`
		Body     string `json:"body"`
	}

	var c config
	if err := Decode(rules.Params{"ms": float64(2000), "jitter_ms": 500, "body": "gone"}, &c); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if c.MS != 2000 || c.JitterMS != 500 || c.Body != "gone" {
		t.Errorf("config = %+v, want the parameters", c)
	}
}

func TestDecodeLeavesAbsentFieldsZero(t *testing.T) {
	type config struct {
		MS       int `json:"ms"`
		JitterMS int `json:"jitter_ms"`
	}

	var c config
	if err := Decode(rules.Params{"ms": 10}, &c); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if c.JitterMS != 0 {
		t.Errorf("jitter_ms = %d, want the zero value when the parameter is absent", c.JitterMS)
	}
}

func TestSchemaNames(t *testing.T) {
	s := Schema{Int("ms").Required(), Int("jitter_ms")}

	got := s.Names()
	if len(got) != 2 || got[0] != "ms" || got[1] != "jitter_ms" {
		t.Errorf("Names() = %v, want the fields in declaration order", got)
	}
}

func TestSchemaValidateAcceptsMapsAndLists(t *testing.T) {
	s := Schema{StrMap("set"), StrList("remove")}

	tests := []struct {
		name   string
		params rules.Params
	}{
		{"decoded from JSON", rules.Params{
			"set":    map[string]any{"X-Test": "1"},
			"remove": []any{"ETag"},
		}},
		{"written in Go", rules.Params{
			"set":    map[string]string{"X-Test": "1"},
			"remove": []string{"ETag"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.Validate(tt.params); err != nil {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}

func TestSchemaValidateNamesTheBadMapOrList(t *testing.T) {
	s := Schema{StrMap("set"), StrList("remove")}

	tests := []struct {
		name   string
		params rules.Params
		field  string
	}{
		{"map that is not a map", rules.Params{"set": "X-Test: 1"}, "set"},
		{"map with a non-string value", rules.Params{"set": map[string]any{"X-Test": 1}}, "set"},
		{"map with an empty name", rules.Params{"set": map[string]any{"": "1"}}, "set"},
		{"empty map", rules.Params{"set": map[string]any{}}, "set"},
		{"list that is not a list", rules.Params{"remove": "ETag"}, "remove"},
		{"list with a non-string entry", rules.Params{"remove": []any{1}}, "remove"},
		{"list with an empty entry", rules.Params{"remove": []any{""}}, "remove"},
		{"empty list", rules.Params{"remove": []any{}}, "remove"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Validate(tt.params)
			var pe *ParamError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a *ParamError", err)
			}
			if pe.Field != tt.field {
				t.Errorf("field = %q, want %q", pe.Field, tt.field)
			}
		})
	}
}

func TestSchemaOrWantsAtLeastOneOfThePair(t *testing.T) {
	s := Schema{Int("a").Or("b"), Int("b")}

	for _, params := range []rules.Params{{"a": 1}, {"b": 2}, {"a": 1, "b": 2}} {
		if err := s.Validate(params); err != nil {
			t.Errorf("Validate(%v): %v", params, err)
		}
	}

	err := s.Validate(rules.Params{})
	var pe *ParamError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a *ParamError", err)
	}
	if pe.Field != "a" {
		t.Errorf("field = %q, want %q", pe.Field, "a")
	}
	if !strings.Contains(pe.Message, "b") {
		t.Errorf("message %q does not mention the alternative", pe.Message)
	}
}

func TestSchemaXorWantsExactlyOneOfThePair(t *testing.T) {
	s := Schema{Int("a").Xor("b"), Int("b")}

	for _, params := range []rules.Params{{"a": 1}, {"b": 2}} {
		if err := s.Validate(params); err != nil {
			t.Errorf("Validate(%v): %v", params, err)
		}
	}

	for _, params := range []rules.Params{{}, {"a": 1, "b": 2}} {
		err := s.Validate(params)
		var pe *ParamError
		if !errors.As(err, &pe) {
			t.Fatalf("Validate(%v) = %v, want a *ParamError", params, err)
		}
		if pe.Field != "a" {
			t.Errorf("field = %q, want %q", pe.Field, "a")
		}
	}
}
