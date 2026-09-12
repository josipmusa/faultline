package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/admin"
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/rules"
)

// TestTheLoopIsSeenComingBackAfterAFaultIsThisExampleUnderFaultline: the
// example's own loop, a real Faultline it sends its traffic through, and a
// rule injected the way an application's test suite would inject one - through
// the client, against the running instance, with no reach into Faultline's
// insides.
func TestTheLoopIsSeenComingBackAfterAFault(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "upstream")
	}))
	defer upstream.Close()

	ctx := context.Background()
	fl := startFaultline(t)

	// The first two calls fail, the third gets through, so the loop's second
	// and third calls are both repeats of a call worth repeating.
	added, err := fl.AddRule(ctx, client.Rule{
		Name:     "the upstream is down",
		Enabled:  true,
		Match:    client.Match{Host: hostOf(t, upstream.URL)},
		Fault:    client.Fault{Type: "status", Params: client.Params{"code": 503}},
		Behavior: &client.Behavior{Type: "first_n", Params: client.Params{"n": 2}},
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	if len(added.Warnings) > 0 {
		t.Fatalf("the rule was accepted with warnings, so it may never apply: %v", added.Warnings)
	}

	loop(ctx, fl.appClient(), upstream.URL+"/get", 50*time.Millisecond, 3)

	report, err := fl.Report(ctx)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if report.Total != 3 || report.Faulted != 2 {
		t.Fatalf("report = %+v, want 3 calls of which 2 were faulted", report.Report)
	}
	if report.Retries != 2 {
		t.Errorf("retries = %d, want 2: the call after each failure repeats it", report.Retries)
	}
}

// hostOf is the host:port an event will be recorded under.
func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	return u.Host
}

// faultline is a running Faultline: the client an application's test suite
// would use, plus the proxy address the application is pointed at.
type faultline struct {
	*client.Client
	proxy *url.URL
}

// appClient is the HTTP client the example would have been handed by the
// environment `faultline run` sets up.
func (f faultline) appClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(f.proxy)},
		Timeout:   5 * time.Second,
	}
}

// startFaultline brings up the forward proxy and the admin API on ports the
// operating system chooses, so the test never fights an instance the developer
// is already running. There is no bypass list: the upstream is on localhost,
// which the default list would pass through untouched.
func startFaultline(t *testing.T) faultline {
	t.Helper()

	recorder := events.NewRecorder(events.DefaultSize)
	t.Cleanup(recorder.Close)

	store := rules.New()
	gate := faults.NewGate()
	pipeline := faults.New(nil, store, recorder, events.TierPlain, gate)

	proxy := forward.NewServer(pipeline, faults.NewDialer(store, recorder, gate), nil, nil, nil)
	if err := proxy.Start(0); err != nil {
		t.Fatalf("starting the forward proxy: %v", err)
	}
	t.Cleanup(func() { _ = proxy.Shutdown(context.Background()) })

	api := admin.NewServer(store, rules.NewScenarios(store), recorder, nil, nil, nil)
	api.Rearms(gate)
	if err := api.Start(0); err != nil {
		t.Fatalf("starting the admin server: %v", err)
	}
	t.Cleanup(func() { _ = api.Shutdown(context.Background()) })

	c, err := client.New("http://" + api.Addr())
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return faultline{Client: c, proxy: &url.URL{Scheme: "http", Host: proxy.Addr()}}
}
