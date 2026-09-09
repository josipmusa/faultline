package forward

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
)

// recordingUpstream answers every request with a fixed body and remembers what
// it saw, so a test can tell exactly what reached the far end.
type recordingUpstream struct {
	*httptest.Server
	hits atomic.Int64

	lastHost   atomic.Value // string
	lastHeader atomic.Value // http.Header
	lastPath   atomic.Value // string
}

func newUpstream(t *testing.T) *recordingUpstream {
	t.Helper()
	up := &recordingUpstream{}
	up.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.hits.Add(1)
		up.lastHost.Store(r.Host)
		up.lastHeader.Store(r.Header.Clone())
		up.lastPath.Store(r.URL.RequestURI())
		w.Header().Set("X-Upstream", "yes")
		_, _ = io.WriteString(w, "hello from upstream")
	}))
	t.Cleanup(up.Close)
	return up
}

func (u *recordingUpstream) header(t *testing.T) http.Header {
	t.Helper()
	h, ok := u.lastHeader.Load().(http.Header)
	if !ok {
		t.Fatal("upstream never saw a request")
	}
	return h
}

// quietLogger keeps proxy logging out of the test output.
func quietLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// proxyFixture is a running forward proxy over a real rule store and recorder.
type proxyFixture struct {
	proxy    *httptest.Server
	store    *rules.Store
	recorder *events.Recorder
}

func newFixture(t *testing.T) *proxyFixture {
	t.Helper()
	return newFixtureWith(t, nil)
}

// newFixtureWith is newFixture with a bypass list.
func newFixtureWith(t *testing.T, bypass *Bypass) *proxyFixture {
	t.Helper()
	store := rules.New()
	rec := events.NewRecorder(events.DefaultSize)
	t.Cleanup(rec.Close)

	transport := faults.New(nil, store, rec, events.TierPlain)
	dialer := faults.NewDialer(store, rec)
	srv := httptest.NewServer(NewServer(transport, dialer, nil, bypass, quietLogger()))
	t.Cleanup(srv.Close)

	return &proxyFixture{proxy: srv, store: store, recorder: rec}
}

// client returns an HTTP client that sends everything through the proxy.
func (f *proxyFixture) client(t *testing.T) *http.Client {
	t.Helper()
	u, err := url.Parse(f.proxy.URL)
	if err != nil {
		t.Fatalf("parsing proxy url: %v", err)
	}
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		Timeout:   5 * time.Second,
	}
}

// raw sends a request line and headers straight down a socket to the proxy and
// reports what came back, for the request forms a well behaved client will not
// produce.
func (f *proxyFixture) raw(t *testing.T, request string) (status int, body string) {
	t.Helper()
	status, _, body = f.rawFull(t, request)
	return status, body
}

// rawFull is raw plus the response headers, for tests that look at them.
func (f *proxyFixture) rawFull(t *testing.T, request string) (status int, header http.Header, body string) {
	t.Helper()
	addr := strings.TrimPrefix(f.proxy.URL, "http://")
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dialing proxy: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting deadline: %v", err)
	}
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatalf("writing request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	read, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	return resp.StatusCode, resp.Header, string(read)
}

func TestProxiesAnAbsoluteURLRequest(t *testing.T) {
	up := newUpstream(t)
	f := newFixture(t)

	resp, err := f.client(t).Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Upstream"); got != "yes" {
		t.Errorf("X-Upstream = %q, want the upstream's own response headers", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from upstream" {
		t.Errorf("body = %q, want the upstream's body", body)
	}
	if got := up.hits.Load(); got != 1 {
		t.Errorf("upstream hits = %d, want 1", got)
	}
	if got, want := up.lastPath.Load(), "/orders"; got != want {
		t.Errorf("upstream path = %v, want %q, the origin form of the absolute URL", got, want)
	}
	if got, want := up.lastHost.Load(), up.Listener.Addr().String(); got != want {
		t.Errorf("upstream Host = %v, want %q", got, want)
	}
}

