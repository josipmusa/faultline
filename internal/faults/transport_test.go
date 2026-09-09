package faults

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/rules"
)

// upstream starts a server that counts its hits and always answers 418 with a
// known body, so a synthetic response is impossible to confuse with a real one.
func upstream(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "real body")
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func mustRequest(ctx context.Context, t *testing.T, method, url string, body string) *http.Request {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	return req
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close() //nolint:errcheck // reading a test body
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return string(b)
}

// storeWith returns a store holding the rules in the order given.
func storeWith(t *testing.T, rs ...rules.Rule) *rules.Store {
	t.Helper()
	s := rules.New()
	for _, r := range rs {
		if err := s.Add(r); err != nil {
			t.Fatalf("adding rule %q: %v", r.ID, err)
		}
	}
	return s
}

// onlyEvent asserts exactly one event was recorded and returns it.
func onlyEvent(t *testing.T, rec *events.Recorder) events.Event {
	t.Helper()
	got := rec.Events()
	if len(got) != 1 {
		t.Fatalf("recorded %d events, want 1: %+v", len(got), got)
	}
	return got[0]
}

func delayRule(id string, ms int) rules.Rule {
	return rules.Rule{
		ID:      id,
		Name:    "slow",
		Enabled: true,
		Fault:   rules.Fault{Type: "delay", Params: rules.Params{"ms": ms}},
	}
}

func statusRule(id string, code int, body string) rules.Rule {
	return rules.Rule{
		ID:      id,
		Name:    "broken",
		Enabled: true,
		Fault:   rules.Fault{Type: "status", Params: rules.Params{"code": code, "body": body}},
	}
}

func TestNoRulePassesTrafficUnchanged(t *testing.T) {
	up, hits := upstream(t)
	rec := events.NewRecorder(10)
	tr := New(http.DefaultTransport, rules.New(), rec, events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/orders/1", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := readBody(t, resp); got != "real body" {
		t.Errorf("body = %q, want the upstream body", got)
	}
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTeapot)
	}
	if resp.Header.Get("X-Upstream") != "yes" {
		t.Error("upstream response headers were not passed through")
	}
	if resp.Header.Get(FaultHeader) != "" {
		t.Errorf("unfaulted response carries %s", FaultHeader)
	}
	if hits.Load() != 1 {
		t.Errorf("upstream hit %d times, want 1", hits.Load())
	}

	e := onlyEvent(t, rec)
	if e.Faulted || e.RuleID != "" {
		t.Errorf("event reports a fault: %+v", e)
	}
	if e.Status != http.StatusTeapot {
		t.Errorf("event status = %d, want %d", e.Status, http.StatusTeapot)
	}
	if e.Method != http.MethodGet || e.Path != "/orders/1" {
		t.Errorf("event method/path = %s %s, want GET /orders/1", e.Method, e.Path)
	}
	if e.Tier != events.TierPlain {
		t.Errorf("event tier = %q, want %q", e.Tier, events.TierPlain)
	}
	if e.ID == "" {
		t.Error("event has no id")
	}
	if e.Timestamp.IsZero() {
		t.Error("event has no timestamp")
	}
}

func TestDelayRuleAddsAtLeastTheConfiguredDelay(t *testing.T) {
	const delay = 60 * time.Millisecond
	up, hits := upstream(t)
	rec := events.NewRecorder(10)
	tr := New(http.DefaultTransport, storeWith(t, delayRule("slow", 60)), rec, events.TierPlain, nil)

	start := time.Now()
	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/", ""))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()

	if elapsed < delay {
		t.Errorf("took %v, want at least %v", elapsed, delay)
	}
	if hits.Load() != 1 {
		t.Errorf("upstream hit %d times, want 1: a delay must still reach upstream", hits.Load())
	}
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want the real upstream status", resp.StatusCode)
	}

	e := onlyEvent(t, rec)
	if !e.Faulted || e.RuleID != "slow" {
		t.Errorf("event = %+v, want faulted by rule slow", e)
	}
	if e.DurationMS < delay.Milliseconds() {
		t.Errorf("event duration = %dms, want at least %dms", e.DurationMS, delay.Milliseconds())
	}
}

