package main

import (
	"strings"
	"testing"

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
		{"no routes at all", nil, nil, "--route"},
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

func TestServeRequiresARoute(t *testing.T) {
	err := runCmdErr(t, "serve")
	if err == nil {
		t.Fatal("serve started with no routes")
	}
	if !strings.Contains(err.Error(), "--route") {
		t.Errorf("err = %q, want it to point at --route", err)
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

func TestServeHelpDocumentsTheRouteFlags(t *testing.T) {
	got := runCmd(t, "serve", "--help")
	for _, want := range []string{"--route", "--route-port", "name=url", "name=port"} {
		if !strings.Contains(got, want) {
			t.Errorf("serve help is missing %q:\n%s", want, got)
		}
	}
}
