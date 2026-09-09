package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/proxy/reverse"
	"github.com/josipmusa/faultline/internal/rules"
)

const statusRule = `rules:
  - id: everything-fails
    name: Everything fails
    fault:
      type: status
      code: 503
`

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestLoadConfigPicksUpTheFileInTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, DefaultConfigFile, statusRule)
	t.Chdir(dir)

	cfg, err := loadConfig("")
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg == nil || len(cfg.Rules) != 1 {
		t.Fatalf("loadConfig read %+v, want the one rule in faultline.yaml", cfg)
	}
}

func TestLoadConfigRunsWithoutAFile(t *testing.T) {
	t.Chdir(t.TempDir())

	cfg, err := loadConfig("")
	if err != nil {
		t.Fatalf("loadConfig with no file at all = %v, want no error: running in memory is normal", err)
	}
	if cfg != nil {
		t.Errorf("loadConfig invented a configuration: %+v", cfg)
	}
}

func TestLoadConfigRejectsAFileThatIsNotThere(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := loadConfig("nowhere/faultline.yaml")
	if err == nil {
		t.Fatal("loadConfig accepted a --config that names no file")
	}
	if !strings.Contains(err.Error(), "nowhere/faultline.yaml") {
		t.Errorf("err = %q, want it to name the file asked for", err)
	}
}

func TestLoadConfigReportsWhereTheFileIsWrong(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, DefaultConfigFile, "rules:\n  - id: x\n    name: x\n    fault:\n      type: delay\n      ms: 0\n")

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("loadConfig accepted a delay of zero")
	}
	if !strings.Contains(err.Error(), "fault.ms") {
		t.Errorf("err = %q, want it to name the field", err)
	}
}

func TestRoutesForMergesTheFileAndTheFlags(t *testing.T) {
	cfg := &config.Config{Routes: []reverse.Route{
		{Name: "stripe", Upstream: mustParse(t, "https://api.stripe.com"), Port: 9100},
		{Name: "orders", Upstream: mustParse(t, "http://orders.internal")},
	}}

	got, err := routesFor(cfg, []string{"orders=http://localhost:8080", "extra=https://extra.example"}, nil)
	if err != nil {
		t.Fatalf("routesFor: %v", err)
	}

	want := []reverse.Route{
		{Name: "stripe", Upstream: mustParse(t, "https://api.stripe.com"), Port: 9100},
		{Name: "orders", Upstream: mustParse(t, "http://localhost:8080"), Port: 9101},
		{Name: "extra", Upstream: mustParse(t, "https://extra.example"), Port: 9102},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d routes, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Name != w.Name || got[i].Port != w.Port || got[i].Upstream.String() != w.Upstream.String() {
			t.Errorf("route %d = %s on %d -> %s, want %s on %d -> %s",
				i, got[i].Name, got[i].Port, got[i].Upstream, w.Name, w.Port, w.Upstream)
		}
	}
}

func TestRoutesForKeepsAnAutomaticPortOffOneTheFileAsked(t *testing.T) {
	cfg := &config.Config{Routes: []reverse.Route{
		{Name: "stripe", Upstream: mustParse(t, "https://api.stripe.com"), Port: reverse.FirstPort},
	}}

	got, err := routesFor(cfg, []string{"orders=http://orders.internal"}, nil)
	if err != nil {
		t.Fatalf("routesFor: %v", err)
	}
	if got[1].Port == got[0].Port {
		t.Errorf("both routes were given port %d", got[0].Port)
	}
}

func TestBypassForIsTheFilePlusTheFlag(t *testing.T) {
	cfg := &config.Config{Bypass: []string{"*.internal"}}

	bypass, err := bypassFor(cfg, []string{"httpbin.org"})
	if err != nil {
		t.Fatalf("bypassFor: %v", err)
	}

	patterns := strings.Join(bypass.Patterns(), " ")
	for _, want := range []string{"localhost", "*.internal", "httpbin.org"} {
		if !strings.Contains(patterns, want) {
			t.Errorf("bypass %q lost %q", patterns, want)
		}
	}
}

func TestReloaderSwapsTheRulesAndSaysWhatNeedsARestart(t *testing.T) {
	store := rules.New()
	out := &syncWriter{}
	log := slog.New(slog.NewTextHandler(out, nil))

	before := &config.Config{
		Routes: []reverse.Route{{Name: "stripe", Upstream: mustParse(t, "https://api.stripe.com"), Port: 9100}},
		Bypass: []string{"*.internal"},
	}
	r := newReloader(before, store, log)

	same := *before
	same.Rules = []rules.Rule{{ID: "a", Name: "a", Enabled: true, Fault: rules.Fault{Type: "delay", Params: rules.Params{"ms": 10}}}}
	if err := r.Apply(&same); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := store.Get("a"); err != nil {
		t.Fatalf("the reloaded rule did not reach the store: %v", err)
	}
	if strings.Contains(out.String(), "restart") {
		t.Errorf("a save that only changed the rules asked for a restart:\n%s", out.String())
	}

	changed := *before
	changed.Routes = []reverse.Route{{Name: "stripe", Upstream: mustParse(t, "https://api.stripe.com"), Port: 9200}}
	changed.Bypass = []string{"*.internal", "httpbin.org"}
	if err := r.Apply(&changed); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	logged := out.String()
	if !strings.Contains(logged, "the routes changed") || !strings.Contains(logged, "the bypass list changed") {
		t.Errorf("a change that needs a restart was not reported:\n%s", logged)
	}
	if strings.Count(logged, "level=WARN") != 2 {
		t.Errorf("want one warning each for the routes and the bypass list:\n%s", logged)
	}
}

func TestReloaderReadsTheRulesBackFromTheStore(t *testing.T) {
	store := rules.New()
	r := newReloader(&config.Config{}, store, slog.New(slog.DiscardHandler))

	if err := store.Add(rules.Rule{ID: "a", Name: "a", Fault: rules.Fault{Type: "delay", Params: rules.Params{"ms": 1}}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := r.Rules(); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("Rules = %+v, want what the store holds", got)
	}
}

// TestServeReloadsARuleWhileItRuns is 5.2 end to end: a rule read from the
// file, changed in the file while Faultline runs, and then a save that does not
// parse.
func TestServeReloadsARuleWhileItRuns(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "upstream")
	}))
	defer up.Close()

	path := writeFile(t, t.TempDir(), DefaultConfigFile, statusRule)
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

	_, routeAddr := addrs(t, out, "up")
	if got := status(t, routeAddr); got != http.StatusServiceUnavailable {
		t.Fatalf("first request = %d, want the 503 the file asks for", got)
	}

	rewriteFile(t, path, strings.Replace(statusRule, "code: 503", "code: 502", 1))
	waitForStatus(t, routeAddr, http.StatusBadGateway)

	rewriteFile(t, path, "rules: [\n")
	time.Sleep(2 * pollInterval)
	if got := status(t, routeAddr); got != http.StatusBadGateway {
		t.Errorf("after a save that does not parse the rule in force gives %d, want the 502 from before", got)
	}
}

// pollInterval mirrors the watcher's, which is not exported.
const pollInterval = 500 * time.Millisecond

func rewriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("rewriting %s: %v", path, err)
	}
}

func status(t *testing.T, addr string) int {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET through the route: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func waitForStatus(t *testing.T, addr string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	last := 0
	for time.Now().Before(deadline) {
		if last = status(t, addr); last == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the edited rule never took effect: last status %d, want %d", last, want)
}