func TestDelayIsCancelledPromptlyWithTheRequestContext(t *testing.T) {
	up, hits := upstream(t)
	rec := events.NewRecorder(10)
	tr := New(http.DefaultTransport, storeWith(t, delayRule("very-slow", 30_000)), rec, events.TierPlain, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	resp, err := tr.RoundTrip(mustRequest(ctx, t, http.MethodGet, up.URL+"/", ""))
	elapsed := time.Since(start)
	cancel()

	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("RoundTrip returned no error, want context.Canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("cancellation took %v, the delay was not interrupted", elapsed)
	}
	if hits.Load() != 0 {
		t.Errorf("upstream hit %d times, want 0: a cancelled request must not reach upstream", hits.Load())
	}

	e := onlyEvent(t, rec)
	if !e.Faulted || e.RuleID != "very-slow" {
		t.Errorf("event = %+v, want faulted by rule very-slow", e)
	}
	if e.Status != 0 {
		t.Errorf("event status = %d, want 0: the request never got one", e.Status)
	}
}

func TestStatusRuleShortCircuitsWithoutHittingUpstream(t *testing.T) {
	up, hits := upstream(t)
	rec := events.NewRecorder(10)
	tr := New(http.DefaultTransport, storeWith(t, statusRule("gateway-down", 503, "upstream is down")), rec, events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/charges", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != 503 {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	if got := resp.Header.Get(FaultHeader); got != "gateway-down" {
		t.Errorf("%s = %q, want the rule id", FaultHeader, got)
	}
	if got := readBody(t, resp); got != "upstream is down" {
		t.Errorf("body = %q, want the configured body", got)
	}
	if resp.Header.Get("X-Upstream") != "" {
		t.Error("synthetic response carries upstream headers")
	}
	if hits.Load() != 0 {
		t.Errorf("upstream hit %d times, want 0", hits.Load())
	}

	e := onlyEvent(t, rec)
	if !e.Faulted || e.RuleID != "gateway-down" {
		t.Errorf("event = %+v, want faulted by rule gateway-down", e)
	}
	if e.Status != 503 {
		t.Errorf("event status = %d, want 503", e.Status)
	}
}

func TestStatusRuleWithoutABodyReturnsAnEmptyBody(t *testing.T) {
	up, _ := upstream(t)
	tr := New(http.DefaultTransport, storeWith(t, statusRule("empty", 500, "")), events.NewRecorder(10), events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := readBody(t, resp); got != "" {
		t.Errorf("body = %q, want empty", got)
	}
	if resp.ContentLength != 0 {
		t.Errorf("ContentLength = %d, want 0", resp.ContentLength)
	}
	if got := resp.Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type = %q, want none for an empty body", got)
	}
}

func TestDisabledRuleIsIgnored(t *testing.T) {
	up, hits := upstream(t)
	rec := events.NewRecorder(10)
	r := statusRule("off", 503, "")
	r.Enabled = false
	tr := New(http.DefaultTransport, storeWith(t, r), rec, events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot || hits.Load() != 1 {
		t.Errorf("status %d after %d upstream hits, want the real response", resp.StatusCode, hits.Load())
	}
	if e := onlyEvent(t, rec); e.Faulted {
		t.Errorf("event = %+v, want unfaulted", e)
	}
}

func TestNonMatchingRuleIsIgnored(t *testing.T) {
	up, hits := upstream(t)
	r := statusRule("stripe-only", 503, "")
	r.Match = rules.Match{Host: "api.stripe.com"}
	tr := New(http.DefaultTransport, storeWith(t, r), events.NewRecorder(10), events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot || hits.Load() != 1 {
		t.Errorf("status %d after %d upstream hits, want the real response", resp.StatusCode, hits.Load())
	}
}

func TestFirstMatchingRuleWins(t *testing.T) {
	up, _ := upstream(t)
	rec := events.NewRecorder(10)
	tr := New(http.DefaultTransport,
		storeWith(t, statusRule("first", 503, ""), statusRule("second", 500, "")),
		rec, events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("status = %d, want 503 from the first rule in the store", resp.StatusCode)
	}
	if e := onlyEvent(t, rec); e.RuleID != "first" {
		t.Errorf("event rule = %q, want first", e.RuleID)
	}
}

func TestRuleMatchesOnPathAndMethod(t *testing.T) {
	up, hits := upstream(t)
	r := statusRule("post-orders", 503, "")
	r.Match = rules.Match{Method: "POST", Path: "/orders/*"}
	tr := New(http.DefaultTransport, storeWith(t, r), events.NewRecorder(10), events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/orders/1", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("GET was faulted by a POST-only rule")
	}

	resp, err = tr.RoundTrip(mustRequest(context.Background(), t, http.MethodPost, up.URL+"/orders/1", "{}"))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("status = %d, want the POST to be faulted", resp.StatusCode)
	}
	if hits.Load() != 1 {
		t.Errorf("upstream hit %d times, want 1 (the unfaulted GET only)", hits.Load())
	}
}

func TestRuleMatchesOnHeader(t *testing.T) {
	up, _ := upstream(t)
	r := statusRule("tagged", 503, "")
	r.Match = rules.Match{Header: map[string]string{"X-Test": "1"}}
	tr := New(http.DefaultTransport, storeWith(t, r), events.NewRecorder(10), events.TierPlain, nil)

	req := mustRequest(context.Background(), t, http.MethodGet, up.URL+"/", "")
	req.Header.Set("X-Test", "1")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// errTransport stands in for an unreachable upstream.
type errTransport struct{ err error }

func (e errTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

func TestUpstreamErrorIsRecordedAndReturned(t *testing.T) {
	boom := errors.New("dial tcp: connection refused")
	rec := events.NewRecorder(10)
	tr := New(errTransport{err: boom}, rules.New(), rec, events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, "http://api.example.com/x", ""))
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("RoundTrip returned no error for an unreachable upstream")
	}
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to wrap the transport error", err)
	}
	if resp != nil {
		t.Error("a response was returned for an unreachable upstream: faultline never fabricates one")
	}

	e := onlyEvent(t, rec)
	if e.Status != 0 {
		t.Errorf("event status = %d, want 0", e.Status)
	}
	if e.Faulted {
		t.Error("an upstream failure was recorded as a fault")
	}
	if e.Host != "api.example.com" {
		t.Errorf("event host = %q, want api.example.com", e.Host)
	}
}

func TestUnknownFaultTypePassesTrafficThrough(t *testing.T) {
	up, hits := upstream(t)
	rec := events.NewRecorder(10)
	r := rules.Rule{ID: "weird", Enabled: true, Fault: rules.Fault{Type: "teleport"}}
	tr := New(http.DefaultTransport, storeWith(t, r), rec, events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot || hits.Load() != 1 {
		t.Errorf("status %d after %d upstream hits, want the real response", resp.StatusCode, hits.Load())
	}
	if e := onlyEvent(t, rec); e.Faulted || e.RuleID != "" {
		t.Errorf("event = %+v, want unfaulted: no fault was actually applied", e)
	}
}

func TestEventCarriesTheConfiguredTier(t *testing.T) {
	up, _ := upstream(t)
	rec := events.NewRecorder(10)
	tr := New(http.DefaultTransport, rules.New(), rec, events.TierIntercepted, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()
	if e := onlyEvent(t, rec); e.Tier != events.TierIntercepted {
		t.Errorf("event tier = %q, want %q", e.Tier, events.TierIntercepted)
	}
}

func TestEventCountsKnownBodyLengths(t *testing.T) {
	up, _ := upstream(t)
	rec := events.NewRecorder(10)
	tr := New(http.DefaultTransport, rules.New(), rec, events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodPost, up.URL+"/", "hello"))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()

	e := onlyEvent(t, rec)
	if e.BytesIn != 5 {
		t.Errorf("bytes_in = %d, want 5", e.BytesIn)
	}
	if e.BytesOut != int64(len("real body")) {
		t.Errorf("bytes_out = %d, want %d", e.BytesOut, len("real body"))
	}
}

func TestUnknownBodyLengthsCountAsZero(t *testing.T) {
	chunked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush() // forces a chunked response with no Content-Length
		_, _ = io.WriteString(w, "streamed")
	}))
	defer chunked.Close()

	rec := events.NewRecorder(10)
	tr := New(http.DefaultTransport, rules.New(), rec, events.TierPlain, nil)
	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, chunked.URL+"/", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()

	if e := onlyEvent(t, rec); e.BytesOut != 0 {
		t.Errorf("bytes_out = %d, want 0 when the length is unknown", e.BytesOut)
	}
}

