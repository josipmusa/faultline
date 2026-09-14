package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/capture"
)

// capturedThrough sends one request with a body through a Faultline started
// with the given capture setting, and answers with the capture it filed.
func capturedThrough(t *testing.T, bodies bool) capture.Capture {
	t.Helper()

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Upstream", "yes")
		_, _ = io.WriteString(w, "real body")
	}))
	defer up.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncWriter{}
	served := make(chan error, 1)
	go func() { served <- serve(ctx, out, nil, DefaultBind, 0, 0, nil, nil, nil, false, bodies) }()
	t.Cleanup(func() {
		cancel()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	adminAddr := waitForAddr(t, out, "admin: http://")
	proxyAddr := waitForAddr(t, out, "proxy: http://")

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(mustParse(t, "http://"+proxyAddr))},
		Timeout:   5 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, up.URL+"/orders", strings.NewReader("who=me"))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request through the forward proxy: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	var recorded []struct {
		ID string `json:"id"`
	}
	getJSON(t, "http://"+adminAddr+"/api/events", &recorded)
	if len(recorded) != 1 {
		t.Fatalf("recorded %d events, want 1", len(recorded))
	}

	var c capture.Capture
	getJSON(t, "http://"+adminAddr+"/api/events/"+recorded[0].ID+"/capture", &c)
	return c
}

// getJSON reads one admin endpoint into out.
func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx // a test against a listener on this machine
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: %d %s", url, resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decoding %s: %v", url, err)
	}
}

// --no-bodies is for traffic Faultline should not be holding. The exchange is
// still captured and still shows what was called; only the payloads are gone.
func TestServeWithoutBodiesCapturesHeadersAndNoPayload(t *testing.T) {
	c := capturedThrough(t, false)

	if c.Bodies {
		t.Error(`"bodies" = true, want false under --no-bodies`)
	}
	if got := c.Request.Headers.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("captured request Authorization = %q, want it kept: --no-bodies is about payloads", got)
	}
	if got := c.Response.Headers.Get("X-Upstream"); got != "yes" {
		t.Errorf("captured response header X-Upstream = %q, want yes", got)
	}
	if len(c.Request.Body) != 0 || len(c.Response.Body) != 0 {
		t.Errorf("captured bodies = %q and %q, want neither", c.Request.Body, c.Response.Body)
	}
}

// The default is unchanged: bodies are captured, and the capture says so.
func TestServeCapturesBodiesByDefault(t *testing.T) {
	c := capturedThrough(t, true)

	if !c.Bodies {
		t.Error(`"bodies" = false, want true by default`)
	}
	if got := string(c.Request.Body); got != "who=me" {
		t.Errorf("captured request body = %q, want %q", got, "who=me")
	}
	if got := string(c.Response.Body); got != "real body" {
		t.Errorf("captured response body = %q, want the upstream body", got)
	}
}

func TestHelpDocumentsTheNoBodiesFlag(t *testing.T) {
	for _, command := range []string{"serve", "run"} {
		got := runCmd(t, command, "--help")
		if !strings.Contains(got, "--no-bodies") {
			t.Errorf("%s help is missing --no-bodies:\n%s", command, got)
		}
	}
}
