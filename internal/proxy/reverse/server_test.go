package reverse

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
)

// recordingUpstream answers every request with a description of what it saw, so
// a test can tell exactly what reached the far end.
type recordingUpstream struct {
	*httptest.Server
	hits atomic.Int64

	lastHost   atomic.Value // string
	lastHeader atomic.Value // http.Header
	lastPath   atomic.Value // string
	lastBody   atomic.Value // string
}

func newUpstream(t *testing.T, name string) *recordingUpstream {
	t.Helper()
	up := &recordingUpstream{}
	up.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		up.lastHost.Store(r.Host)
		up.lastHeader.Store(r.Header.Clone())
		up.lastPath.Store(r.URL.RequestURI())
		up.lastBody.Store(string(body))
		w.Header().Set("X-Upstream", name)
		_, _ = io.WriteString(w, "hello from "+name)
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

func (u *recordingUpstream) str(t *testing.T, v *atomic.Value) string {
	t.Helper()
	s, ok := v.Load().(string)
	if !ok {
		t.Fatal("upstream never saw a request")
	}
	return s
}

// quietLogger keeps route logging out of the test output.
func quietLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	return u
}

// startRoutes starts a server for the given routes and returns it, shut down
// when the test ends. Ports are 0 so the OS picks free ones.
func startRoutes(t *testing.T, store *rules.Store, rec *events.Recorder, upstreams map[string]string) *Server {
	t.Helper()

	var routes []Route
	for name, raw := range upstreams {
		routes = append(routes, Route{Name: name, Upstream: mustURL(t, raw)})
	}

	srv, err := NewServer(routes, faults.New(nil, store, rec, events.TierPlain), quietLogger())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return srv
}

// reply is everything a test wants to know about a response, read and closed.
type reply struct {
	status int
	header http.Header
	body   string
}

// get sends a request through the named route.
func get(t *testing.T, srv *Server, name, path string) reply {
	t.Helper()
	resp, err := http.Get("http://" + srv.Addr(name) + path)
	if err != nil {
		t.Fatalf("GET %s%s: %v", name, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body of %s%s: %v", name, path, err)
	}
	return reply{status: resp.StatusCode, header: resp.Header.Clone(), body: string(body)}
}

// refused reports whether nothing is listening on addr any more.
func refused(t *testing.T, addr string) bool {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		return true
	}
	_ = resp.Body.Close()
	return false
}

func TestRouteProxiesToItsUpstream(t *testing.T) {
	up := newUpstream(t, "stripe")
	srv := startRoutes(t, rules.New(), events.NewRecorder(10), map[string]string{"stripe": up.URL})

	got := get(t, srv, "stripe", "/v1/charges?limit=3")
	if got.status != http.StatusOK {
		t.Errorf("status = %d, want 200", got.status)
	}
	if got.body != "hello from stripe" {
		t.Errorf("body = %q, want the upstream body", got.body)
	}
	if got := got.header.Get("X-Upstream"); got != "stripe" {
		t.Errorf("X-Upstream = %q, want the upstream response headers passed back", got)
	}
	if got := up.str(t, &up.lastPath); got != "/v1/charges?limit=3" {
		t.Errorf("upstream saw %q, want the path and query preserved", got)
	}
}

func TestUpstreamSeesTheUpstreamHostHeader(t *testing.T) {
	up := newUpstream(t, "stripe")
	srv := startRoutes(t, rules.New(), events.NewRecorder(10), map[string]string{"stripe": up.URL})

	get(t, srv, "stripe", "/")

	wantHost := mustURL(t, up.URL).Host
	if got := up.str(t, &up.lastHost); got != wantHost {
		t.Errorf("upstream saw Host %q, want %q: the route address must not leak upstream", got, wantHost)
	}
}

