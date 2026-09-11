package admin

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/rules"
)

// counter is a Persister that records how a change reached it, standing in for
// a configuration file.
type counter struct {
	changes int
	fail    error
	path    string
}

func (c *counter) Path() string { return c.path }

func (c *counter) Change(mutate func() error) error {
	c.changes++
	if c.fail != nil {
		return c.fail
	}
	return mutate()
}

func persisting(t *testing.T) (*Server, *counter) {
	t.Helper()
	s := newTestServer(t)
	c := &counter{}
	s.Persist(c)
	return s, c
}

func TestEveryRuleChangeIsPersisted(t *testing.T) {
	s, c := persisting(t)

	wantStatus(t, do(t, s, http.MethodPost, "/api/rules",
		`{"id":"slow","name":"Slow","fault":{"type":"delay","ms":10}}`), http.StatusCreated)
	wantStatus(t, do(t, s, http.MethodPut, "/api/rules/slow",
		`{"id":"slow","name":"Slower","fault":{"type":"delay","ms":20}}`), http.StatusOK)
	wantStatus(t, do(t, s, http.MethodPost, "/api/rules/slow/disable", ""), http.StatusOK)
	wantStatus(t, do(t, s, http.MethodPost, "/api/rules/slow/enable", ""), http.StatusOK)
	wantStatus(t, do(t, s, http.MethodDelete, "/api/rules/slow", ""), http.StatusNoContent)

	if c.changes != 5 {
		t.Errorf("%d of the 5 rule changes went through the config file", c.changes)
	}
}

func TestReadingRulesIsNotPersisted(t *testing.T) {
	s, c := persisting(t)
	wantStatus(t, do(t, s, http.MethodPost, "/api/rules",
		`{"id":"slow","name":"Slow","fault":{"type":"delay","ms":10}}`), http.StatusCreated)

	wantStatus(t, do(t, s, http.MethodGet, "/api/rules", ""), http.StatusOK)
	wantStatus(t, do(t, s, http.MethodGet, "/api/rules/slow", ""), http.StatusOK)

	if c.changes != 1 {
		t.Errorf("reading the rules wrote the file: %d changes", c.changes)
	}
}

func TestAChangeIsRefusedWhileTheConfigFileIsBroken(t *testing.T) {
	s, c := persisting(t)
	c.fail = &config.Error{File: "faultline.yaml", Line: 12, Path: "rules[0].fault", Message: "ms must be 1 or more"}

	w := do(t, s, http.MethodPost, "/api/rules", `{"id":"slow","name":"Slow","fault":{"type":"delay","ms":10}}`)

	wantStatus(t, w, http.StatusConflict)
	got := decodeBody[apiError](t, w)
	if !strings.Contains(got.Message, "faultline.yaml:12") {
		t.Errorf("the error does not say where the problem is: %q", got.Message)
	}
	if _, err := s.rules.Get("slow"); err == nil {
		t.Error("the rule was added even though it could not be saved")
	}
}

func TestAChangeThatCannotBeWrittenSaysSo(t *testing.T) {
	s, c := persisting(t)
	c.fail = config.ErrNotSaved

	w := do(t, s, http.MethodPost, "/api/rules", `{"id":"slow","name":"Slow","fault":{"type":"delay","ms":10}}`)

	wantStatus(t, w, http.StatusInternalServerError)
	if got := decodeBody[apiError](t, w); !strings.Contains(got.Message, "could not be written") {
		t.Errorf("error = %q, want it to say the change did not reach the file", got.Message)
	}
}

func TestAStoreErrorReachesTheClientThroughThePersister(t *testing.T) {
	s, _ := persisting(t)
	wantStatus(t, do(t, s, http.MethodPost, "/api/rules",
		`{"id":"slow","name":"Slow","fault":{"type":"delay","ms":10}}`), http.StatusCreated)

	w := do(t, s, http.MethodPost, "/api/rules", `{"id":"slow","name":"Slow","fault":{"type":"delay","ms":10}}`)

	wantError(t, w, http.StatusConflict, "id")
}

// applier is the smallest thing a config file can be applied to: the server's
// own rule store.
type applier struct {
	store     *rules.Store
	bypass    *forward.Bypass
	scenarios *rules.Scenarios
}

func (a applier) Apply(cfg *config.Config) error { a.store.Replace(cfg.Rules); return nil }
func (a applier) Rules() []rules.Rule            { return a.store.List() }
func (a applier) Bypass() []string               { return a.bypass.Configured() }

func (a applier) Scenarios() []rules.Scenario {
	if a.scenarios == nil {
		return nil
	}
	list, _ := a.scenarios.List()
	return list
}