func TestRecordsAPlainTierEvent(t *testing.T) {
	up := newUpstream(t)
	f := newFixture(t)

	resp, err := f.client(t).Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	_ = resp.Body.Close()

	recorded := f.recorder.Events()
	if len(recorded) != 1 {
		t.Fatalf("recorded %d events, want 1", len(recorded))
	}
	e := recorded[0]
	if e.Tier != events.TierPlain {
		t.Errorf("tier = %q, want %q", e.Tier, events.TierPlain)
	}
	if e.Host != up.Listener.Addr().String() {
		t.Errorf("host = %q, want %q", e.Host, up.Listener.Addr().String())
	}
	if e.Method != http.MethodGet || e.Path != "/orders" {
		t.Errorf("method/path = %s %s, want GET /orders", e.Method, e.Path)
	}
	if e.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", e.Status)
	}
}

func TestAppliesTheFaultPipeline(t *testing.T) {
	up := newUpstream(t)
	f := newFixture(t)

	// hostOf keeps a non-default port, so the rule matches host and port.
	if err := f.store.Add(rules.Rule{
		ID:      "orders-down",
		Enabled: true,
		Match:   rules.Match{Host: up.Listener.Addr().String()},
		Fault:   rules.Fault{Type: rules.FaultStatus, Code: http.StatusServiceUnavailable},
	}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}

	resp, err := f.client(t).Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 from the status fault", resp.StatusCode)
	}
	if got := resp.Header.Get("Faultline-Fault"); got != "orders-down" {
		t.Errorf("Faultline-Fault = %q, want the rule id", got)
	}
	if got := up.hits.Load(); got != 0 {
		t.Errorf("upstream hits = %d, want 0: a status fault answers instead of the upstream", got)
	}
}

func TestStripsHopByHopHeaders(t *testing.T) {
	up := newUpstream(t)
	f := newFixture(t)

	req, err := http.NewRequest(http.MethodGet, up.URL+"/orders", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Connection", "X-Single-Hop")
	req.Header.Set("X-Single-Hop", "gone")
	req.Header.Set("Proxy-Connection", "keep-alive")
	req.Header.Set("Proxy-Authorization", "Basic c2VjcmV0")
	req.Header.Set("X-End-To-End", "kept")

	resp, err := f.client(t).Do(req)
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	_ = resp.Body.Close()

	seen := up.header(t)
	for _, h := range []string{"X-Single-Hop", "Proxy-Connection", "Proxy-Authorization", "Keep-Alive"} {
		if got := seen.Get(h); got != "" {
			t.Errorf("upstream saw %s = %q, want it stripped", h, got)
		}
	}
	if got := seen.Get("X-End-To-End"); got != "kept" {
		t.Errorf("upstream saw X-End-To-End = %q, want it forwarded", got)
	}
}

func TestRejectsANonAbsoluteRequest(t *testing.T) {
	f := newFixture(t)

	status, body := f.raw(t, "GET /orders HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n")

	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(body, "absolute") {
		t.Errorf("body = %q, want a message explaining that a forward proxy needs an absolute URL", body)
	}
}

func TestRejectsANonHTTPScheme(t *testing.T) {
	f := newFixture(t)

	status, body := f.raw(t, "GET ftp://example.com/orders HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n")

	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(body, "ftp") {
		t.Errorf("body = %q, want a message naming the unsupported scheme", body)
	}
}

func TestAnswers502WhenTheUpstreamIsUnreachable(t *testing.T) {
	up := newUpstream(t)
	target := up.URL
	up.Close() // nothing is listening there any more

	f := newFixture(t)
	resp, err := f.client(t).Get(target + "/orders")
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

func TestStartListensAndShutdownStops(t *testing.T) {
	up := newUpstream(t)
	srv := NewServer(faults.New(nil, rules.New(), nil, events.TierPlain), nil, nil, nil, quietLogger())

	if err := srv.Start(0); err != nil {
		t.Fatalf("starting: %v", err)
	}
	addr := srv.Addr()
	if addr == "" {
		t.Fatal("Addr is empty after Start")
	}

	proxyURL := &url.URL{Scheme: "http", Host: addr}
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	_ = resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if got := srv.Addr(); got != "" {
		t.Errorf("Addr = %q after shutdown, want empty", got)
	}
	if _, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		t.Error("proxy still accepting connections after shutdown")
	}
}
