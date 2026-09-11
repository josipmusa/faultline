package admin

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigReportsTheFileInUse(t *testing.T) {
	s := newTestServer(t)
	s.Persist(&counter{path: "/tmp/ops/faultline.yaml"})

	w := do(t, s, http.MethodGet, "/api/config", "")
	wantStatus(t, w, http.StatusOK)
	got := decodeBody[configInfo](t, w)

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
	got := decodeBody[configInfo](t, w)

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

	got := decodeBody[configInfo](t, do(t, s, http.MethodGet, "/api/config", ""))

	if !filepath.IsAbs(got.Path) {
		t.Errorf("config path is %q, want it resolved against the working directory", got.Path)
	}
	if filepath.Base(got.Path) != "faultline.yaml" {
		t.Errorf("config path is %q, want it to still name the file in use", got.Path)
	}
}
