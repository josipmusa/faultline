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
)

// TestServeResetSessionRearmsTheRunningPipeline proves the wiring a unit test
// cannot: the gate the fault pipelines decide against is the one the reset
// endpoint re-arms. A first_n rule that has spent itself breaks the next
// request again after a reset, and it is the same running instance throughout.
func TestServeResetSessionRearmsTheRunningPipeline(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "upstream")
	}))
	defer up.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncWriter{}
	served := make(chan error, 1)
	go func() {
		served <- serve(ctx, out, nil, 0, 0, nil, nil, nil)
	}()

	adminAddr := waitForAddr(t, out, "admin: http://")
	proxyAddr := waitForAddr(t, out, "proxy: http://")

	admin := "http://" + adminAddr
	upstreamHost := hostOf(t, up.URL)

	// A rule that breaks only the first matching request, so "spent" is one
	// request away and the test does not have to count.
	postJSON(t, admin+"/api/rules", `{"name":"once","match":{"host":"`+upstreamHost+`"},`+
		`"fault":{"type":"status","code":503},"behavior":{"type":"first_n","n":1}}`)

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: proxyAddr})},
		Timeout:   5 * time.Second,
	}

	if got := statusThrough(t, client, up.URL+"/orders"); got != http.StatusServiceUnavailable {
		t.Fatalf("first request = %d, want 503 from the rule", got)
	}
	if got := statusThrough(t, client, up.URL+"/orders"); got != http.StatusOK {
		t.Fatalf("second request = %d, want 200: first_n 1 is spent", got)
	}

	postJSON(t, admin+"/api/sessions/current/reset", "")

	if got := statusThrough(t, client, up.URL+"/orders"); got != http.StatusServiceUnavailable {
		t.Errorf("request after a reset = %d, want 503: the rule should be re-armed", got)
	}

	// The reset cleared what was observed too, so only the request since it
	// remains.
	if got := countEvents(t, admin); got != 1 {
		t.Errorf("%d events after a reset and one request, want 1", got)
	}

	cancel()
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	return u.Host
}

func postJSON(t *testing.T, target, body string) {
	t.Helper()
	resp, err := http.Post(target, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		got, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s = %d: %s", target, resp.StatusCode, got)
	}
}

func statusThrough(t *testing.T, client *http.Client, target string) int {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

func countEvents(t *testing.T, admin string) int {
	t.Helper()
	resp, err := http.Get(admin + "/api/events")
	if err != nil {
		t.Fatalf("reading events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return strings.Count(string(body), `"id":`)
}
