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

func mustSave(t *testing.T, path string, rs []rules.Rule) {
	t.Helper()
	if err := Save(path, rs); err != nil {
		t.Fatalf("Save: %v", err)
	}
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

	err := Save(path, nil)
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
