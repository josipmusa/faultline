package faults

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

func buildSlowBody(t *testing.T, params rules.Params) Applier {
	t.Helper()
	applier, err := Build(rules.Fault{Type: "slow_body", Params: params})
	if err != nil {
		t.Fatalf("building the slow_body fault: %v", err)
	}
	return applier
}

func TestSlowBodySpreadsACorrectBodyOverTheDuration(t *testing.T) {
	body := strings.Repeat("x", 400)

	start := time.Now()
	resp := respondThrough(t, buildSlowBody(t, rules.Params{"ms": 300}), "trickle", body, nil)
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	elapsed := time.Since(start)

	if string(got) != body {
		t.Errorf("body is %d bytes, want the whole %d: a slow body is still correct", len(got), len(body))
	}
	if resp.ContentLength != int64(len(body)) {
		t.Errorf("content length = %d, want %d", resp.ContentLength, len(body))
	}
	if elapsed < 250*time.Millisecond {
		t.Errorf("the body arrived in %v, want it spread over about 300ms", elapsed)
	}
}

func TestSlowBodyArrivesInChunksAlongTheWay(t *testing.T) {
	body := strings.Repeat("x", 400)
	resp := respondThrough(t, buildSlowBody(t, rules.Params{"ms": 300}), "trickle", body, nil)
	defer func() { _ = resp.Body.Close() }()

	start := time.Now()
	var firstChunkAt time.Duration
	buf := make([]byte, len(body))
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 && firstChunkAt == 0 {
			firstChunkAt = time.Since(start)
		}
		if err != nil {
			break
		}
	}

	if firstChunkAt == 0 {
		t.Fatal("no chunk arrived at all")
	}
	if firstChunkAt > 150*time.Millisecond {
		t.Errorf("the first chunk took %v, want it well before the end of the duration", firstChunkAt)
	}
}

func TestSlowBodyStopsWhenTheClientGivesUp(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 4000))
	}))
	defer upstream.Close()

	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodGet, upstream.URL, nil).WithContext(ctx)

	resp, err := buildSlowBody(t, rules.Params{"ms": 10000}).Respond("trickle", req, http.DefaultTransport)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	time.AfterFunc(20*time.Millisecond, cancel)

	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("the whole body arrived after the client gave up")
	}
}

func TestSlowBodySpreadsAChunkedBodyItHadToMeasure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flush := http.NewResponseController(w)
		for range 4 { // flushing without a Content-Length makes the response chunked
			_, _ = io.WriteString(w, strings.Repeat("x", 100))
			_ = flush.Flush()
		}
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, upstream.URL, nil).WithContext(t.Context())

	start := time.Now()
	resp, err := buildSlowBody(t, rules.Params{"ms": 300}).Respond("trickle", req, http.DefaultTransport)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	elapsed := time.Since(start)

	if len(got) != 400 {
		t.Errorf("body is %d bytes, want all 400", len(got))
	}
	if elapsed < 250*time.Millisecond {
		t.Errorf("the body arrived in %v, want it spread over about 300ms", elapsed)
	}
}
