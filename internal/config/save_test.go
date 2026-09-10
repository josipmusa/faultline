package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

const exampleConfig = "../../examples/faultline.yaml"

const commentedFile = `# yaml-language-server: $schema=../../schema/faultline.schema.json
#
# The file a developer wrote by hand.

# Explicit routes, for clients that ignore HTTP_PROXY.
routes:
  - name: stripe
    upstream: https://api.stripe.com

# Rules written here are on as soon as the file is read.
rules:
  # Stripe takes its time.
  - id: slow-stripe
    name: Stripe is slow
    match:
      host: api.stripe.com
    fault:
      type: delay
      ms: 2000

scenarios:
  - name: payments-down
    rules:
      - slow-stripe
      - id: stripe-503
        name: Stripe answers 503
        fault:
          type: status
          code: 503
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "faultline.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the config: %v", err)
	}
	return path
}

func loadRules(t *testing.T, path string) []rules.Rule {
	t.Helper()
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	return cfg.Rules
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// mustSave writes rs back, leaving the bypass list and the scenarios the file
// already holds as they are, which is what saving a rule change does.
func mustSave(t *testing.T, path string, rs []rules.Rule) {
	t.Helper()
	var bypass []string
	var scenarios []rules.Scenario
	if cfg, err := Load(path); err == nil {
		bypass, scenarios = cfg.Bypass, cfg.Scenarios
	}
	if err := Save(path, rs, bypass, scenarios); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// loadScenarios is the scenario list as the file declares it, which is what a
// save that changes nothing about them passes back.
func loadScenarios(t *testing.T, path string) []rules.Scenario {
	t.Helper()
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg.Scenarios
}

func find(rs []rules.Rule, id string) (rules.Rule, bool) {
	for _, r := range rs {
		if r.ID == id {
			return r, true
		}
	}
	return rules.Rule{}, false
}

func TestSaveLeavesAFileItNeedNotChangeExactlyAsItWas(t *testing.T) {
	path := writeConfig(t, commentedFile)

	mustSave(t, path, loadRules(t, path))

	if got := read(t, path); got != commentedFile {
		t.Errorf("saving the rules it already holds rewrote the file:\n%s", got)
	}
}

func TestSaveKeepsCommentsAroundAnEditedRule(t *testing.T) {
	path := writeConfig(t, commentedFile)

	edited := loadRules(t, path)
	for i := range edited {
		if edited[i].ID == "slow-stripe" {
			edited[i].Fault.Params["ms"] = 50
		}
	}
	mustSave(t, path, edited)

	body := read(t, path)
	for _, want := range []string{
		"# yaml-language-server:",
		"# Explicit routes, for clients that ignore HTTP_PROXY.",
		"# Rules written here are on as soon as the file is read.",
		"# Stripe takes its time.",
		"ms: 50",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the saved file lost %q:\n%s", want, body)
		}
	}

	after, ok := find(loadRules(t, path), "slow-stripe")
	if !ok || after.Fault.Params["ms"] != 50 {
		t.Errorf("the edited rule reloaded as %+v", after)
	}
}

func TestSaveAppendsANewRuleToTheRules(t *testing.T) {
	path := writeConfig(t, commentedFile)

	added := rules.Rule{
		ID:       "flaky-orders",
		Name:     "Orders is flaky",
		Enabled:  true,
		Match:    rules.Match{Host: "orders.internal", Method: "GET", Header: map[string]string{"X-Test": "1"}},
		Fault:    rules.Fault{Type: "status", Params: rules.Params{"code": 503}},
		Behavior: &rules.Behavior{Type: "first_n", Params: rules.Params{"n": 2}},
	}
	mustSave(t, path, append(loadRules(t, path), added))

	body := read(t, path)
	if !strings.Contains(body, "# Stripe takes its time.") {
		t.Errorf("adding a rule dropped a comment:\n%s", body)
	}

	reloaded := loadRules(t, path)
	got, ok := find(reloaded, "flaky-orders")
	if !ok {
		t.Fatalf("the new rule is not in the saved file:\n%s", body)
	}
	if !reflect.DeepEqual(got, added) {
		t.Errorf("the new rule reloaded as\n %+v\nwant %+v", got, added)
	}
	if len(reloaded) != 3 {
		t.Errorf("the saved file holds %d rules, want 3", len(reloaded))
	}
}

func TestSaveEditsAScenarioRuleWhereItIsWritten(t *testing.T) {
	path := writeConfig(t, commentedFile)

	edited := loadRules(t, path)
	for i := range edited {
		if edited[i].ID == "stripe-503" {
			edited[i].Fault.Params["code"] = 429
		}
	}
	mustSave(t, path, edited)

	body := read(t, path)
	if strings.Count(body, "id: stripe-503") != 1 {
		t.Errorf("the scenario rule was copied to the top level, so its id is written twice:\n%s", body)
	}
	if strings.Index(body, "id: stripe-503") < strings.Index(body, "scenarios:") {
		t.Errorf("the scenario rule moved out of its scenario:\n%s", body)
	}

	after, ok := find(loadRules(t, path), "stripe-503")
	if !ok || after.Fault.Params["code"] != 429 {
		t.Fatalf("the scenario rule reloaded as %+v", after)
	}
	if after.Enabled {
		t.Error("a rule written inside a scenario came back enabled")
	}
}

func TestSaveRemovesARuleAndTheScenarioEntryNamingIt(t *testing.T) {
	path := writeConfig(t, commentedFile)

	kept := []rules.Rule{}
	for _, r := range loadRules(t, path) {
		if r.ID != "slow-stripe" {
			kept = append(kept, r)
		}
	}
	mustSave(t, path, kept)

	body := read(t, path)
	if strings.Contains(body, "slow-stripe") {
		t.Errorf("the deleted rule is still named in the file:\n%s", body)
	}

	reloaded := loadRules(t, path)
	if len(reloaded) != 1 || reloaded[0].ID != "stripe-503" {
		t.Errorf("after deleting one rule the file holds %v", reloaded)
	}
}

func TestSaveRemovesTheLastRuleAndLeavesAFileThatLoads(t *testing.T) {
	path := writeConfig(t, "rules:\n  - id: only\n    name: The only rule\n    fault:\n      type: delay\n      ms: 1\n")

	mustSave(t, path, nil)

	if got := loadRules(t, path); len(got) != 0 {
		t.Errorf("the file still holds %v", got)
	}
}

func TestSaveWritesAFileThatIsNotThereYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "faultline.yaml")

	added := rules.Rule{
		ID:      "slow",
		Name:    "Slow",
		Enabled: true,
		Fault:   rules.Fault{Type: "delay", Params: rules.Params{"ms": 100}},
	}
	mustSave(t, path, []rules.Rule{added})

	got := loadRules(t, path)
	if len(got) != 1 || !reflect.DeepEqual(got[0], added) {
		t.Errorf("the created file holds %+v\n%s", got, read(t, path))
	}
}

func TestSaveLeavesNoTemporaryFileBehind(t *testing.T) {
	path := writeConfig(t, commentedFile)

	mustSave(t, path, loadRules(t, path))

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("reading the directory: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("the directory holds %d files, want only the config", len(entries))
	}
}

func TestSaveKeepsTheModeOfTheFile(t *testing.T) {
	path := writeConfig(t, commentedFile)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	mustSave(t, path, loadRules(t, path))

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode is %v, want 0640", info.Mode().Perm())
	}
}

func TestSaveRefusesAFileItCannotRead(t *testing.T) {
	path := writeConfig(t, "rules: [\n")

	err := Save(path, nil, nil, nil)
	if err == nil {
		t.Fatal("Save into a broken file returned no error")
	}
	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Save error is %T (%v), want a *config.Error", err, err)
	}
}

func TestSaveLeavesTheExampleConfigExactlyAsItIs(t *testing.T) {
	original, err := os.ReadFile(exampleConfig)
	if err != nil {
		t.Fatalf("reading %s: %v", exampleConfig, err)
	}
	path := writeConfig(t, string(original))

	mustSave(t, path, loadRules(t, path))

	if got := read(t, path); got != string(original) {
		t.Errorf("saving the example config rewrote it:\n%s", got)
	}
}

// A gap between two rules is written back as an empty line, not as the
// indentation the encoder puts in front of the blank line of a comment block.
// Trailing spaces in a committed file are noise in every diff that follows.
func TestSaveWritesAGapBetweenRulesAsAnEmptyLine(t *testing.T) {
	const spaced = `rules:
  # The first one.
  - id: a
    name: Rule A
    match:
      host: example.com
    fault:
      type: status
      code: 500

  # The second one, after a gap.
  - id: b
    name: Rule B
    match:
      host: other.com
    fault:
      type: delay
      ms: 10
