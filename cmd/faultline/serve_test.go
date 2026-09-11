package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/proxy/reverse"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

func TestParseRoutes(t *testing.T) {
	tests := []struct {
		name   string
		routes []string
		ports  []string
		want   []reverse.Route
	}{
		{
			name:   "one route gets the first port",
			routes: []string{"stripe=https://api.stripe.com"},
			want:   []reverse.Route{{Name: "stripe", Port: reverse.FirstPort, Upstream: mustParse(t, "https://api.stripe.com")}},
		},
		{
			name:   "ports are handed out in order",
			routes: []string{"a=https://a.example.com", "b=http://b.example.com"},
			want: []reverse.Route{
				{Name: "a", Port: reverse.FirstPort, Upstream: mustParse(t, "https://a.example.com")},
				{Name: "b", Port: reverse.FirstPort + 1, Upstream: mustParse(t, "http://b.example.com")},
			},
		},
		{
			name:   "an explicit port wins",
			routes: []string{"stripe=https://api.stripe.com"},
			ports:  []string{"stripe=9200"},
			want:   []reverse.Route{{Name: "stripe", Port: 9200, Upstream: mustParse(t, "https://api.stripe.com")}},
		},
		{
			name:   "defaults skip a port claimed explicitly",
			routes: []string{"a=https://a.example.com", "b=https://b.example.com"},
			ports:  []string{"a=9100"},
			want: []reverse.Route{
				{Name: "a", Port: 9100, Upstream: mustParse(t, "https://a.example.com")},
				{Name: "b", Port: 9101, Upstream: mustParse(t, "https://b.example.com")},
			},
		},
		{
			name:   "an upstream may carry a path prefix",
			routes: []string{"api=https://example.com/v2"},
			want:   []reverse.Route{{Name: "api", Port: reverse.FirstPort, Upstream: mustParse(t, "https://example.com/v2")}},
		},
		{
			name:   "the url may contain an equals sign",
			routes: []string{"api=https://example.com/?a=b"},
			want:   []reverse.Route{{Name: "api", Port: reverse.FirstPort, Upstream: mustParse(t, "https://example.com/?a=b")}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := routesFor(nil, tt.routes, tt.ports)
			if err != nil {
				t.Fatalf("routesFor: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d routes, want %d", len(got), len(tt.want))
			}
			for i, w := range tt.want {
				if got[i].Name != w.Name || got[i].Port != w.Port || got[i].Upstream.String() != w.Upstream.String() {
					t.Errorf("route %d = %s on %d -> %s, want %s on %d -> %s",
						i, got[i].Name, got[i].Port, got[i].Upstream, w.Name, w.Port, w.Upstream)
				}
			}
		})
	}
}

