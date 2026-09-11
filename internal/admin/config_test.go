package admin

import (
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/proxy/forward"
)

func TestConfigReportsTheFileInUse(t *testing.T) {
	s := newTestServer(t)
	s.Persist(&counter{path: "/tmp/ops/faultline.yaml"})

	w := do(t, s, http.MethodGet, "/api/config", "")
	wantStatus(t, w, http.StatusOK)
	got := decodeBody[Config](t, w)

	if !got.Persisted {
		t.Error("config says nothing is persisted while a file is in use")
	}
	if got.Path != "/tmp/ops/faultline.yaml" {
		t.Errorf("config path is %q, want the file in use", got.Path)
	}
}

// Without a file the UI has to say so rather than name a path, so the absence
// is a field of its own and not an empty string to be guessed at.
func TestConfigReportsRunningInMemory(t *testing.T) {
	s := newTestServer(t)

	w := do(t, s, http.MethodGet, "/api/config", "")
	wantStatus(t, w, http.StatusOK)
	got := decodeBody[Config](t, w)

	if got.Persisted {
		t.Error("config says changes are persisted while Faultline holds them in memory")
	}
	if got.Path != "" {
		t.Errorf("config names %q while no file is in use", got.Path)
	}
}

func TestConfigOmitsThePathWhenThereIsNone(t *testing.T) {
	s := newTestServer(t)

	if body := do(t, s, http.MethodGet, "/api/config", "").Body.String(); strings.Contains(body, `"path"`) {
		t.Errorf("config carries a path key with nothing in it: %s", body)
	}
}

// The path is read in a browser, away from the working directory that would
// make a relative one mean anything, so it is resolved before it is reported.
func TestConfigReportsAnAbsolutePath(t *testing.T) {
	s := newTestServer(t)
	s.Persist(&counter{path: "faultline.yaml"})

	got := decodeBody[Config](t, do(t, s, http.MethodGet, "/api/config", ""))

	if !filepath.IsAbs(got.Path) {
		t.Errorf("config path is %q, want it resolved against the working directory", got.Path)
	}
	if filepath.Base(got.Path) != "faultline.yaml" {
		t.Errorf("config path is %q, want it to still name the file in use", got.Path)
	}
}

// An agent wrapping a command needs three things from the instance it is
// talking to: where the proxy is, which hosts the child should reach directly,
// and whether the CA is in play. Without the bypass list the child would send
// its localhost calls, the admin port among them, back through the proxy.
func TestConfigReportsHowToAttachAChild(t *testing.T) {
	bypass, err := forward.NewBypass(append(slices.Clone(forward.DefaultBypass), "*.internal"))
	if err != nil {
		t.Fatalf("building the bypass list: %v", err)
	}
	s := newTestServerWith(t, bypass)
	s.ProxiesAt("http://localhost:9001", true)

	got := decodeBody[Config](t, do(t, s, http.MethodGet, "/api/config", ""))

	if got.ProxyURL != "http://localhost:9001" {
		t.Errorf("config says the proxy is at %q, want the address a child should use", got.ProxyURL)
	}
	if !got.Intercepting {
		t.Error("config says HTTPS is passed through while the CA is intercepting it")
	}
	if !slices.Contains(got.NoProxy, ".internal") {
		t.Errorf("no_proxy is %v, want the bypass list spelled the way NO_PROXY wants it", got.NoProxy)
	}
	if !slices.Contains(got.NoProxy, "localhost") {
		t.Errorf("no_proxy is %v, want the defaults that keep a child off the admin port", got.NoProxy)
	}
}

// Nothing answering on the proxy fields is the honest report for a Server used
// as a plain handler, which is what every other face of it already does.
func TestConfigOmitsTheProxyWhenThereIsNone(t *testing.T) {
	s := newTestServer(t)

	body := do(t, s, http.MethodGet, "/api/config", "").Body.String()
	if strings.Contains(body, `"proxy_url"`) {
		t.Errorf("config names a proxy while none is running: %s", body)
	}
}
