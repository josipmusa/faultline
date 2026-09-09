package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodFile = `# yaml-language-server: $schema=../../schema/faultline.schema.json
routes:
  - name: stripe
    upstream: https://api.stripe.com
    port: 9100
  - name: orders
    upstream: http://orders.internal:8080

bypass:
  - httpbin.org
  - "*.internal"

rules:
  - id: slow-stripe
    name: Stripe is slow
    match:
      host: api.stripe.com
      method: POST
      path: /v1/charges/*
      header:
        X-Test: "1"
    fault:
      type: delay
      ms: 2000
      jitter_ms: 500
    behavior:
      type: first_n
      n: 2

scenarios:
  - name: payments-down
    rules:
      - slow-stripe
      - id: stripe-503
        name: Stripe is down
        fault:
          type: status
          code: 503
`

func TestParseReadsAWholeFile(t *testing.T) {
	cfg, err := Parse("faultline.yaml", []byte(goodFile))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(cfg.Routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(cfg.Routes))
	}
	if got := cfg.Routes[0]; got.Name != "stripe" || got.Port != 9100 || got.Upstream.String() != "https://api.stripe.com" {
		t.Errorf("routes[0] = %+v", got)
	}
	if cfg.Routes[1].Port != 0 {
		t.Errorf("routes[1].Port = %d, want 0 so the caller can assign one", cfg.Routes[1].Port)
	}

	if want := []string{"httpbin.org", "*.internal"}; len(cfg.Bypass) != 2 || cfg.Bypass[0] != want[0] || cfg.Bypass[1] != want[1] {
		t.Errorf("bypass = %v, want %v", cfg.Bypass, want)
	}

	if len(cfg.Rules) != 2 {
		t.Fatalf("rules = %d, want the top level rule and the inline one", len(cfg.Rules))
	}
	r := cfg.Rules[0]
	if r.ID != "slow-stripe" || r.Name != "Stripe is slow" || !r.Enabled {
		t.Errorf("rules[0] = %+v, want an enabled slow-stripe", r)
	}
	if r.Match.Host != "api.stripe.com" || r.Match.Method != "POST" || r.Match.Path != "/v1/charges/*" || r.Match.Header["X-Test"] != "1" {
		t.Errorf("rules[0].Match = %+v", r.Match)
	}
	if r.Fault.Type != "delay" || r.Fault.Params["ms"] != 2000 || r.Fault.Params["jitter_ms"] != 500 {
		t.Errorf("rules[0].Fault = %+v", r.Fault)
	}
	if r.Behavior == nil || r.Behavior.Type != "first_n" || r.Behavior.Params["n"] != 2 {
		t.Errorf("rules[0].Behavior = %+v", r.Behavior)
	}

	// A rule written inside a scenario is a rule like any other, but it stays
	// off until the scenario is activated.
	inline := cfg.Rules[1]
	if inline.ID != "stripe-503" || inline.Enabled {
		t.Errorf("rules[1] = %+v, want a disabled stripe-503", inline)
	}

	if len(cfg.Scenarios) != 1 {
		t.Fatalf("scenarios = %d, want 1", len(cfg.Scenarios))
	}
	s := cfg.Scenarios[0]
	if s.Name != "payments-down" {
		t.Errorf("scenario name = %q", s.Name)
	}
	if want := []string{"slow-stripe", "stripe-503"}; strings.Join(s.Rules, ",") != strings.Join(want, ",") {
		t.Errorf("scenario rules = %v, want %v", s.Rules, want)
	}
}

func TestParseAcceptsAnEmptyFile(t *testing.T) {
	for _, in := range []string{"", "# nothing but a comment\n", "{}\n"} {
		cfg, err := Parse("faultline.yaml", []byte(in))
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if len(cfg.Rules) != 0 || len(cfg.Routes) != 0 || len(cfg.Scenarios) != 0 || len(cfg.Bypass) != 0 {
			t.Errorf("Parse(%q) = %+v, want an empty config", in, cfg)
		}
	}
}

// numbered turns a body into a file whose lines are easy to count in a
// failure: line 1 is the first line of body.
func numbered(body string) []byte { return []byte(strings.TrimPrefix(body, "\n")) }

