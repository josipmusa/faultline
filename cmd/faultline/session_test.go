package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/rules"
)

func TestNewSessionRefusesAScenarioTheFileDoesNotDeclare(t *testing.T) {
	cfg := &config.Config{
		Path:      "faultline.yaml",
		Scenarios: []rules.Scenario{{Name: "api-down"}, {Name: "api-slow"}},
	}

	_, err := newSession(cfg, "ghost", "")
	if err == nil {
		t.Fatal("newSession accepted a scenario nobody declared")
	}
	for _, want := range []string{"--scenario", "ghost", "faultline.yaml", "api-down", "api-slow"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestNewSessionSaysWhenThereIsNoFileAtAll(t *testing.T) {
	_, err := newSession(nil, "api-down", "")
	if err == nil {
		t.Fatal("newSession accepted a scenario with no configuration file")
	}
	if !strings.Contains(err.Error(), DefaultConfigFile) {
		t.Errorf("err = %q, want it to say where scenarios are declared", err)
	}
}

func TestNewSessionAcceptsADeclaredScenario(t *testing.T) {
	cfg := &config.Config{Path: "faultline.yaml", Scenarios: []rules.Scenario{{Name: "api-down"}}}

	got, err := newSession(cfg, "api-down", "")
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	if got.scenario != "api-down" {
		t.Errorf("session scenario = %q, want api-down", got.scenario)
	}
}

func TestNewSessionReadsReportAsAPathNotAFormat(t *testing.T) {
	_, err := newSession(nil, "", "json")
	if err == nil {
		t.Fatal("--report json was read as a file name")
	}
	for _, want := range []string{"--report", "format", "report.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestNewSessionRefusesAReportInADirectoryThatIsNotThere(t *testing.T) {
	_, err := newSession(nil, "", filepath.Join(t.TempDir(), "nowhere", "report.json"))
	if err == nil {
		t.Fatal("newSession accepted a report path with no directory to write it in")
	}
	if !strings.Contains(err.Error(), "--report") {
		t.Errorf("err = %q, want it to name the flag", err)
	}
}

func TestReportTableShowsTheCountsAndTheWait(t *testing.T) {
	var out bytes.Buffer
	err := writeReportTable(&out, client.ReportResult{Report: events.Report{
		Total: 3, Faulted: 2, Retries: 2, MaxRetryWaitMS: 1026, Abandoned: 1,
	}})
	if err != nil {
		t.Fatalf("writeReportTable: %v", err)
	}

	got := out.String()
	for _, want := range []string{"REQUESTS", "FAULTED", "RETRIES", "ABANDONED", "1026ms"} {
		if !strings.Contains(got, want) {
			t.Errorf("report = %q, want it to contain %q", got, want)
		}
	}
}

func TestRunReportExplainsARuleThatCouldNotFire(t *testing.T) {
	var out bytes.Buffer
	err := writeRunReport(&out, client.ReportResult{
		Report:   events.Report{Total: 3},
		Warnings: []string{"rule stripe-503: api.stripe.com has only been seen encrypted"},
	})
	if err != nil {
		t.Fatalf("writeRunReport: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "warning: rule stripe-503") {
		t.Errorf("report = %q, want it to explain why nothing was faulted", got)
	}
}

func TestRunPrintsTheReportOnStderrWithNoScenario(t *testing.T) {
	var out, errOut bytes.Buffer
	code, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, nil, nil,
		session{}, []string{"sh", "-c", "exit 0"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(errOut.String(), "REQUESTS") {
		t.Errorf("stderr = %q, want a report table; every run is a session", errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want it left to the child alone", out.String())
	}
}

func TestRunReportsEvenWhenTheChildFails(t *testing.T) {
	var out, errOut bytes.Buffer
	code, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, nil, nil,
		session{}, []string{"sh", "-c", "exit 7"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want the child's 7", code)
	}
	if !strings.Contains(errOut.String(), "REQUESTS") {
		t.Errorf("stderr = %q, want the report even though the child failed", errOut.String())
	}
}

func TestRunWritesTheReportFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")

	var out, errOut bytes.Buffer
	code, err := run(context.Background(), &out, &errOut, nil, nil, 0, 0, nil, nil, nil,
		session{report: path}, []string{"sh", "-c", "exit 0"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	raw, err := os.ReadFile(path) //nolint:gosec // the path is the test's own
	if err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	var report events.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("the report is not JSON: %v\n%s", err, raw)
	}
	if report.Total != 0 {
		t.Errorf("report = %+v, want an empty session; the child made no calls", report)
	}
	if !strings.Contains(string(raw), `"max_retry_wait_ms"`) {
		t.Errorf("report = %s, want the API's own field names", raw)
	}
	if !strings.Contains(errOut.String(), path) {
		t.Errorf("stderr = %q, want it to say where the report went", errOut.String())
	}
}

// TestRunActivatesTheScenarioForTheChildAndTurnsItOffAfter is 5.5 end to end:
// the scenario is on while the child runs, off once it has exited, and the
// report says what happened in between.
func TestRunActivatesTheScenarioForTheChildAndTurnsItOffAfter(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "upstream")
	}))
	defer up.Close()

	path := writeSessionConfig(t, up.URL)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	done := filepath.Join(t.TempDir(), "done")
	child := []string{"sh", "-c", `while [ ! -f "$0" ]; do sleep 0.01; done`, done}

	var out bytes.Buffer
	errOut := &syncWriter{}
	reportPath := filepath.Join(t.TempDir(), "report.json")

	var code int
	ran := make(chan error, 1)
	go func() {
		var runErr error
		code, runErr = run(context.Background(), &out, errOut, nil, cfg, 0, 0, cfg.Routes, nil, nil,
			session{scenario: "api-down", report: reportPath}, child)
		ran <- runErr
	}()
	// Releasing the child is how run ends, so it happens even if the test
	// gives up before it gets there.
	release := func() { _ = os.WriteFile(done, nil, 0o600) }
	defer release()

	routeAddr := waitForAddr(t, errOut, "route api: http://")
	resp, err := http.Get("http://" + routeAddr + "/orders") //nolint:noctx // a test request to a local route
	if err != nil {
		t.Fatalf("request through the route: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("route answered %d, want 503; the scenario should be on for the child", resp.StatusCode)
	}

	release()
	if err := <-ran; err != nil {
		t.Fatalf("run: %v", err)
	}

	if code != 0 {
		t.Errorf("exit code = %d, want the child's 0", code)
	}
	if !strings.Contains(errOut.String(), "scenario: api-down") {
		t.Errorf("stderr = %q, want the scenario named in the banner", errOut.String())
	}

	// A route address is an invitation to send traffic to it, so it is not
	// printed until the run is in the state it advertises. Announcing it first
	// leaves a window in which the rehearsal is not yet on and the caller gets
	// the real upstream back.
	banner := errOut.String()
	if scenario, route := strings.Index(banner, "scenario: "), strings.Index(banner, "route api: "); scenario > route {
		t.Errorf("the route was announced before the scenario was on:\n%s", banner)
	}

	report := readReport(t, reportPath)
	if report.Total != 1 || report.Faulted != 1 {
		t.Errorf("report = %+v, want the one faulted call", report)
	}

	saved, err := os.ReadFile(path) //nolint:gosec // the path is the test's own
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	if !strings.Contains(string(saved), "enabled: false") {
		t.Errorf("faultline.yaml still has the scenario's rule on:\n%s", saved)
	}
}

func readReport(t *testing.T, path string) events.Report {
	t.Helper()

	raw, err := os.ReadFile(path) //nolint:gosec // the path is the test's own
	if err != nil {
		t.Fatalf("reading the report: %v", err)
	}
	var report events.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("the report is not JSON: %v\n%s", err, raw)
	}
	return report
}

func writeSessionConfig(t *testing.T, upstream string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), DefaultConfigFile)
	body := fmt.Sprintf(`routes:
  - name: api
    upstream: %s

rules:
  - id: api-fails
    name: The api is down
    enabled: false
    fault:
      type: status
      code: 503

scenarios:
  - name: api-down
    rules:
      - api-fails
`, upstream)

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the config: %v", err)
	}
	return path
}

func TestRunRefusesAnUnknownScenarioBeforeTheChildStarts(t *testing.T) {
	path := writeSessionConfig(t, "http://127.0.0.1:1")
	marker := filepath.Join(t.TempDir(), "ran")

	err := runCmdErr(t, "run", "--config", path, "--scenario", "ghost", "--",
		"sh", "-c", "touch "+marker)
	if err == nil {
		t.Fatal("run accepted a scenario the file does not declare")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("err = %q, want it to name the scenario", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("the child ran anyway; an unknown scenario must be refused before it starts")
	}
}

func TestRunRefusesAReportFormatWhereAPathBelongs(t *testing.T) {
	err := runCmdErr(t, "run", "--report", "json", "--", "true")
	if err == nil {
		t.Fatal("run read --report json as a file name")
	}
	if !strings.Contains(err.Error(), "--report") {
		t.Errorf("err = %q, want it to name the flag", err)
	}
}

func TestRunHelpDocumentsTheSessionFlags(t *testing.T) {
	got := runCmd(t, "run", "--help")
	for _, want := range []string{"--scenario", "--report"} {
		if !strings.Contains(got, want) {
			t.Errorf("run help is missing %q:\n%s", want, got)
		}
	}
}