`
	path := writeConfig(t, spaced)

	edited := loadRules(t, path)
	for i := range edited {
		if edited[i].ID == "a" {
			edited[i] = edited[i].WithEnabled(false)
		}
	}
	mustSave(t, path, edited)

	body := read(t, path)
	if !strings.Contains(body, "\n\n  # The second one, after a gap.") {
		t.Errorf("the gap above the second rule is not an empty line:\n%q", body)
	}
	for i, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" && line != "" {
			t.Errorf("line %d is %q, spaces where an empty line belongs", i+1, line)
		}
	}
}

// A block scalar's value is not layout: a line of spaces inside one is part of
// what the rule sends, so emptying it would change the fault.
func TestSaveKeepsSpacesInsideABlockScalar(t *testing.T) {
	path := writeConfig(t, commentedFile)

	edited := loadRules(t, path)
	for i := range edited {
		if edited[i].ID == "stripe-503" {
			edited[i].Fault.Params["body"] = "first\n   \nlast\n"
		}
	}
	mustSave(t, path, edited)

	after, ok := find(loadRules(t, path), "stripe-503")
	if !ok {
		t.Fatal("the rule is gone after saving it")
	}
	if got := after.Fault.Params["body"]; got != "first\n   \nlast\n" {
		t.Errorf("the body came back as %q", got)
	}
}

const bypassFile = `# The file a developer wrote by hand.