func TestParseNamesTheFileTheLineAndTheField(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		line int
		path string
		want string // a fragment of the message
	}{
		{
			name: "unknown top level key",
			yaml: `
rules: []
scenraios: []
`,
			line: 2,
			path: "scenraios",
			want: "unknown",
		},
		{
			name: "unknown rule key",
			yaml: `
rules:
  - id: a
    name: A
    disabled: true
    fault:
      type: refuse
`,
			line: 4,
			path: "rules[0].disabled",
			want: "unknown",
		},
		{
			name: "unknown fault parameter",
			yaml: `
rules:
  - id: a
    name: A
    fault:
      type: delay
      ms: 10
      seconds: 3
`,
			line: 7,
			path: "rules[0].fault.seconds",
			want: "unknown parameter",
		},
		{
			name: "bad fault parameter",
			yaml: `
rules:
  - id: a
    name: A
    fault:
      type: delay
      ms: 0
`,
			line: 6,
			path: "rules[0].fault.ms",
			want: "1 or more",
		},
		{
			name: "missing fault parameter",
			yaml: `
rules:
  - id: a
    name: A
    fault:
      type: delay
`,
			line: 5,
			path: "rules[0].fault.ms",
			want: "required",
		},
		{
			name: "unknown fault type",
			yaml: `
rules:
  - id: a
    name: A
    fault:
      type: explode
`,
			line: 5,
			path: "rules[0].fault.type",
			want: "unknown fault type",
		},
		{
			name: "bad behavior parameter",
			yaml: `
rules:
  - id: a
    name: A
    fault:
      type: refuse
    behavior:
      type: percent
      percent: 250
`,
			line: 8,
			path: "rules[0].behavior.percent",
			want: "100 or less",
		},
		{
			name: "duplicate rule id",
			yaml: `
rules:
  - id: a
    name: A
    fault:
      type: refuse
  - id: a
    name: Also A
    fault:
      type: refuse
`,
			line: 6,
			path: "rules[1].id",
			want: "duplicate",
		},
		{
			name: "duplicate inline rule id",
			yaml: `
rules:
  - id: a
    name: A
    fault:
      type: refuse
scenarios:
  - name: outage
    rules:
      - id: a
        name: A again
        fault:
          type: refuse
`,
			line: 9,
			path: "scenarios[0].rules[0].id",
			want: "duplicate",
		},
		{
			name: "missing rule id",
			yaml: `
rules:
  - name: A
    fault:
      type: refuse
`,
			line: 2,
			path: "rules[0].id",
			want: "required",
		},
		{
			name: "bad rule id",
			yaml: `
rules:
  - id: "a rule"
    name: A
    fault:
      type: refuse
`,
			line: 2,
			path: "rules[0].id",
			want: "letters",
		},
		{
			name: "rule without a fault",
			yaml: `
rules:
  - id: a
    name: A
`,
			line: 2,
			path: "rules[0].fault",
			want: "required",
		},
		{
			name: "match host is not a url",
			yaml: `
rules:
  - id: a
    name: A
    match:
      host: https://api.stripe.com/v1
    fault:
      type: refuse
`,
			line: 5,
			path: "rules[0].match.host",
			want: "hostname",
		},
		{
			name: "duplicate scenario name",
			yaml: `
scenarios:
  - name: outage
    rules: []
  - name: outage
    rules: []
`,
			line: 4,
			path: "scenarios[1].name",
			want: "duplicate",
		},
		{
			name: "scenario names a rule that is not there",
			yaml: `
scenarios:
  - name: outage
    rules:
      - ghost
`,
			line: 4,
			path: "scenarios[0].rules[0]",
			want: "no rule",
		},
		{
			name: "duplicate route name",
			yaml: `
routes:
  - name: stripe
    upstream: https://api.stripe.com
  - name: stripe
    upstream: https://api.stripe.com
`,
			line: 4,
			path: "routes[1].name",
			want: "duplicate",
		},
		{
			name: "duplicate route port",
			yaml: `
routes:
  - name: stripe
    upstream: https://api.stripe.com
    port: 9100
  - name: orders
    upstream: http://orders.internal
    port: 9100
`,
			line: 7,
			path: "routes[1].port",
			want: "duplicate",
		},
		{
			name: "route upstream without a scheme",
			yaml: `
routes:
  - name: stripe
    upstream: api.stripe.com
`,
			line: 3,
			path: "routes[0].upstream",
			want: "http or https",
		},
		{
			name: "bypass entry that is not a host",
			yaml: `
bypass:
  - "in*ernal"
`,
			line: 2,
			path: "bypass[0]",
			want: "*",
		},
		{
			name: "the same key twice",
			yaml: `
rules:
  - id: a
    name: A
    name: B
    fault:
      type: refuse
`,
			line: 4,
			path: "rules[0]",
			want: "twice",
		},
		{
			name: "wrong type for a list",
			yaml: `
rules:
  id: a
`,
			line: 2,
			path: "rules",
			want: "list",
		},
		{
			name: "wrong type for a scalar",
			yaml: `
rules:
  - id: a
    name: [A]
    fault:
      type: refuse
`,
			line: 3,
			path: "rules[0].name",
			want: "text",
		},
		{
			name: "not yaml at all",
			yaml: `
rules: [
`,
			line: 1,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("faultline.yaml", numbered(tt.yaml))

			var cerr *Error
			if !errors.As(err, &cerr) {
				t.Fatalf("err = %v, want a *config.Error", err)
			}
			if cerr.File != "faultline.yaml" {
				t.Errorf("file = %q, want faultline.yaml", cerr.File)
			}
			if cerr.Line != tt.line {
				t.Errorf("line = %d, want %d (message: %s)", cerr.Line, tt.line, cerr)
			}
			if cerr.Path != tt.path {
				t.Errorf("path = %q, want %q", cerr.Path, tt.path)
			}
			if !strings.Contains(cerr.Message, tt.want) {
				t.Errorf("message = %q, want it to mention %q", cerr.Message, tt.want)
			}
			if !strings.HasPrefix(cerr.Error(), "faultline.yaml:") {
				t.Errorf("Error() = %q, want it to start with the file and line", cerr.Error())
			}
		})
	}
}

func TestLoadReadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "faultline.yaml")
	if err := os.WriteFile(path, []byte(goodFile), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Path != path {
		t.Errorf("Path = %q, want %q", cfg.Path, path)
	}
	if len(cfg.Rules) != 2 {
		t.Errorf("rules = %d, want 2", len(cfg.Rules))
	}
}

func TestLoadReportsTheRealPathInErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "faultline.yaml")
	if err := os.WriteFile(path, []byte("nonsense: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	var cerr *Error
	if !errors.As(err, &cerr) {
		t.Fatalf("err = %v, want a *config.Error", err)
	}
	if cerr.File != path {
		t.Errorf("file = %q, want %q", cerr.File, path)
	}
}

func TestLoadSaysWhenTheFileIsMissing(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "faultline.yaml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want it to wrap os.ErrNotExist", err)
	}
}

// TestTheExampleConfigIsValid keeps the file users copy honest. It is the only
// test that reads outside the package, and it is worth it: an example that does
// not load is worse than no example.
func TestTheExampleConfigIsValid(t *testing.T) {
	const path = "../../examples/faultline.yaml"

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	if len(cfg.Routes) == 0 || len(cfg.Rules) == 0 || len(cfg.Scenarios) == 0 {
		t.Errorf("the example should show a route, a rule, and a scenario, got %+v", cfg)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# yaml-language-server: $schema=" + SchemaID; !strings.HasPrefix(string(data), want) {
		t.Errorf("the example should start with %q so editors validate it", want)
	}
}

func TestParseReadsEveryShapeOfFaultParameter(t *testing.T) {
	cfg, err := Parse("faultline.yaml", numbered(`
rules:
  - id: strip
    name: Strip the cache headers
    enabled: false
    fault:
      type: headers
      set:
        X-Faultline: "1"
      remove:
        - ETag
        - Cache-Control
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	rule := cfg.Rules[0]
	if rule.Enabled {
		t.Error("enabled: false at the top level should be honoured")
	}

	set, ok := rule.Fault.Params["set"].(map[string]any)
	if !ok || set["X-Faultline"] != "1" {
		t.Errorf("set = %#v, want a mapping of names to text", rule.Fault.Params["set"])
	}
	remove, ok := rule.Fault.Params["remove"].([]any)
	if !ok || len(remove) != 2 || remove[0] != "ETag" {
		t.Errorf("remove = %#v, want a list of names", rule.Fault.Params["remove"])
	}
}

func TestParseFollowsAnchorsAndAliases(t *testing.T) {
	cfg, err := Parse("faultline.yaml", numbered(`
rules:
  - id: slow-stripe
    name: Stripe is slow
    match: &stripe
      host: api.stripe.com
    fault: &slow
      type: delay
      ms: 100
  - id: slow-stripe-again
    name: Stripe is slow here too
    match: *stripe
    fault: *slow
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(cfg.Rules))
	}
	second := cfg.Rules[1]
	if second.Match.Host != "api.stripe.com" || second.Fault.Type != "delay" || second.Fault.Params["ms"] != 100 {
		t.Errorf("the aliased rule = %+v, want the same match and fault as the first", second)
	}
}

func TestParseRejectsValuesItCannotCarry(t *testing.T) {
	// A fault parameter has to survive the trip to JSON, since the same rule can
	// arrive over the API. Anything else is refused where it is written.
	_, err := Parse("faultline.yaml", numbered(`
rules:
  - id: a
    name: A
    fault:
      type: status
      code: 503
      body: !!binary aGk=
`))

	var cerr *Error
	if !errors.As(err, &cerr) {
		t.Fatalf("err = %v, want a *config.Error", err)
	}
	if cerr.Line != 7 || cerr.Path != "rules[0].fault.body" {
		t.Errorf("err = %v, want it to point at the body on line 7", cerr)
	}
}

func TestParseTakesFractionsAndRejectsThemWhereTheyDoNotBelong(t *testing.T) {
	_, err := Parse("faultline.yaml", numbered(`
rules:
  - id: a
    name: A
    fault:
      type: delay
      ms: 1.5
`))

	var cerr *Error
	if !errors.As(err, &cerr) {
		t.Fatalf("err = %v, want a *config.Error", err)
	}
	if cerr.Path != "rules[0].fault.ms" || !strings.Contains(cerr.Message, "whole number") {
		t.Errorf("err = %v, want the catalogue's complaint about ms", cerr)
	}
}