func TestNewFillsInDefaults(t *testing.T) {
	up, _ := upstream(t)
	tr := New(nil, nil, nil, "", nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/", ""))
	if err != nil {
		t.Fatalf("RoundTrip with a nil base: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want the real response", resp.StatusCode)
	}
	if tr.tier != events.TierPlain {
		t.Errorf("tier = %q, want %q by default", tr.tier, events.TierPlain)
	}
}

func TestHostOf(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		reqHost string
		want    string
	}{
		{"https default port is dropped", "https://api.stripe.com:443/v1", "", "api.stripe.com"},
		{"http default port is dropped", "http://api.stripe.com:80/v1", "", "api.stripe.com"},
		{"no port", "https://api.stripe.com/v1", "", "api.stripe.com"},
		{"other ports are kept", "http://localhost:8080/v1", "", "localhost:8080"},
		{"falls back to the Host header", "/v1/charges", "api.stripe.com", "api.stripe.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{}
			u, err := url.Parse(tt.url)
			if err != nil {
				t.Fatalf("parsing %q: %v", tt.url, err)
			}
			req.URL = u
			req.Host = tt.reqHost
			if got := hostOf(req); got != tt.want {
				t.Errorf("hostOf(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestRoundTripIsSafeForConcurrentUse(t *testing.T) {
	up, _ := upstream(t)
	store := storeWith(t, delayRule("slow", 1))
	rec := events.NewRecorder(200)
	tr := New(http.DefaultTransport, store, rec, events.TierPlain, nil)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 5 {
				resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/", ""))
				if err != nil {
					t.Errorf("RoundTrip: %v", err)
					return
				}
				_ = resp.Body.Close()
			}
		})
	}
	// Mutate the store while requests are in flight.
	wg.Go(func() {
		for range 50 {
			_ = store.Disable("slow")
			_ = store.Enable("slow")
		}
	})
	wg.Wait()

	if got := len(rec.Events()); got != 100 {
		t.Errorf("recorded %d events, want 100", got)
	}
}

func refuseRule(id string) rules.Rule {
	return rules.Rule{
		ID:      id,
		Name:    "down",
		Enabled: true,
		Fault:   rules.Fault{Type: "refuse"},
	}
}

func TestRefuseFaultAnswersABadGatewayWithoutCallingUpstream(t *testing.T) {
	up, hits := upstream(t)
	rec := events.NewRecorder(10)
	tr := New(http.DefaultTransport, storeWith(t, refuseRule("stripe-down")), rec, events.TierPlain, nil)

	resp, err := tr.RoundTrip(mustRequest(context.Background(), t, http.MethodGet, up.URL+"/orders/1", ""))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502: a refused connection is an upstream that cannot be reached", resp.StatusCode)
	}
	if got := resp.Header.Get(FaultHeader); got != "stripe-down" {
		t.Errorf("%s = %q, want the rule id on every synthetic response", FaultHeader, got)
	}
	if body := readBody(t, resp); !strings.Contains(body, "refused") {
		t.Errorf("body = %q, want it to say the connection was refused", body)
	}
	if hits.Load() != 0 {
		t.Errorf("upstream hit %d times, want 0", hits.Load())
	}

	e := onlyEvent(t, rec)
	if !e.Faulted || e.RuleID != "stripe-down" {
		t.Errorf("event does not report the refuse rule: %+v", e)
	}
	if e.Status != http.StatusBadGateway {
		t.Errorf("event status = %d, want 502", e.Status)
	}
}