func TestARuleAddedOverTheAPIReachesTheFile(t *testing.T) {
	const body = `# The rules of this application.
rules:
  # Stripe takes its time.
  - id: slow-stripe
    name: Stripe is slow
    fault:
      type: delay
      ms: 100
`
	path := filepath.Join(t.TempDir(), "faultline.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	s := newTestServer(t)
	s.rules.Replace(cfg.Rules)
	s.Persist(config.Watch(cfg, applier{store: s.rules}, slog.New(slog.DiscardHandler)))

	wantStatus(t, do(t, s, http.MethodPost, "/api/rules",
		`{"id":"orders-503","name":"Orders answers 503","fault":{"type":"status","code":503}}`), http.StatusCreated)
	wantStatus(t, do(t, s, http.MethodPost, "/api/rules/slow-stripe/disable", ""), http.StatusOK)

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	for _, want := range []string{"id: orders-503", "code: 503", "enabled: false", "# Stripe takes its time."} {
		if !strings.Contains(string(saved), want) {
			t.Errorf("the saved file lost %q:\n%s", want, saved)
		}
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("the saved file no longer loads: %v", err)
	}
	if len(reloaded.Rules) != 2 {
		t.Errorf("the saved file holds %d rules, want 2", len(reloaded.Rules))
	}
}

// A bypass added over the API is a change like a rule change, so it belongs in
// the file too: the host stays out of the way after a restart.
func TestABypassAddedOverTheAPIReachesTheFile(t *testing.T) {
	const body = `# The rules of this application.

# Hosts we keep our hands off.
bypass:
  # Pins its certificate.
  - telemetry.internal

rules:
  - id: slow-stripe
    name: Stripe is slow
    fault:
      type: delay
      ms: 100
`
	path := filepath.Join(t.TempDir(), "faultline.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	bypass, err := forward.NewBypass(append(forward.DefaultBypass, cfg.Bypass...))
	if err != nil {
		t.Fatalf("NewBypass: %v", err)
	}
	s := newTestServerWith(t, bypass)
	s.rules.Replace(cfg.Rules)
	s.Persist(config.Watch(cfg, applier{store: s.rules, bypass: bypass}, slog.New(slog.DiscardHandler)))

	wantStatus(t, do(t, s, http.MethodPost, "/api/bypass", `{"host":"httpbin.org"}`), http.StatusCreated)
	wantStatus(t, do(t, s, http.MethodDelete, "/api/bypass/telemetry.internal", ""), http.StatusNoContent)

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	for _, want := range []string{"- httpbin.org", "# Hosts we keep our hands off.", "# The rules of this application."} {
		if !strings.Contains(string(saved), want) {
			t.Errorf("the saved file lost %q:\n%s", want, saved)
		}
	}
	if strings.Contains(string(saved), "telemetry.internal") {
		t.Errorf("the host taken off the list is still in the file:\n%s", saved)
	}
	// The defaults are Faultline's own and are not somebody's configuration.
	if strings.Contains(string(saved), "localhost") {
		t.Errorf("the default bypass entries were written to the file:\n%s", saved)
	}
}

// A scenario created over the API is a change like a rule change, so it belongs
// in the file: it is committed with the repository and rehearsed again later.
func TestAScenarioCreatedOverTheAPIReachesTheFile(t *testing.T) {
	const body = `# The rules of this application.
rules:
  # Stripe takes its time.
  - id: slow-stripe
    name: Stripe is slow
    fault:
      type: delay
      ms: 100
`
	path := filepath.Join(t.TempDir(), "faultline.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	s := newTestServer(t)
	s.rules.Replace(cfg.Rules)
	s.scenarios.Replace(cfg.Scenarios)
	s.Persist(config.Watch(cfg, applier{store: s.rules, scenarios: s.scenarios}, slog.New(slog.DiscardHandler)))

	wantStatus(t, do(t, s, http.MethodPost, "/api/scenarios",
		`{"name":"stripe-slow","rules":["slow-stripe"]}`), http.StatusCreated)

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	for _, want := range []string{"scenarios:", "name: stripe-slow", "- slow-stripe", "# Stripe takes its time."} {
		if !strings.Contains(string(saved), want) {
			t.Errorf("the saved file lost %q:\n%s", want, saved)
		}
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("the saved file no longer loads: %v", err)
	}
	if len(reloaded.Scenarios) != 1 || reloaded.Scenarios[0].Name != "stripe-slow" {
		t.Errorf("the saved file holds %+v, want the scenario that was created", reloaded.Scenarios)
	}
}
