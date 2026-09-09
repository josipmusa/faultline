package config

import (
	"strings"
	"testing"
)

// The file `faultline init` writes lands in a directory Faultline has never
// seen, and the next thing that happens to it is being read back. If it does
// not load, the first minute of the product is an error message.
func TestTheStarterFileLoadsWithNoErrors(t *testing.T) {
	cfg, err := Parse("faultline.yaml", []byte(Starter))
	if err != nil {
		t.Fatalf("parsing the starter file: %v", err)
	}
	if len(cfg.Rules) == 0 {
		t.Error("the starter file shows no rule, and a rule is what a reader came for")
	}
	if len(cfg.Scenarios) != 1 {
		t.Errorf("the starter file declares %d scenarios, want the one example", len(cfg.Scenarios))
	}
}

// Nothing in the starter file is on: `faultline serve` right after
// `faultline init` has to leave the developer's traffic exactly as it was.
func TestTheStarterFileChangesNothingUntilItIsEdited(t *testing.T) {
	cfg, err := Parse("faultline.yaml", []byte(Starter))
	if err != nil {
		t.Fatalf("parsing the starter file: %v", err)
	}
	for _, r := range cfg.Rules {
		if r.Enabled {
			t.Errorf("rule %q arrives enabled; a starter file that faults traffic on sight is a trap", r.ID)
		}
	}
	if len(cfg.Routes) != 0 {
		t.Errorf("the starter file opens listeners %+v; nothing should be bound after init", cfg.Routes)
	}
	if len(cfg.Bypass) != 0 {
		t.Errorf("the starter file hides %v from the event stream before anyone asked", cfg.Bypass)
	}
}

func TestTheStarterFilePointsEditorsAtTheSchema(t *testing.T) {
	if want := "# yaml-language-server: $schema=" + SchemaID; !strings.HasPrefix(Starter, want) {
		t.Errorf("the starter file starts %q, want it to start with %q", firstLine(Starter), want)
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
