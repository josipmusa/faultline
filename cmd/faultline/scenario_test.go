package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/proxy/reverse"
	"github.com/josipmusa/faultline/internal/rules"
)

const twoScenarioFile = `rules:
  - id: fails
    name: Everything fails
    enabled: false
    fault:
      type: status
      code: 503
  - id: gateway
    name: The gateway is gone
    enabled: false
    fault:
      type: status
      code: 502

scenarios:
  - name: down
    rules:
      - fails
  - name: gateway-down
    rules:
      - gateway
`

func TestReloaderReseedsTheScenarios(t *testing.T) {
	store := rules.New()
	scenarios := rules.NewScenarios(store)
	out := &syncWriter{}
	r := newReloader(&config.Config{}, store, scenarios, slog.New(slog.NewTextHandler(out, nil)))

	first := &config.Config{Scenarios: []rules.Scenario{{Name: "down", Rules: []string{"fails"}}}}
	if err := r.Apply(first); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if list, _ := scenarios.List(); len(list) != 1 || list[0].Name != "down" {
		t.Fatalf("scenarios = %+v, want the one the file declares", list)
	}
}

func TestReloaderSaysWhenTheActiveScenarioLeavesTheFile(t *testing.T) {
	store := rules.New()
	scenarios := rules.NewScenarios(store)
	scenarios.Replace([]rules.Scenario{{Name: "down"}})
	if _, err := scenarios.Activate("down"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	out := &syncWriter{}
	r := newReloader(&config.Config{}, store, scenarios, slog.New(slog.NewTextHandler(out, nil)))
	if err := r.Apply(&config.Config{Scenarios: []rules.Scenario{{Name: "other"}}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, active := scenarios.List(); active != "" {
		t.Errorf("active = %q, want nothing active once the file stopped declaring it", active)
	}
	if logged := out.String(); !strings.Contains(logged, "down") || !strings.Contains(logged, "level=WARN") {
		t.Errorf("a scenario that went away while it was on was not reported:\n%s", logged)
	}
}

// TestServeActivatesAScenarioWhileItRuns is 5.3 end to end: two scenarios in
// the file, one activated over the API, the other activated after it, and the
// enabled flags written back to the file both times.
func TestServeActivatesAScenarioWhileItRuns(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "upstream")
	}))
	defer up.Close()

	path := writeFile(t, t.TempDir(), DefaultConfigFile, twoScenarioFile)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	out := &syncWriter{}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- serve(ctx, out, cfg, 0, 0, []reverse.Route{{Name: "up", Upstream: mustParse(t, up.URL)}}, nil, nil)
	}()
	defer func() {
		cancel()
		<-served
	}()

	adminAddr, routeAddr := addrs(t, out, "up")

	if got := status(t, routeAddr); got != http.StatusOK {
		t.Fatalf("before any scenario the route gives %d, want the upstream's 200", got)
	}

	post(t, adminAddr, "/api/scenarios/down/activate")
	if got := status(t, routeAddr); got != http.StatusServiceUnavailable {
		t.Fatalf("with down active the route gives %d, want 503", got)
	}

	post(t, adminAddr, "/api/scenarios/gateway-down/activate")
	if got := status(t, routeAddr); got != http.StatusBadGateway {
		t.Fatalf("with gateway-down active the route gives %d, want 502", got)
	}

	written, err := os.ReadFile(path) // #nosec G304 -- the test wrote this path
	if err != nil {
		t.Fatalf("reading the file back: %v", err)
	}
	saved, err := config.Parse(path, written)
	if err != nil {
		t.Fatalf("the file Faultline wrote does not load: %v", err)
	}
	for _, want := range []struct {
		id string
		on bool
	}{{"fails", false}, {"gateway", true}} {
		i := indexOfRule(saved.Rules, want.id)
		if i < 0 {
			t.Fatalf("the file lost rule %q:\n%s", want.id, written)
		}
		if saved.Rules[i].Enabled != want.on {
			t.Errorf("the file says %q is enabled=%v, want %v:\n%s", want.id, saved.Rules[i].Enabled, want.on, written)
		}
	}

	post(t, adminAddr, "/api/scenarios/gateway-down/deactivate")
	if got := status(t, routeAddr); got != http.StatusOK {
		t.Errorf("after deactivating, the route gives %d, want the upstream's 200 back", got)
	}
}

func indexOfRule(rs []rules.Rule, id string) int {
	for i, r := range rs {
		if r.ID == id {
			return i
		}
	}
	return -1
}

func post(t *testing.T, adminAddr, path string) {
	t.Helper()
	resp, err := http.Post("http://"+adminAddr+path, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s = %d: %s", path, resp.StatusCode, body)
	}
}