func TestHopByHopHeadersAreStripped(t *testing.T) {
	up := newUpstream(t, "stripe")
	srv := startRoutes(t, rules.New(), events.NewRecorder(10), map[string]string{"stripe": up.URL})

	req, err := http.NewRequest(http.MethodGet, "http://"+srv.Addr("stripe")+"/", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Connection", "X-Custom-Hop")
	req.Header.Set("X-Custom-Hop", "should not survive")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Proxy-Authorization", "Basic nope")
	req.Header.Set("Te", "trailers")
	req.Header.Set("X-Kept", "yes")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	got := up.header(t)
	for _, name := range []string{"Connection", "X-Custom-Hop", "Keep-Alive", "Proxy-Authorization"} {
		if v := got.Get(name); v != "" {
			t.Errorf("upstream saw hop-by-hop header %s: %q", name, v)
		}
	}
	if got.Get("X-Kept") != "yes" {
		t.Error("an end-to-end header was stripped along with the hop-by-hop ones")
	}
	// TE is hop-by-hop, but RFC 7230 requires `TE: trailers` to be forwarded,
	// and net/http's proxy does exactly that. Pinned so a future change to the
	// handler cannot quietly break chunked trailers.
	if got.Get("Te") != "trailers" {
		t.Errorf("Te = %q, want trailers to be forwarded", got.Get("Te"))
	}
}

func TestRequestBodyAndMethodAreForwarded(t *testing.T) {
	up := newUpstream(t, "stripe")
	srv := startRoutes(t, rules.New(), events.NewRecorder(10), map[string]string{"stripe": up.URL})

	resp, err := http.Post("http://"+srv.Addr("stripe")+"/v1/charges", "application/json", strings.NewReader(`{"amount":100}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := up.str(t, &up.lastBody); got != `{"amount":100}` {
		t.Errorf("upstream saw body %q", got)
	}
	if got := up.header(t).Get("Content-Type"); got != "application/json" {
		t.Errorf("upstream saw Content-Type %q", got)
	}
}

func TestFaultsApplyThroughTheRoute(t *testing.T) {
	up := newUpstream(t, "stripe")
	host := mustURL(t, up.URL).Host

	store := rules.New()
	if err := store.Add(rules.Rule{
		ID:      "stripe-down",
		Enabled: true,
		Match:   rules.Match{Host: host},
		Fault:   rules.Fault{Type: "status", Params: rules.Params{"code": 503, "body": "down"}},
	}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}

	rec := events.NewRecorder(10)
	srv := startRoutes(t, store, rec, map[string]string{"stripe": up.URL})

	got := get(t, srv, "stripe", "/v1/charges")
	if got.status != 503 {
		t.Errorf("status = %d, want the injected 503", got.status)
	}
	if id := got.header.Get(faults.FaultHeader); id != "stripe-down" {
		t.Errorf("%s = %q, want the rule id", faults.FaultHeader, id)
	}
	if got.body != "down" {
		t.Errorf("body = %q, want the configured body", got.body)
	}
	if up.hits.Load() != 0 {
		t.Errorf("upstream was hit %d times by a short-circuited request", up.hits.Load())
	}

	recorded := rec.Events()
	if len(recorded) != 1 {
		t.Fatalf("recorded %d events, want 1", len(recorded))
	}
	if e := recorded[0]; !e.Faulted || e.RuleID != "stripe-down" || e.Host != host {
		t.Errorf("event = %+v, want it attributed to stripe-down on %s", e, host)
	}
}

func TestRoutesRunSideBySide(t *testing.T) {
	stripe := newUpstream(t, "stripe")
	github := newUpstream(t, "github")
	srv := startRoutes(t, rules.New(), events.NewRecorder(10), map[string]string{
		"stripe": stripe.URL,
		"github": github.URL,
	})

	if srv.Addr("stripe") == srv.Addr("github") {
		t.Fatal("both routes bound the same address")
	}
	if got := get(t, srv, "stripe", "/").body; got != "hello from stripe" {
		t.Errorf("stripe route returned %q", got)
	}
	if got := get(t, srv, "github", "/").body; got != "hello from github" {
		t.Errorf("github route returned %q", got)
	}
	if stripe.hits.Load() != 1 || github.hits.Load() != 1 {
		t.Errorf("hits: stripe %d, github %d, want 1 each", stripe.hits.Load(), github.hits.Load())
	}
}

func TestUnreachableUpstreamReturns502(t *testing.T) {
	// Bind a port and release it, so nothing is listening there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	dead := "http://" + ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	rec := events.NewRecorder(10)
	srv := startRoutes(t, rules.New(), rec, map[string]string{"gone": dead})

	got := get(t, srv, "gone", "/")
	if got.status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", got.status)
	}
	if id := got.header.Get(faults.FaultHeader); id != "" {
		t.Errorf("a real failure was marked as a fault: %s = %q", faults.FaultHeader, id)
	}

	recorded := rec.Events()
	if len(recorded) != 1 {
		t.Fatalf("recorded %d events, want 1", len(recorded))
	}
	if e := recorded[0]; e.Faulted || e.Status != 0 {
		t.Errorf("event = %+v, want an unfaulted event with no status", e)
	}
}

func TestShutdownStopsListening(t *testing.T) {
	up := newUpstream(t, "stripe")
	routes := []Route{{Name: "stripe", Upstream: mustURL(t, up.URL)}}
	srv, err := NewServer(routes, faults.New(nil, rules.New(), events.NewRecorder(10), events.TierPlain), nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	addr := srv.Addr("stripe")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !refused(t, addr) {
		t.Error("the route still answers after shutdown")
	}
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown: %v", err)
	}
}

func TestStartFailsWhenAPortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = taken.Close() }()

	_, port, err := net.SplitHostPort(taken.Addr().String())
	if err != nil {
		t.Fatalf("splitting %q: %v", taken.Addr(), err)
	}
	p, err := net.LookupPort("tcp", port)
	if err != nil {
		t.Fatalf("port %q: %v", port, err)
	}

	up := newUpstream(t, "stripe")
	free := newUpstream(t, "other")
	srv, err := NewServer([]Route{
		{Name: "ok", Upstream: mustURL(t, free.URL)},
		{Name: "clash", Port: p, Upstream: mustURL(t, up.URL)},
	}, faults.New(nil, rules.New(), events.NewRecorder(10), events.TierPlain), nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	err = srv.Start()
	if err == nil {
		t.Fatal("Start succeeded on a taken port")
	}
	if !strings.Contains(err.Error(), "clash") {
		t.Errorf("err = %v, want it to name the route", err)
	}
	// The route that did bind must be released again, not left listening.
	if addr := srv.Addr("ok"); addr != "" && !refused(t, addr) {
		t.Error("a route is still listening after Start failed")
	}
}

func TestNewServerRejectsBadRoutes(t *testing.T) {
	good := mustURL(t, "https://httpbin.org")

	tests := []struct {
		name   string
		routes []Route
		want   string
	}{
		{"no routes", nil, "no routes"},
		{"empty name", []Route{{Upstream: good}}, "name"},
		{"no upstream", []Route{{Name: "a"}}, "upstream"},
		{"upstream without a host", []Route{{Name: "a", Upstream: mustURL(t, "/just/a/path")}}, "upstream"},
		{"unsupported scheme", []Route{{Name: "a", Upstream: mustURL(t, "ftp://files.example.com")}}, "scheme"},
		{"port out of range", []Route{{Name: "a", Port: 70000, Upstream: good}}, "port"},
		{"negative port", []Route{{Name: "a", Port: -1, Upstream: good}}, "port"},
		{
			"duplicate names",
			[]Route{{Name: "a", Port: 9100, Upstream: good}, {Name: "a", Port: 9101, Upstream: good}},
			"duplicate",
		},
		{
			"duplicate ports",
			[]Route{{Name: "a", Port: 9100, Upstream: good}, {Name: "b", Port: 9100, Upstream: good}},
			"duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := NewServer(tt.routes, nil, nil)
			if err == nil {
				_ = srv.Shutdown(context.Background())
				t.Fatalf("NewServer accepted %+v", tt.routes)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestAddrOfAnUnknownRouteIsEmpty(t *testing.T) {
	up := newUpstream(t, "stripe")
	srv := startRoutes(t, rules.New(), events.NewRecorder(10), map[string]string{"stripe": up.URL})
	if got := srv.Addr("nope"); got != "" {
		t.Errorf("Addr(nope) = %q, want empty", got)
	}
}

func TestUpstreamPathPrefixIsKept(t *testing.T) {
	up := newUpstream(t, "api")
	srv := startRoutes(t, rules.New(), events.NewRecorder(10), map[string]string{"api": up.URL + "/v2"})

	get(t, srv, "api", "/orders")
	if got := up.str(t, &up.lastPath); got != "/v2/orders" {
		t.Errorf("upstream saw %q, want the route's base path prefixed", got)
	}
}

func TestRoutesReportTheirAddresses(t *testing.T) {
	up := newUpstream(t, "stripe")
	srv := startRoutes(t, rules.New(), events.NewRecorder(10), map[string]string{"stripe": up.URL})

	addr := srv.Addr("stripe")
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("Addr = %q, not a host:port: %v", addr, err)
	}
	if host != "127.0.0.1" {
		t.Errorf("route bound %q, want it on the loopback interface only", host)
	}
}

func TestShutdownReportsRequestsItCouldNotDrain(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	handling := make(chan struct{})
	var once sync.Once
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(handling) })
		<-release
	}))
	// Cleanup, not defer: closing the upstream waits for the handler, which
	// only returns once release above has been closed.
	t.Cleanup(slow.Close)

	srv := startRoutes(t, rules.New(), events.NewRecorder(10), map[string]string{"slow": slow.URL})
	addr := srv.Addr("slow")

	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-handling // the request is in flight and will not finish on its own

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := srv.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown = %v, want context.DeadlineExceeded while a request is still draining", err)
	}
	if !refused(t, addr) {
		t.Error("the route still accepts connections after a timed-out shutdown")
	}
}

func TestShutdownLetsAnInFlightRequestFinish(t *testing.T) {
	arrived, release := make(chan struct{}), make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(arrived)
		<-release
		_, _ = io.WriteString(w, "finished")
	}))
	t.Cleanup(up.Close)

	srv := startRoutes(t, rules.New(), events.NewRecorder(10), map[string]string{"slow": up.URL})
	addr := srv.Addr("slow")

	type result struct {
		status int
		body   string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			done <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		done <- result{status: resp.StatusCode, body: string(body), err: err}
	}()
	<-arrived

	shutdown := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdown <- srv.Shutdown(ctx)
	}()

	// The listener goes first, so nothing new gets in while the request that
	// is already running is still being served.
	for !refused(t, addr) {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)

	got := <-done
	if got.err != nil {
		t.Fatalf("the in-flight request failed: %v", got.err)
	}
	if got.status != http.StatusOK || got.body != "finished" {
		t.Errorf("in-flight reply = %d %q, want 200 %q", got.status, got.body, "finished")
	}

	select {
	case err := <-shutdown:
		if err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not return once the in-flight request finished")
	}
}
