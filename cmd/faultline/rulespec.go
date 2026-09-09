package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	client "github.com/josipmusa/faultline/clients/go"
)

// ruleSpec is a rule written on the command line. The flag surface is
// deliberately small and regular: the match has one flag per field, and both
// catalogues - faults and behaviors - are reached through a type and repeated
// name=value settings, so a fault added later needs no new flag here.
type ruleSpec struct {
	id       string
	name     string
	host     string
	method   string
	path     string
	headers  []string
	fault    string
	settings []string

	behavior    string
	behaviorSet []string

	disabled bool
	from     string
}

// shapingFlags are the flags --from replaces. Mixing the two ways of writing a
// rule would leave it unclear which one won.
var shapingFlags = []string{
	"id", "name", "host", "method", "path", "header",
	"fault", "set", "behavior", "behavior-set", "disabled",
}

func (s *ruleSpec) register(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&s.id, "id", "", "id for the rule; left out, Faultline makes one from the name")
	f.StringVar(&s.name, "name", "", "what the rule is for (--name \"Stripe is slow\")")
	f.StringVar(&s.host, "host", "", "upstream the rule matches; left out, it matches every host")
	f.StringVar(&s.method, "method", "", "HTTP method the rule matches (--method POST)")
	f.StringVar(&s.path, "path", "", "path glob the rule matches (--path '/v1/charges/*')")
	f.StringArrayVar(&s.headers, "header", nil,
		"request header the rule matches as name=value, repeatable (--header X-Test=1)")
	f.StringVar(&s.fault, "fault", "", "fault to inject (--fault delay)")
	f.StringArrayVar(&s.settings, "set", nil,
		"fault parameter as name=value, repeatable (--set ms=2000); "+
			"a value is read as JSON when it is JSON, so quote text that looks like a number")
	f.StringVar(&s.behavior, "behavior", "", "behavior that gates the fault (--behavior first_n)")
	f.StringArrayVar(&s.behaviorSet, "behavior-set", nil,
		"behavior parameter as name=value, repeatable (--behavior-set n=2)")
	f.BoolVar(&s.disabled, "disabled", false, "add the rule turned off")
	f.StringVar(&s.from, "from", "",
		"read the whole rule as JSON from a file, or from - for standard input")
}

// rule builds the rule to send. Nothing here judges what the fault catalogue
// will accept: the API validates, and its answer names the field.
func (s *ruleSpec) rule(cmd *cobra.Command) (client.Rule, error) {
	if s.from != "" {
		if used := changedAmong(cmd, shapingFlags); len(used) > 0 {
			return client.Rule{}, fmt.Errorf(
				"--from reads the whole rule, so it cannot be used with %s", strings.Join(used, ", "))
		}
		return readRule(cmd, s.from)
	}

	header, err := settings("--header", s.headers, asText)
	if err != nil {
		return client.Rule{}, err
	}
	faultParams, err := settings("--set", s.settings, paramValue)
	if err != nil {
		return client.Rule{}, err
	}
	behavior, err := s.behaviorFor()
	if err != nil {
		return client.Rule{}, err
	}

	return client.Rule{
		ID:      s.id,
		Name:    s.ruleName(),
		Enabled: !s.disabled,
		Match: client.Match{
			Host:   s.host,
			Method: s.method,
			Path:   s.path,
			Header: header,
		},
		Fault:    client.Fault{Type: client.FaultType(s.fault), Params: faultParams},
		Behavior: behavior,
	}, nil
}

func (s *ruleSpec) behaviorFor() (*client.Behavior, error) {
	if s.behavior == "" {
		if len(s.behaviorSet) > 0 {
			return nil, errors.New("--behavior-set needs a --behavior to set parameters on")
		}
		return nil, nil
	}

	params, err := settings("--behavior-set", s.behaviorSet, paramValue)
	if err != nil {
		return nil, err
	}
	return &client.Behavior{Type: client.BehaviorType(s.behavior), Params: params}, nil
}

// ruleName keeps the name the API requires from being a flag the operator has
// to remember. "delay on api.stripe.com" is what the rule is, said plainly.
func (s *ruleSpec) ruleName() string {
	switch {
	case s.name != "":
		return s.name
	case s.fault != "" && s.host != "":
		return s.fault + " on " + s.host
	case s.fault != "":
		return s.fault
	case s.host != "":
		return "rule for " + s.host
	default:
		return "rule"
	}
}

// readRule decodes a whole rule from a file, or from standard input for "-".
// It starts from an enabled rule so an absent "enabled" means the same here as
// it does on POST /api/rules.
func readRule(cmd *cobra.Command, from string) (client.Rule, error) {
	source := cmd.InOrStdin()
	name := "standard input"

	if from != "-" {
		file, err := os.Open(from) //nolint:gosec // the path is the operator's own argument
		if err != nil {
			return client.Rule{}, fmt.Errorf("--from: %w", err)
		}
		defer func() { _ = file.Close() }()
		source, name = file, from
	}

	rule := client.Rule{Enabled: true}
	if err := json.NewDecoder(io.LimitReader(source, maxRuleBytes)).Decode(&rule); err != nil {
		return client.Rule{}, fmt.Errorf("--from: %s is not a rule: %w", name, err)
	}
	return rule, nil
}

const maxRuleBytes = 64 << 10

// settings turns repeated name=value flags into a map, with value read by the
// caller's own reading of what a value is.
func settings[T any](flag string, raw []string, value func(string) T) (map[string]T, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	out := make(map[string]T, len(raw))
	for _, setting := range raw {
		name, text, ok := strings.Cut(setting, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("%s %q: want name=value", flag, setting)
		}
		out[name] = value(text)
	}
	return out, nil
}

func asText(raw string) string { return raw }

// paramValue reads a parameter the way the catalogue means it: 2000 is the
// number, {"X-Test":"1"} is the map, ["Server"] is the list, and anything else
// is text. Quoting text is how to write a string that looks like a number.
func paramValue(raw string) any {
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
		switch decoded.(type) {
		case float64, bool, map[string]any, []any, string:
			return decoded
		}
	}
	return raw
}

// changedAmong reports which of the named flags the operator actually set.
func changedAmong(cmd *cobra.Command, names []string) []string {
	var used []string
	for _, name := range names {
		if cmd.Flags().Changed(name) {
			used = append(used, "--"+name)
		}
	}
	return used
}
