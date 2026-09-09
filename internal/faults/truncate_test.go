package faults

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

func buildTruncate(t *testing.T, params rules.Params) Applier {
	t.Helper()
	applier, err := Build(rules.Fault{Type: "truncate", Params: params})
	if err != nil {
		t.Fatalf("building the truncate fault: %v", err)
	}
	return applier
}

func TestTruncateCutsTheBodyAfterTheGivenBytes(t *testing.T) {
	body := strings.Repeat("x", 400)

	resp := respondThrough(t, buildTruncate(t, rules.Params{"after_bytes": 100}), "cut", body, nil)
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if len(got) != 100 {
		t.Errorf("body is %d bytes, want 100", len(got))
	}
	if resp.ContentLength != 400 {
		t.Errorf("content length = %d, want the original 400 so the transfer looks broken", resp.ContentLength)
	}
	if got := resp.Header.Get("Content-Length"); got != "400" {
		t.Errorf("Content-Length = %q, want the original %q", got, "400")
	}
}

func TestTruncateCutsNothingAtAllWithZeroBytes(t *testing.T) {
	resp := respondThrough(t, buildTruncate(t, rules.Params{"after_bytes": 0}), "cut", strings.Repeat("x", 40), nil)
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("body is %d bytes, want none of it", len(got))
	}
	if resp.ContentLength != 40 {
		t.Errorf("content length = %d, want the original 40", resp.ContentLength)
	}
}

func TestTruncateCutsAtAPercentageOfADeclaredLength(t *testing.T) {
	resp := respondThrough(t, buildTruncate(t, rules.Params{"percent": 25}), "cut", strings.Repeat("x", 400), nil)
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if len(got) != 100 {
		t.Errorf("body is %d bytes, want 100, a quarter of 400", len(got))
	}
	if resp.ContentLength != 400 {
		t.Errorf("content length = %d, want the original 400", resp.ContentLength)
	}
}

func TestTruncateLearnsTheLengthOfAChunkedBodyForAPercentage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flush := http.NewResponseController(w)
		for range 4 { // flushing without a Content-Length makes the response chunked
			_, _ = io.WriteString(w, strings.Repeat("x", 100))
			_ = flush.Flush()
		}
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, upstream.URL, nil).WithContext(t.Context())

	resp, err := buildTruncate(t, rules.Params{"percent": 50}).Respond("cut", req, http.DefaultTransport)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if len(got) != 200 {
		t.Errorf("body is %d bytes, want 200, half of the 400 the upstream streamed", len(got))
	}
	if resp.ContentLength != 400 {
		t.Errorf("content length = %d, want the 400 bytes the upstream really sent", resp.ContentLength)
	}
	if got := resp.Header.Get("Content-Length"); got != "400" {
		t.Errorf("Content-Length = %q, want %q so the client notices the short transfer", got, "400")
	}
}

func TestTruncateDeliversEverythingWhenTheCutIsPastTheEnd(t *testing.T) {
	resp := respondThrough(t, buildTruncate(t, rules.Params{"after_bytes": 4000}), "cut", strings.Repeat("x", 400), nil)
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if len(got) != 400 {
		t.Errorf("body is %d bytes, want all 400", len(got))
	}
}
