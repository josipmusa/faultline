package main

import (
	"strings"
	"testing"

	client "github.com/josipmusa/faultline/clients/go"
)

func TestSessionReportPrintsTheCounts(t *testing.T) {
	i := newInstance(t)
	i.record(t, "1", "httpbin.org", true)
	i.record(t, "2", "httpbin.org", false)

	out := i.run(t, "session", "report")

	for _, column := range reportColumns {
		if !strings.Contains(out, column) {
			t.Errorf("the table has no %s column:\n%s", column, out)
		}
	}
	// Two requests, one of them faulted on purpose.
	wantLine(t, out, "2", "1")
}

func TestSessionReportJSONIsTheAPIsOwnShape(t *testing.T) {
	i := newInstance(t)
	i.record(t, "1", "httpbin.org", true)

	got := decodeJSON[client.ReportResult](t, i.run(t, "session", "report", "--json"))

	if got.Total != 1 || got.Faulted != 1 {
		t.Fatalf("session report --json = %+v, want one request, one faulted", got)
	}
}

// TestSessionReportPutsAWarningOnStderr is the trap this command exists to
// spring: a faulted count of zero beside a warning means the fault never
// applied, not that the application coped with it. The warning has to be
// visible to a person without landing in a piped payload.
func TestSessionReportPutsAWarningOnStderr(t *testing.T) {
	i := newInstance(t)
	i.encrypted(t, "api.stripe.com")
	i.run(t, "rule", "add", "--name", "Stripe is down",
		"--host", "api.stripe.com", "--fault", "status", "--set", "code=503")

	out, errOut := i.runSplit(t, "session", "report")

	if !strings.Contains(errOut, "warning: ") || !strings.Contains(errOut, "api.stripe.com") {
		t.Errorf("stderr = %q, want a warning naming the host", errOut)
	}
	if strings.Contains(out, "warning") {
		t.Errorf("stdout = %q, want the warning kept off it", out)
	}
}

func TestSessionReportJSONCarriesTheWarningAndStderrStillHasIt(t *testing.T) {
	i := newInstance(t)
	i.encrypted(t, "api.stripe.com")
	i.run(t, "rule", "add", "--name", "Stripe is down",
		"--host", "api.stripe.com", "--fault", "status", "--set", "code=503")

	out, errOut := i.runSplit(t, "session", "report", "--json")

	if !strings.Contains(errOut, "warning: ") {
		t.Errorf("stderr = %q, want the warning there too", errOut)
	}
	got := decodeJSON[client.ReportResult](t, out)
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "api.stripe.com") {
		t.Errorf("warnings = %v, want one naming the host", got.Warnings)
	}
}

func TestSessionResetSaysWhatItCleared(t *testing.T) {
	i := newInstance(t)
	i.record(t, "1", "httpbin.org", true)

	if out := i.run(t, "session", "reset"); !strings.Contains(out, "re-armed") {
		t.Errorf("session reset printed %q, want it to say the rules were re-armed", out)
	}
	if got := decodeJSON[client.ReportResult](t, i.run(t, "session", "report", "--json")); got.Total != 0 {
		t.Errorf("report after a reset = %+v, want nothing observed", got)
	}
}

func TestSessionResetJSONIsReadableByAScript(t *testing.T) {
	i := newInstance(t)

	got := decodeJSON[sessionResetResult](t, i.run(t, "session", "reset", "--json"))

	if !got.Reset {
		t.Errorf("session reset --json = %+v, want reset true", got)
	}
}
