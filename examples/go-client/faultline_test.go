package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	client "github.com/josipmusa/faultline/clients/go"
)

// adminURL is where a Faultline started by `faultline run` answers. The runner
// injects no variable naming it, so the default port is the assumption, and
// FAULTLINE_ADMIN is the way past it when a workflow moved the port.
func adminURL() string {
	if addr := os.Getenv("FAULTLINE_ADMIN"); addr != "" {
		return addr
	}
	return "http://127.0.0.1:9000"
}

// TestUnderFaultline is the example's loop against a real upstream, through a
// Faultline that was installed and started outside this process: the binary,
// the forward proxy, the CA and the environment `faultline run` injects, none
// of which an in-process test can reach.
//
// It skips unless something set HTTP_PROXY, so `make test` on a developer's
// machine is unaffected and this runs only where it was wrapped.
func TestUnderFaultline(t *testing.T) {
	if os.Getenv("HTTP_PROXY") == "" {
		t.Skip("not running under `faultline run`: HTTP_PROXY is unset")
	}

	ctx := context.Background()
	fl, err := client.New(adminURL())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	// The report counts what the session has seen, so it starts here rather
	// than at whatever the wrapping command did first.
	if err := fl.ResetSession(ctx); err != nil {
		t.Fatalf("ResetSession: %v", err)
	}

	// example.com rather than a local upstream: loopback is on the bypass list
	// by design, so a httptest server would never reach the proxy, and a real
	// HTTPS host is what proves the CA and the trust variables as well.
	const host = "example.com"
	added, err := fl.AddRule(ctx, client.Rule{
		Name:     "the upstream is down",
		Enabled:  true,
		Match:    client.Match{Host: host},
		Fault:    client.Fault{Type: "status", Params: client.Params{"code": 503}},
		Behavior: &client.Behavior{Type: "first_n", Params: client.Params{"n": 2}},
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	// A warning here means the rule was accepted and will never apply - the
	// encrypted tier, usually, which is what a missing CA looks like.
	if len(added.Warnings) > 0 {
		t.Fatalf("the rule was accepted with warnings, so it may never apply: %v", added.Warnings)
	}

	// The transport takes the proxy from the environment, which is the whole
	// point: nothing here knows Faultline's address.
	app := &http.Client{Timeout: 15 * time.Second}
	loop(ctx, app, "https://"+host+"/", time.Second, 1, retry{times: 3, first: 100 * time.Millisecond})

	report, err := fl.Report(ctx)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if report.Total != 3 || report.Faulted != 2 {
		t.Fatalf("report = %+v, want one call that took 3 attempts, 2 of them faulted", report.Report)
	}
	if report.Retries != 2 {
		t.Errorf("retries = %d, want 2: the call was repeated twice before it got through", report.Retries)
	}
}
