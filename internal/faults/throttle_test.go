package faults

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

func buildThrottle(t *testing.T, params rules.Params) Applier {
	t.Helper()
	applier, err := Build(rules.Fault{Type: "throttle", Params: params})
	if err != nil {
		t.Fatalf("building the throttle fault: %v", err)
	}
	return applier
}

func TestThrottlePacesTheResponseBody(t *testing.T) {
	body := strings.Repeat("x", 400)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, upstream.URL, nil).WithContext(t.Context())

	start := time.Now()
	resp, err := buildThrottle(t, rules.Params{"bytes_per_sec": 2000}).
		Respond("slow", req, http.DefaultTransport)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	elapsed := time.Since(start)

	if string(got) != body {
		t.Errorf("body is %d bytes, want %d: a throttle delivers everything", len(got), len(body))
	}
	if resp.ContentLength != int64(len(body)) {
		t.Errorf("content length = %d, want %d", resp.ContentLength, len(body))
	}
	// 400 bytes at 2000 a second is a fifth of a second, with the first chunk
	// free; anything near instant means nothing was paced.
	if elapsed < 100*time.Millisecond {
		t.Errorf("the body arrived in %v, want it paced", elapsed)
	}
}

func TestThrottleStopsWhenTheClientGivesUp(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 4000))
	}))
	defer upstream.Close()

	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodGet, upstream.URL, nil).WithContext(ctx)

	resp, err := buildThrottle(t, rules.Params{"bytes_per_sec": 100}).
		Respond("slow", req, http.DefaultTransport)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	time.AfterFunc(20*time.Millisecond, cancel)

	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("the whole body arrived after the client gave up")
	}
}

func TestThrottlePacesTheUpstreamHalfOfATunnel(t *testing.T) {
	client, upstream := net.Pipe()
	defer func() { _ = client.Close(); _ = upstream.Close() }()
	go func() {
		_, _ = io.WriteString(upstream, strings.Repeat("x", 400))
		_ = upstream.Close()
	}()

	next := func(context.Context, string, string) (net.Conn, error) { return client, nil }

	start := time.Now()
	conn, err := buildThrottle(t, rules.Params{"bytes_per_sec": 2000}).(Tunneler).
		Dial(t.Context(), "slow", "api.stripe.com:443", next)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("reading the tunnel: %v", err)
	}
	if len(got) != 400 {
		t.Errorf("tunnel carried %d bytes, want 400", len(got))
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("the bytes arrived in %v, want them paced", elapsed)
	}
}
