package faults

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/josipmusa/faultline/internal/capture"
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/rules"
)

// captured runs one request through a transport capturing into caps and
// returns the capture filed against the event it recorded.
func captured(t *testing.T, tr *Transport, rec *events.Recorder, caps *capture.Store, req *http.Request) capture.Capture {
	t.Helper()

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	readBody(t, resp) // the response body is captured as it streams, so drain it

	id := onlyEvent(t, rec).ID
	c, ok := caps.Get(id)
	if !ok {
		t.Fatalf("no capture filed for event %s", id)
	}
	return c
}

func TestCaptureHoldsBothSidesOfARealExchange(t *testing.T) {
	up, _ := upstream(t)
	rec, caps := events.NewRecorder(10), capture.NewStore(0)
	tr := New(http.DefaultTransport, rules.New(), rec, events.TierPlain, nil)
	tr.CaptureTo(caps)

	req := mustRequest(context.Background(), t, http.MethodPost, up.URL+"/orders", "who=me")
	req.Header.Set("X-Test", "1")

	c := captured(t, tr, rec, caps, req)

	if got := c.Request.Headers.Get("X-Test"); got != "1" {
		t.Errorf("captured request header X-Test = %q, want 1", got)
	}
	if got := string(c.Request.Body); got != "who=me" {
		t.Errorf("captured request body = %q, want %q", got, "who=me")
	}
	if got := c.Response.Headers.Get("X-Upstream"); got != "yes" {
		t.Errorf("captured response header X-Upstream = %q, want yes", got)
	}
	if got := string(c.Response.Body); got != "real body" {
		t.Errorf("captured response body = %q, want the upstream body", got)
	}
}

// The capture is what the client received, not what the upstream sent, so a
// fault that rewrites a body shows its own work.
func TestCaptureHoldsTheFaultedResponseNotTheUpstreamOne(t *testing.T) {
	up, hits := upstream(t)
	rec, caps := events.NewRecorder(10), capture.NewStore(0)
	tr := New(http.DefaultTransport, storeWith(t, statusRule("broken", 503, "upstream is down")), rec, events.TierPlain, nil)
	tr.CaptureTo(caps)

	c := captured(t, tr, rec, caps, mustRequest(context.Background(), t, http.MethodGet, up.URL+"/orders", ""))

	if hits.Load() != 0 {
		t.Fatalf("upstream was hit %d times, want 0: the status fault answers by itself", hits.Load())
	}
	if got := string(c.Response.Body); got != "upstream is down" {
		t.Errorf("captured response body = %q, want the fault's own body", got)
	}
}

func TestCaptureIsFiledAgainstTheEventId(t *testing.T) {
	up, _ := upstream(t)
	rec, caps := events.NewRecorder(10), capture.NewStore(0)
	tr := New(http.DefaultTransport, rules.New(), rec, events.TierPlain, nil)
	tr.CaptureTo(caps)

	c := captured(t, tr, rec, caps, mustRequest(context.Background(), t, http.MethodGet, up.URL+"/orders", ""))

	if c.EventID != onlyEvent(t, rec).ID {
		t.Errorf("capture event id = %q, want the recorded event's %q", c.EventID, onlyEvent(t, rec).ID)
	}
}

// Headers are filed at the response, before the body has finished streaming,
// so an exchange still in flight can already be opened and read.
func TestCaptureHoldsHeadersBeforeTheBodyIsDrained(t *testing.T) {
	up, _ := upstream(t)
	rec, caps := events.NewRecorder(10), capture.NewStore(0)
	tr := New(http.DefaultTransport, rules.New(), rec, events.TierPlain, nil)
	tr.CaptureTo(caps)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/orders", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // the test drains it below

	c, ok := caps.Get(onlyEvent(t, rec).ID)
	if !ok {
		t.Fatal("no capture filed at the response headers")
	}
	if c.Response.Headers.Get("X-Upstream") != "yes" {
		t.Error("response headers were not captured at the response")
	}
	if len(c.Response.Body) != 0 {
		t.Errorf("captured response body = %q before the body was read, want nothing yet", c.Response.Body)
	}
}

func TestATransportWithNoCaptureStoreStillWorks(t *testing.T) {
	up, _ := upstream(t)
	rec := events.NewRecorder(10)
	tr := New(http.DefaultTransport, rules.New(), rec, events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/orders", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := readBody(t, resp); got != "real body" {
		t.Errorf("body = %q, want the upstream body", got)
	}
}

// An upstream that never answers still leaves the request side worth reading:
// what was sent is exactly what the debugging question is about.
func TestCaptureOfAFailedRequestHoldsTheRequestSide(t *testing.T) {
	rec, caps := events.NewRecorder(10), capture.NewStore(0)
	tr := New(http.DefaultTransport, rules.New(), rec, events.TierPlain, nil)
	tr.CaptureTo(caps)

	// Port 1 on loopback: nothing listens there, so the dial fails.
	req := mustRequest(context.Background(), t, http.MethodPost, "http://127.0.0.1:1/orders", "who=me")
	req.Header.Set("X-Test", "1")
	resp, err := tr.RoundTrip(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("RoundTrip succeeded, want the dial to an unreachable upstream to fail")
	}

	c, ok := caps.Get(onlyEvent(t, rec).ID)
	if !ok {
		t.Fatal("no capture filed for a request that never got a response")
	}
	if c.Request.Headers.Get("X-Test") != "1" {
		t.Error("the request side was not captured when the request failed")
	}
	if len(c.Response.Headers) != 0 {
		t.Errorf("captured response headers %v, want none: there was no response", c.Response.Headers)
	}
}

// The spec's own example: a truncate fault must capture the four bytes the
// client received, not the nine the upstream sent.
func TestCaptureHoldsATruncatedBodyAtItsCutLength(t *testing.T) {
	up, _ := upstream(t)
	rec, caps := events.NewRecorder(10), capture.NewStore(0)
	tr := New(http.DefaultTransport, storeWith(t, rules.Rule{
		ID: "cut", Name: "cut short", Enabled: true,
		Fault: rules.Fault{Type: "truncate", Params: rules.Params{"after_bytes": 4}},
	}), rec, events.TierPlain, nil)
	tr.CaptureTo(caps)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/orders", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	// The cut body is a broken transfer, so reading it errors; the capture is
	// completed on close either way.
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	c, ok := caps.Get(onlyEvent(t, rec).ID)
	if !ok {
		t.Fatal("no capture filed")
	}
	if got := string(c.Response.Body); got != "real" {
		t.Errorf("captured response body = %q, want the cut %q", got, "real")
	}
}