func TestParseRoutesRejectsBadInput(t *testing.T) {
	tests := []struct {
		name   string
		routes []string
		ports  []string
		want   string
	}{
		{"missing equals", []string{"stripe"}, nil, "name=url"},
		{"empty name", []string{"=https://api.stripe.com"}, nil, "name"},
		{"empty url", []string{"stripe="}, nil, "url"},
		{"unparsable url", []string{"stripe=://nope"}, nil, "stripe"},
		{"relative url", []string{"stripe=api.stripe.com"}, nil, "scheme"},
		{"port without an equals", []string{"stripe=https://api.stripe.com"}, []string{"stripe"}, "name=port"},
		{"port for an unknown route", []string{"stripe=https://api.stripe.com"}, []string{"other=9100"}, "other"},
		{"port is not a number", []string{"stripe=https://api.stripe.com"}, []string{"stripe=http"}, "port"},
		{"port out of range", []string{"stripe=https://api.stripe.com"}, []string{"stripe=70000"}, "port"},
		{"duplicate route name", []string{"a=https://a.example.com", "a=https://b.example.com"}, nil, "duplicate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := routesFor(nil, tt.routes, tt.ports)
			if err == nil {
				t.Fatalf("routesFor accepted %v / %v, returning %+v", tt.routes, tt.ports, got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestParseRoutesAcceptsNoRoutes(t *testing.T) {
	got, err := routesFor(nil, nil, nil)
	if err != nil {
		t.Fatalf("routesFor: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d routes, want none: the forward proxy is reason enough to serve", len(got))
	}
}

func TestServeRejectsABadRouteBeforeBinding(t *testing.T) {
	err := runCmdErr(t, "serve", "--route", "stripe=api.stripe.com")
	if err == nil {
		t.Fatal("serve accepted a route with no scheme")
	}
	if !strings.Contains(err.Error(), "scheme") {
		t.Errorf("err = %q, want it to name the problem", err)
	}
}

func TestServeHelpDocumentsTheProxyPortFlag(t *testing.T) {
	got := runCmd(t, "serve", "--help")
	if !strings.Contains(got, "--proxy-port") {
		t.Errorf("serve help is missing --proxy-port:\n%s", got)
	}
}

func TestServeHelpDocumentsTheRouteFlags(t *testing.T) {
	got := runCmd(t, "serve", "--help")
	for _, want := range []string{"--route", "--route-port", "name=url", "name=port"} {
		if !strings.Contains(got, want) {
			t.Errorf("serve help is missing %q:\n%s", want, got)
		}
	}
}

// waitForAddr reads an address serve printed, waiting for the line to appear.
func waitForAddr(t *testing.T, out *syncWriter, prefix string) string {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for line := range strings.SplitSeq(out.String(), "\n") {
			if rest, ok := strings.CutPrefix(line, prefix); ok {
				addr, _, _ := strings.Cut(rest, " ")
				return addr
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("serve never printed a line starting %q:\n%s", prefix, out.String())
	return ""
}

// TestServeRunsTheForwardProxy is 2.1 end to end: no routes at all, a client
// that only knows HTTP_PROXY, and an event to show for it.
func TestServeRunsTheForwardProxy(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "upstream")
	}))
	defer up.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncWriter{}
	served := make(chan error, 1)
	go func() {
		// Port 0 everywhere: the test must not fight a real faultline.
		served <- serve(ctx, out, nil, 0, 0, nil, nil, nil)
	}()

	adminAddr := waitForAddr(t, out, "admin: http://")
	proxyAddr := waitForAddr(t, out, "proxy: http://")

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: proxyAddr})},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("request through the forward proxy: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "upstream" {
		t.Fatalf("got %d %q, want 200 %q", resp.StatusCode, body, "upstream")
	}

	events, err := http.Get("http://" + adminAddr + "/api/events")
	if err != nil {
		t.Fatalf("reading events: %v", err)
	}
	recorded, _ := io.ReadAll(events.Body)
	_ = events.Body.Close()
	for _, want := range []string{`"path":"/orders"`, `"tier":"plain"`} {
		if !strings.Contains(string(recorded), want) {
			t.Errorf("events are missing %s:\n%s", want, recorded)
		}
	}

	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestResolveInterception(t *testing.T) {
	withCA := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if _, err := tlsmitm.Create(dir); err != nil {
			t.Fatalf("creating CA: %v", err)
		}
		return dir
	}

	t.Run("on by default when the CA exists", func(t *testing.T) {
		ca, err := resolveInterception(withCA(t), false, false)
		if err != nil {
			t.Fatalf("resolveInterception: %v", err)
		}
		if ca == nil {
			t.Error("got no CA, want interception on")
		}
	})
	t.Run("off by default without a CA", func(t *testing.T) {
		ca, err := resolveInterception(t.TempDir(), false, false)
		if err != nil {
			t.Fatalf("resolveInterception: %v", err)
		}
		if ca != nil {
			t.Error("got a CA from an empty dir")
		}
	})
	t.Run("off when asked, even with a CA", func(t *testing.T) {
		ca, err := resolveInterception(withCA(t), true, false)
		if err != nil {
			t.Fatalf("resolveInterception: %v", err)
		}
		if ca != nil {
			t.Error("--intercept=false still intercepts")
		}
	})
	t.Run("asking for it without a CA is an error that says what to do", func(t *testing.T) {
		_, err := resolveInterception(t.TempDir(), true, true)
		if err == nil {
			t.Fatal("--intercept without a CA was accepted")
		}
		if !strings.Contains(err.Error(), "ca init") {
			t.Errorf("err = %q, want it to point at faultline ca init", err)
		}
	})
}

func TestServeHelpDocumentsTheInterceptFlag(t *testing.T) {
	got := runCmd(t, "serve", "--help")
	if !strings.Contains(got, "--intercept") {
		t.Errorf("serve help is missing --intercept:\n%s", got)
	}
}

// waitForLine returns the rest of the first line serve prints starting with
// prefix, waiting for it to appear.
func waitForLine(t *testing.T, out *syncWriter, prefix string) string {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for line := range strings.SplitSeq(out.String(), "\n") {
			if rest, ok := strings.CutPrefix(line, prefix); ok {
				return rest
			}
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("serve never printed a line starting %q:\n%s", prefix, out.String())
	return ""
}

// TestServeSaysWhetherItIntercepts checks the startup line that tells the
// operator which tier HTTPS will land in.
func TestServeSaysWhetherItIntercepts(t *testing.T) {
	ca, err := tlsmitm.Create(t.TempDir())
	if err != nil {
		t.Fatalf("creating CA: %v", err)
	}

	tests := []struct {
		name string
		ca   *tlsmitm.CA
		want string
	}{
		{"intercepting", ca, "intercepting"},
		{"passing through", nil, "ca init"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			out := &syncWriter{}
			served := make(chan error, 1)
			go func() { served <- serve(ctx, out, nil, 0, 0, nil, tt.ca, nil) }()

			line := waitForLine(t, out, "tls: ")
			if !strings.Contains(line, tt.want) {
				t.Errorf("tls line = %q, want it to mention %q", line, tt.want)
			}

			cancel()
			if err := <-served; err != nil {
				t.Fatalf("serve: %v", err)
			}
		})
	}
}