# Hosts Faultline keeps its hands off.
bypass:
  # Our own telemetry, which pins its certificate.
  - telemetry.internal
  - httpbin.org

rules:
  - id: slow-stripe
    name: Stripe is slow
    match:
      host: api.stripe.com
    fault:
      type: delay
      ms: 2000
`

func TestSaveWritesTheBypassList(t *testing.T) {
	path := writeConfig(t, bypassFile)

	if err := Save(path, loadRules(t, path), []string{"telemetry.internal", "api.stripe.com"}, loadScenarios(t, path)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	body := read(t, path)
	for _, want := range []string{
		"# Hosts Faultline keeps its hands off.",
		"# Our own telemetry, which pins its certificate.",
		"- telemetry.internal",
		"- api.stripe.com",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the saved file lost %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "httpbin.org") {
		t.Errorf("the host taken off the list is still written:\n%s", body)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if !reflect.DeepEqual(cfg.Bypass, []string{"telemetry.internal", "api.stripe.com"}) {
		t.Errorf("bypass = %q, want what was saved", cfg.Bypass)
	}
}

func TestSaveAddsABypassKeyToAFileWithout(t *testing.T) {
	path := writeConfig(t, commentedFile)

	if err := Save(path, loadRules(t, path), []string{"httpbin.org"}, loadScenarios(t, path)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if !reflect.DeepEqual(cfg.Bypass, []string{"httpbin.org"}) {
		t.Errorf("bypass = %q, want the host added", cfg.Bypass)
	}
}

func TestSaveLeavesAFileWithoutABypassListAlone(t *testing.T) {
	path := writeConfig(t, commentedFile)

	if err := Save(path, loadRules(t, path), nil, loadScenarios(t, path)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := read(t, path); got != commentedFile {
		t.Errorf("saving an empty bypass list rewrote the file:\n%s", got)
	}
}

// An emptied list has to leave the key behind, or the next load would read the
// hosts that were just taken off it.
func TestSaveEmptiesTheBypassList(t *testing.T) {
	path := writeConfig(t, bypassFile)

	if err := Save(path, loadRules(t, path), nil, loadScenarios(t, path)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if len(cfg.Bypass) != 0 {
		t.Errorf("bypass = %q, want none", cfg.Bypass)
	}
}

func TestSaveAppendsANewScenarioNamingRulesTheFileHolds(t *testing.T) {
	path := writeConfig(t, commentedFile)

	added := rules.Scenario{Name: "stripe-slow-only", Rules: []string{"slow-stripe"}}
	if err := Save(path, loadRules(t, path), nil, append(loadScenarios(t, path), added)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if len(cfg.Scenarios) != 2 {
		t.Fatalf("the file declares %d scenarios, want 2", len(cfg.Scenarios))
	}
	if got := cfg.Scenarios[1]; got.Name != added.Name || !reflect.DeepEqual(got.Rules, added.Rules) {
		t.Errorf("scenarios[1] = %+v, want %+v written last", got, added)
	}
	body := read(t, path)
	if !strings.Contains(body, "# Stripe takes its time.") {
		t.Errorf("appending a scenario lost a comment:\n%s", body)
	}
	if !strings.Contains(body, "name: Stripe answers 503") {
		t.Errorf("appending a scenario lost the rule written inside the other one:\n%s", body)
	}
}

// A scenario the file already declares is never rewritten: its rules list may
// hold a whole rule written in place, and writing that list back as ids would
// leave a file that no longer loads.
func TestSaveLeavesTheScenariosTheFileWritesExactlyAsTheyAre(t *testing.T) {
	path := writeConfig(t, commentedFile)

	if err := Save(path, loadRules(t, path), nil, loadScenarios(t, path)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := read(t, path); got != commentedFile {
		t.Errorf("saving the scenarios it already holds rewrote the file:\n%s", got)
	}
}

func TestSaveAddsAScenariosKeyToAFileWithout(t *testing.T) {
	path := writeConfig(t, bypassFile)

	added := rules.Scenario{Name: "stripe-slow-only", Rules: []string{"slow-stripe"}}
	if err := Save(path, loadRules(t, path), loadBypass(t, path), []rules.Scenario{added}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if len(cfg.Scenarios) != 1 || !reflect.DeepEqual(cfg.Scenarios[0].Rules, added.Rules) {
		t.Errorf("scenarios = %+v, want the one added", cfg.Scenarios)
	}
	// A section of its own gets a line of its own: the file spaces its keys
	// out, and a key appended against the end of the last one reads as part
	// of it.
	if body := read(t, path); !strings.Contains(body, "\n\nscenarios:") {
		t.Errorf("the new key is jammed against what came before it:\n%s", body)
	}
}

// A scenario naming a rule the file does not hold would not load, so it is
// refused rather than written.
func TestSaveRefusesAScenarioNamingARuleThatIsNotThere(t *testing.T) {
	path := writeConfig(t, commentedFile)

	added := rules.Scenario{Name: "ghost", Rules: []string{"nothing-by-that-id"}}
	err := Save(path, loadRules(t, path), nil, append(loadScenarios(t, path), added))
	if err == nil {
		t.Fatalf("Save wrote a scenario naming a rule that is not there")
	}
	if got := read(t, path); got != commentedFile {
		t.Errorf("the refused save changed the file:\n%s", got)
	}
}

func loadBypass(t *testing.T, path string) []string {
	t.Helper()
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg.Bypass
}
