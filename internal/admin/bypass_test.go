package admin

import (
	"net/http"
	"testing"

	"github.com/josipmusa/faultline/internal/proxy/forward"
)

func testBypass(t *testing.T, hosts ...string) *forward.Bypass {
	t.Helper()
	b, err := forward.NewBypass(append(forward.DefaultBypass, hosts...))
	if err != nil {
		t.Fatalf("NewBypass: %v", err)
	}
	return b
}

func TestAddBypass(t *testing.T) {
	bypass := testBypass(t)
	s := newTestServerWith(t, bypass)

	w := do(t, s, http.MethodPost, "/api/bypass", `{"host":"API.Stripe.com"}`)
	wantStatus(t, w, http.StatusCreated)

	if got := decodeBody[bypassEntry](t, w).Host; got != "api.stripe.com" {
		t.Errorf("host = %q, want it normalized", got)
	}
	if !bypass.Matches("api.stripe.com") {
		t.Error("the host is not bypassed, so the next request would still be proxied")
	}

	// The panel's toggle can be clicked again before the row refreshes.
	wantStatus(t, do(t, s, http.MethodPost, "/api/bypass", `{"host":"api.stripe.com"}`), http.StatusCreated)
	if got := bypass.Configured(); len(got) != 1 {
		t.Errorf("Configured() = %q, want the host once", got)
	}
}

func TestAddBypassRejectsBadInput(t *testing.T) {
	s := newTestServerWith(t, testBypass(t))

	for name, body := range map[string]string{
		"empty body":     "",
		"no host":        `{}`,
		"blank host":     `{"host":"  "}`,
		"bad wildcard":   `{"host":"*.*.internal"}`,
		"unknown fields": `{"host":"httpbin.org","tier":"plain"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := do(t, s, http.MethodPost, "/api/bypass", body)
			wantStatus(t, w, http.StatusBadRequest)
			if field := decodeBody[apiError](t, w).Field; body != "" && field != "host" {
				t.Errorf("field = %q, want it to name host", field)
			}
		})
	}
}

func TestRemoveBypass(t *testing.T) {
	bypass := testBypass(t, "httpbin.org")
	s := newTestServerWith(t, bypass)

	wantStatus(t, do(t, s, http.MethodDelete, "/api/bypass/httpbin.org", ""), http.StatusNoContent)
	if bypass.Matches("httpbin.org") {
		t.Error("the host is still bypassed")
	}

	wantStatus(t, do(t, s, http.MethodDelete, "/api/bypass/httpbin.org", ""), http.StatusNotFound)
}

func TestRemoveBypassKeepsLoopback(t *testing.T) {
	bypass := testBypass(t)
	s := newTestServerWith(t, bypass)

	w := do(t, s, http.MethodDelete, "/api/bypass/localhost", "")
	wantStatus(t, w, http.StatusConflict)
	if !bypass.Matches("localhost") {
		t.Error("localhost stopped being bypassed; faultline would proxy itself")
	}
}

// Without a forward proxy there is no list to change, and saying so is better
// than a change that quietly reaches nothing.
func TestBypassWithoutForwardProxy(t *testing.T) {
	s := newTestServer(t)

	wantStatus(t, do(t, s, http.MethodPost, "/api/bypass", `{"host":"httpbin.org"}`), http.StatusConflict)
	wantStatus(t, do(t, s, http.MethodDelete, "/api/bypass/httpbin.org", ""), http.StatusConflict)
}
