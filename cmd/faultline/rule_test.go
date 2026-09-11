package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/events"
)

func TestRuleAddFromFlagsAndList(t *testing.T) {
	i := newInstance(t)

	out := i.run(t, "rule", "add",
		"--name", "Httpbin is slow",
		"--host", "httpbin.org",
		"--method", "GET",
		"--path", "/get",
		"--fault", "delay",
		"--set", "ms=2000",
		"--set", "jitter_ms=100",
		"--behavior", "first_n",
		"--behavior-set", "n=2")

	// Parameters are rendered in name order, so the table reads the same way
	// however the flags were written.
	wantLine(t, out, "httpbin-is-slow", "yes", "GET httpbin.org/get",
		"delay jitter_ms=100 ms=2000", "first_n n=2")

	list := i.run(t, "rule", "list")
	wantLine(t, list, "httpbin-is-slow", "delay jitter_ms=100 ms=2000")

	stored := i.rules.List()
	if len(stored) != 1 {
		t.Fatalf("store holds %d rules, want 1", len(stored))
	}
	if got := stored[0].Fault.Params["ms"]; got != float64(2000) {
		t.Errorf("ms = %v (%T), want the number 2000", got, got)
	}
}

func TestRuleAddWithoutANameNamesItselfAfterTheFaultAndHost(t *testing.T) {
	i := newInstance(t)

	i.run(t, "rule", "add", "--host", "api.stripe.com", "--fault", "delay", "--set", "ms=2000")

	stored := i.rules.List()
	if len(stored) != 1 {
		t.Fatalf("store holds %d rules, want 1", len(stored))
	}
	if stored[0].Name != "delay on api.stripe.com" {
		t.Errorf("name = %q, want it derived from the fault and the host", stored[0].Name)
	}
}

func TestRuleAddReportsTheFieldTheAPIRefused(t *testing.T) {
	i := newInstance(t)

	err := i.runErr(t, "rule", "add", "--host", "httpbin.org", "--fault", "delay", "--set", "ms=soon")
	if err == nil {
		t.Fatal("rule add with a bad parameter = nil error, want one")
	}
	if !strings.Contains(err.Error(), "fault.ms") {
		t.Errorf("error = %q, want it to name fault.ms", err)
	}
}

func TestRuleAddWithoutAFaultAsksForOne(t *testing.T) {
	i := newInstance(t)

	err := i.runErr(t, "rule", "add", "--host", "httpbin.org")
	if err == nil || !strings.Contains(err.Error(), "fault.type") {
		t.Fatalf("error = %v, want it to name fault.type", err)
	}
}

func TestRuleAddRejectsAMalformedSetting(t *testing.T) {
	i := newInstance(t)

	err := i.runErr(t, "rule", "add", "--host", "httpbin.org", "--fault", "delay", "--set", "ms")
	if err == nil || !strings.Contains(err.Error(), "--set") {
		t.Fatalf("error = %v, want it to name --set", err)
	}
}

func TestRuleAddSettingsTakeJSONForShapesAndQuotesForText(t *testing.T) {
	i := newInstance(t)

	i.run(t, "rule", "add", "--name", "Rewrite the headers", "--host", "httpbin.org",
		"--fault", "headers",
		"--set", `set={"X-Test":"1"}`,
		"--set", `remove=["Server"]`)

	stored := i.rules.List()
	if len(stored) != 1 {
		t.Fatalf("store holds %d rules, want 1", len(stored))
	}
	set, ok := stored[0].Fault.Params["set"].(map[string]any)
	if !ok || set["X-Test"] != "1" {
		t.Errorf("set = %#v, want a map with X-Test", stored[0].Fault.Params["set"])
	}
	remove, ok := stored[0].Fault.Params["remove"].([]any)
	if !ok || len(remove) != 1 || remove[0] != "Server" {
		t.Errorf("remove = %#v, want a list with Server", stored[0].Fault.Params["remove"])
	}
}

