package faults

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

func buildHeaders(t *testing.T, params rules.Params) Applier {
	t.Helper()
	applier, err := Build(rules.Fault{Type: "headers", Params: params})
	if err != nil {
		t.Fatalf("building the headers fault: %v", err)
	}
	return applier
}

// respondThrough runs a fault against an upstream that answers with body and
// the given headers, and returns the response the client would see. The caller
// closes its body.
func respondThrough(t *testing.T, fault Applier, ruleID, body string, header http.Header) *http.Response {
	t.Helper()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for name, values := range header {
			for _, v := range values {
				w.Header().Add(name, v)
			}
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.Close)

	req := httptest.NewRequest(http.MethodGet, upstream.URL, nil).WithContext(t.Context())

	resp, err := fault.Respond(ruleID, req, http.DefaultTransport)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	return resp
}

func TestHeadersSetsAndRemovesOnTheResponse(t *testing.T) {
	fault := buildHeaders(t, rules.Params{
		"set":    map[string]any{"X-Faultline": "set", "Cache-Control": "no-store"},
		"remove": []any{"etag"},
	})

	resp := respondThrough(t, fault, "reheaded", `{"ok":true}`, http.Header{
		"Etag":          []string{`"v1"`},
		"Cache-Control": []string{"max-age=60"},
	})
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get("X-Faultline"); got != "set" {
		t.Errorf("X-Faultline = %q, want %q", got, "set")
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want the set value to replace the upstream one", got)
	}
	if _, ok := resp.Header["Etag"]; ok {
		t.Errorf("ETag survived removal: %q", resp.Header.Get("Etag"))
	}
}

func TestHeadersLeavesTheBodyAlone(t *testing.T) {
	fault := buildHeaders(t, rules.Params{"set": map[string]any{"X-Faultline": "set"}})

	resp := respondThrough(t, fault, "reheaded", `{"ok":true}`, nil)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q, want the upstream body untouched", body)
	}
	if resp.ContentLength != int64(len(`{"ok":true}`)) {
		t.Errorf("content length = %d, want %d", resp.ContentLength, len(`{"ok":true}`))
	}
}

func TestHeadersRemovesEveryValueOfAName(t *testing.T) {
	fault := buildHeaders(t, rules.Params{"remove": []any{"Set-Cookie"}})

	resp := respondThrough(t, fault, "recookied", "hello", http.Header{
		"Set-Cookie": []string{"a=1", "b=2"},
	})
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie = %v, want every value gone", got)
	}
}
