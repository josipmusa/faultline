package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The admin port has no authentication, so the only thing between a web page
// and it is the browser. A page can defeat the same-origin policy by pointing
// a name it controls at 127.0.0.1 (DNS rebinding), after which its requests
// arrive with that name in Host and look same-origin to the browser. Refusing
// a Host that is not a loopback name is what closes that, and refusing a
// cross-site Origin is what stops a page mutating state with a request the
// browser sends without asking (a "simple" POST).
func TestLoopbackAdminRefusesForeignHost(t *testing.T) {
	h := newTestServer(t).guardBrowsers("127.0.0.1")

	for _, host := range []string{"localhost:9000", "127.0.0.1:9000", "[::1]:9000", "localhost", "app.localhost:9000"} {
		w := doWithHost(h, host, "")
		if w.Code != http.StatusOK {
			t.Errorf("Host %q: status %d, want 200", host, w.Code)
		}
	}

	for _, host := range []string{"attacker.example:9000", "attacker.example", "faultline:9000", ""} {
		w := doWithHost(h, host, "")
		if w.Code != http.StatusForbidden {
			t.Errorf("Host %q: status %d, want 403", host, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Host %q: Content-Type %q, want the JSON error shape", host, ct)
		}
	}
}

// --bind 0.0.0.0 is a choice to be reachable by name from other machines and
// containers, so the Host check stands down there; the Origin check does not.
func TestWidenedAdminAcceptsAnyHostButNotAForeignOrigin(t *testing.T) {
	h := newTestServer(t).guardBrowsers("0.0.0.0")

	if w := doWithHost(h, "faultline:9000", ""); w.Code != http.StatusOK {
		t.Errorf("Host faultline:9000: status %d, want 200", w.Code)
	}
	if w := doWithHost(h, "faultline:9000", "http://evil.example"); w.Code != http.StatusForbidden {
		t.Errorf("foreign Origin: status %d, want 403", w.Code)
	}
}

func TestOriginMustMatchHost(t *testing.T) {
	h := newTestServer(t).guardBrowsers("127.0.0.1")

	ok := map[string]string{
		"localhost:9000": "http://localhost:9000",
		"127.0.0.1:9000": "http://127.0.0.1:9000",
		"[::1]:9000":     "http://[::1]:9000",
		"LOCALHOST:9000": "http://localhost:9000",
	}
	for host, origin := range ok {
		if w := doWithHost(h, host, origin); w.Code != http.StatusOK {
			t.Errorf("Host %q Origin %q: status %d, want 200", host, origin, w.Code)
		}
	}

	bad := []string{
		"http://evil.example",
		"http://localhost:3000", // another local dev server is still another site
		"http://127.0.0.1:9000", // a different spelling of the same machine is a different origin to a browser
		"null",
		"not a url",
	}
	for _, origin := range bad {
		if w := doWithHost(h, "localhost:9000", origin); w.Code != http.StatusForbidden {
			t.Errorf("Origin %q: status %d, want 403", origin, w.Code)
		}
	}
}

// The guard covers everything on the port, /mcp included: start_wrapped runs
// commands, so that path is the one a rebinding page would go for.
func TestGuardCoversEveryPath(t *testing.T) {
	h := newTestServer(t).guardBrowsers("127.0.0.1")

	for _, path := range []string{"/api/rules", "/mcp", "/", "/api/events/stream"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Host = "attacker.example:9000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("POST %s with a foreign Host: status %d, want 403", path, w.Code)
		}
	}
}

// Start is where the guard is installed, so this is the check that the
// listening server, not just the handler, refuses a rebound name.
func TestStartedServerRefusesForeignHost(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})

	get := func(host string) int {
		req, err := http.NewRequest(http.MethodGet, "http://"+srv.Addr()+"/api/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		if host != "" {
			req.Host = host
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := res.Body.Close(); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode
	}

	if got := get(""); got != http.StatusOK {
		t.Errorf("own address: status %d, want 200", got)
	}
	if got := get("attacker.example"); got != http.StatusForbidden {
		t.Errorf("rebound name: status %d, want 403", got)
	}
}

func doWithHost(h http.Handler, host, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