func TestRuleAddFromAJSONFile(t *testing.T) {
	i := newInstance(t)

	path := filepath.Join(t.TempDir(), "rule.json")
	body := `{"id":"slow-stripe","name":"Stripe is slow","match":{"host":"api.stripe.com"},` +
		`"fault":{"type":"delay","ms":2000}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the rule file: %v", err)
	}

	i.run(t, "rule", "add", "--from", path)

	if _, err := i.rules.Get("slow-stripe"); err != nil {
		t.Fatalf("rule from the file is not in the store: %v", err)
	}
}

func TestRuleAddFromStdin(t *testing.T) {
	i := newInstance(t)

	root := newRootCmd()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(`{"name":"From stdin","match":{"host":"httpbin.org"},` +
		`"fault":{"type":"status","code":503}}`))
	root.SetArgs([]string{"rule", "add", "--from", "-", "--admin", i.url})

	if err := root.Execute(); err != nil {
		t.Fatalf("rule add --from -: %v", err)
	}
	if _, err := i.rules.Get("from-stdin"); err != nil {
		t.Fatalf("rule from stdin is not in the store: %v", err)
	}
}

func TestRuleAddFromAndTheShapingFlagsAreExclusive(t *testing.T) {
	i := newInstance(t)

	err := i.runErr(t, "rule", "add", "--from", "-", "--fault", "delay")
	if err == nil || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("error = %v, want it to say --from is on its own", err)
	}
}

func TestRuleEnableDisableAndRemove(t *testing.T) {
	i := newInstance(t)

	i.run(t, "rule", "add", "--name", "Slow", "--host", "httpbin.org", "--fault", "delay", "--set", "ms=1")

	if out := i.run(t, "rule", "disable", "slow"); !strings.Contains(out, "no") {
		t.Errorf("disable printed %q, want the rule shown as off", out)
	}
	if rule, _ := i.rules.Get("slow"); rule.Enabled {
		t.Error("rule is still enabled after disable")
	}

	i.run(t, "rule", "enable", "slow")
	if rule, _ := i.rules.Get("slow"); !rule.Enabled {
		t.Error("rule is still disabled after enable")
	}

	if out := i.run(t, "rule", "rm", "slow"); !strings.Contains(out, "removed rule") {
		t.Errorf("rm printed %q, want it to say what it removed", out)
	}
	if len(i.rules.List()) != 0 {
		t.Errorf("store holds %d rules, want none", len(i.rules.List()))
	}
}

func TestRuleRmReportsAnUnknownID(t *testing.T) {
	i := newInstance(t)

	err := i.runErr(t, "rule", "rm", "ghost")
	if err == nil || !strings.Contains(err.Error(), `no rule with id "ghost"`) {
		t.Fatalf("error = %v, want the API's own message", err)
	}
}

func TestRuleListJSONIsTheAPIsOwnShape(t *testing.T) {
	i := newInstance(t)
	i.run(t, "rule", "add", "--name", "Slow", "--host", "httpbin.org", "--fault", "delay", "--set", "ms=1")

	list := decodeJSON[[]client.Rule](t, i.run(t, "rule", "list", "--json"))

	if len(list) != 1 || list[0].ID != "slow" || list[0].Fault.Type != "delay" {
		t.Fatalf("rule list --json = %+v, want the one delay rule", list)
	}
}

func TestRuleListWithNoRulesSaysSo(t *testing.T) {
	i := newInstance(t)

	if out := i.run(t, "rule", "list"); !strings.Contains(out, "no rules") {
		t.Errorf("rule list printed %q, want it to say there are none", out)
	}
	if out := i.run(t, "rule", "list", "--json"); strings.TrimSpace(out) != "[]" {
		t.Errorf("rule list --json printed %q, want an empty array", out)
	}
}

// runSplit executes a command with stdout and stderr kept apart, which is the
// point of a warning: it must not land in a piped --json payload.
func (i instance) runSplit(t *testing.T, args ...string) (string, string) {
	t.Helper()

	var out, errOut bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append(args, "--admin", i.url))

	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return out.String(), errOut.String()
}

// encrypted records a call Faultline could see nothing of, which is what makes
// a response-tier fault on that host inert.
func (i instance) encrypted(t *testing.T, host string) {
	t.Helper()
	i.events.Record(events.Event{
		ID:        events.NextID(),
		Timestamp: time.Now(),
		Host:      host,
		Method:    http.MethodConnect,
		Tier:      events.TierEncrypted,
	})
}

func TestRuleAddPrintsAWarningToStderr(t *testing.T) {
	i := newInstance(t)
	i.encrypted(t, "api.stripe.com")

	out, errOut := i.runSplit(t, "rule", "add", "--name", "Stripe is down",
		"--host", "api.stripe.com", "--fault", "status", "--set", "code=503")

	if !strings.Contains(errOut, "warning: ") || !strings.Contains(errOut, "api.stripe.com has only been seen encrypted") {
		t.Errorf("stderr = %q, want a warning naming the host", errOut)
	}
	if strings.Contains(out, "warning") {
		t.Errorf("stdout = %q, want the table only", out)
	}
}

func TestRuleAddJSONCarriesTheWarning(t *testing.T) {
	i := newInstance(t)
	i.encrypted(t, "api.stripe.com")

	out, errOut := i.runSplit(t, "rule", "add", "--name", "Stripe is down",
		"--host", "api.stripe.com", "--fault", "status", "--set", "code=503", "--json")

	if !strings.Contains(errOut, "warning: ") {
		t.Errorf("stderr = %q, want the warning there too", errOut)
	}
	got := decodeJSON[[]client.RuleResult](t, out)
	if len(got) != 1 {
		t.Fatalf("JSON = %q, want one rule", out)
	}
	if len(got[0].Warnings) != 1 || !strings.Contains(got[0].Warnings[0], "api.stripe.com") {
		t.Errorf("warnings = %v, want one naming the host", got[0].Warnings)
	}
}

func TestRuleEnableAndDisablePrintTheirWarning(t *testing.T) {
	i := newInstance(t)
	i.encrypted(t, "api.stripe.com")
	i.run(t, "rule", "add", "--name", "Stripe is down",
		"--host", "api.stripe.com", "--fault", "status", "--set", "code=503")

	for _, action := range []string{"disable", "enable"} {
		_, errOut := i.runSplit(t, "rule", action, "stripe-is-down")
		if !strings.Contains(errOut, "warning: ") {
			t.Errorf("%s stderr = %q, want the warning", action, errOut)
		}
	}
}

func TestRuleAddSaysNothingWhenThereIsNothingToWarnAbout(t *testing.T) {
	i := newInstance(t)

	_, errOut := i.runSplit(t, "rule", "add", "--name", "Slow",
		"--host", "httpbin.org", "--fault", "delay", "--set", "ms=1")

	if errOut != "" {
		t.Errorf("stderr = %q, want nothing", errOut)
	}
}
