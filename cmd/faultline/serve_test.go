package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/proxy/reverse"
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
			got, err := parseRoutes(tt.routes, tt.ports)
			if err != nil {
				t.Fatalf("parseRoutes: %v", err)
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
			got, err := parseRoutes(tt.routes, tt.ports)
			if err == nil {
				t.Fatalf("parseRoutes accepted %v / %v, returning %+v", tt.routes, tt.ports, got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestParseRoutesAcceptsNoRoutes(t *testing.T) {
	got, err := parseRoutes(nil, nil)
	if err != nil {
		t.Fatalf("parseRoutes: %v", err)
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
		served <- serve(ctx, out, 0, 0, nil)
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