func TestServeHelpDocumentsTheBypassFlag(t *testing.T) {
	got := runCmd(t, "serve", "--help")
	for _, want := range []string{"--bypass", "*.internal", "localhost"} {
		if !strings.Contains(got, want) {
			t.Errorf("serve help is missing %q:\n%s", want, got)
		}
	}
}

func TestServeRejectsABadBypassBeforeBinding(t *testing.T) {
	err := runCmdErr(t, "serve", "--bypass", "api.*.com")
	if err == nil {
		t.Fatal("serve accepted a bypass with a wildcard in the middle")
	}
	if !strings.Contains(err.Error(), "--bypass") || !strings.Contains(err.Error(), "api.*.com") {
		t.Errorf("err = %q, want it to name the flag and the entry", err)
	}
}

// The operator's entries come on top of the defaults, never instead of them:
// Faultline must not be talked into proxying itself.
func TestBypassListStartsFromTheDefaults(t *testing.T) {
	b, err := bypassList([]string{"httpbin.org", "*.internal"})
	if err != nil {
		t.Fatalf("bypassList: %v", err)
	}
	got := strings.Join(b.Patterns(), ",")
	if want := "localhost,127.0.0.1,::1,httpbin.org,*.internal"; got != want {
		t.Errorf("patterns = %q, want %q", got, want)
	}
}

// TestServeBypassesAHostWithoutRecording is 2.5 end to end: the upstream is on
// the bypass list, so the request goes through, no event is recorded, and the
// upstreams API says why.
func TestServeBypassesAHostWithoutRecording(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "upstream")
	}))
	defer up.Close()

	bypass, err := forward.NewBypass([]string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("NewBypass: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncWriter{}
	served := make(chan error, 1)
	go func() { served <- serve(ctx, out, nil, 0, 0, nil, nil, bypass) }()

	adminAddr := waitForAddr(t, out, "admin: http://")
	proxyAddr := waitForAddr(t, out, "proxy: http://")
	if line := waitForLine(t, out, "bypass: "); !strings.Contains(line, "127.0.0.1") {
		t.Errorf("bypass line = %q, want it to list 127.0.0.1", line)
	}

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: proxyAddr})},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("request through the forward proxy: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "upstream" {
		t.Fatalf("got %d %q, want 200 %q", resp.StatusCode, body, "upstream")
	}

	if got := fetch(t, "http://"+adminAddr+"/api/events"); got != "[]\n" {
		t.Errorf("events = %s, want none for a bypassed host", got)
	}
	upstreams := fetch(t, "http://"+adminAddr+"/api/upstreams")
	for _, want := range []string{`"host":"` + up.Listener.Addr().String() + `"`, `"bypassed":true`, `"requests":1`} {
		if !strings.Contains(upstreams, want) {
			t.Errorf("upstreams are missing %s:\n%s", want, upstreams)
		}
	}

	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// fetch GETs a URL and returns its body.
func fetch(t *testing.T, target string) string {
	t.Helper()
	resp, err := http.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", target, err)
	}
	return string(body)
}

// The proxy gets its port when it starts, so the API cannot report where a
// child should send its traffic until then. This is the wiring that says it
// did: a running instance answers the question an agent asks before it wraps
// a command.
func TestServeSaysHowToAttachAChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The bypass list the command builds, not nil: its defaults are what keep
	// a child off the admin port, and reporting them is half the point here.
	bypass, err := bypassList(nil)
	if err != nil {
		t.Fatalf("bypassList: %v", err)
	}

	out := &syncWriter{}
	served := make(chan error, 1)
	go func() { served <- serve(ctx, out, nil, 0, 0, nil, nil, bypass) }()

	adminAddr := waitForAddr(t, out, "admin: http://")
	proxyAddr := waitForAddr(t, out, "proxy: http://")

	c, err := client.New("http://" + adminAddr)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	cfg, err := c.Config(ctx)
	if err != nil {
		t.Fatalf("Config: %v", err)
	}

	if cfg.ProxyURL != "http://"+proxyAddr {
		t.Errorf("config says the proxy is at %q, want %q, the port it printed", cfg.ProxyURL, "http://"+proxyAddr)
	}
	if !slices.Contains(cfg.NoProxy, "localhost") {
		t.Errorf("no_proxy is %v, want the defaults that keep a child off the admin port", cfg.NoProxy)
	}
	if cfg.Intercepting {
		t.Error("config says HTTPS is intercepted while serve was given no CA")
	}

	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
}
