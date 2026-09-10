package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// export is a static export's shape: an index, a 404 page, and a hashed asset
// under a directory Next names with a leading underscore.
func export() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                 {Data: []byte("<!DOCTYPE html>index")},
		"404.html":                   {Data: []byte("<!DOCTYPE html>not found")},
		"_next/static/chunk/main.js": {Data: []byte("console.log(1)")},
		"_next/static/css/theme.css": {Data: []byte("body{}")},
	}
}

func getUI(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func TestUIServesExport(t *testing.T) {
	h := newUIHandler(export())

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{"root serves the index", "/", "index"},
		{"nested asset", "/_next/static/chunk/main.js", "console.log(1)"},
		{"stylesheet", "/_next/static/css/theme.css", "body{}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := getUI(t, h, tc.target)
			wantStatus(t, w, http.StatusOK)
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Errorf("body = %q, want it to contain %q", w.Body.String(), tc.want)
			}
		})
	}
}

// A mistyped URL is a browser reading a page, so it gets the export's own 404
// document rather than a Go error, and still the status that says so.
func TestUIUnknownPathServesTheExports404(t *testing.T) {
	w := getUI(t, newUIHandler(export()), "/nope")

	wantStatus(t, w, http.StatusNotFound)
	if !strings.Contains(w.Body.String(), "not found") {
		t.Errorf("body = %q, want the export's 404 document", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

// A directory is not a page: serving a listing would expose the export's
// layout, so it reads as a miss like any other.
func TestUIDirectoryIsNotServed(t *testing.T) {
	w := getUI(t, newUIHandler(export()), "/_next/static")

	wantStatus(t, w, http.StatusNotFound)
	if strings.Contains(w.Body.String(), "theme.css") {
		t.Errorf("body = %q, want no directory listing", w.Body.String())
	}
}

func TestUIWithoutA404DocumentStillAnswers(t *testing.T) {
	w := getUI(t, newUIHandler(fstest.MapFS{"index.html": {Data: []byte("index")}}), "/nope")

	wantStatus(t, w, http.StatusNotFound)
	if w.Body.Len() == 0 {
		t.Error("body is empty, want an explanation")
	}
}

// Without the ui build tag there is nothing to serve, and a blank 404 would
// read as a broken install. The page names the command that fixes it.
func TestUINotBuiltExplainsItself(t *testing.T) {
	w := getUI(t, newUIHandler(nil), "/")

	wantStatus(t, w, http.StatusServiceUnavailable)
	body := w.Body.String()
	for _, want := range []string{"make ui", "/api"} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %q, want it to mention %q", body, want)
		}
	}
}

// The UI takes over /, so an unknown endpoint must still be told apart from a
// mistyped page: under /api it stays a JSON error with the documented shape.
func TestUnknownAPIEndpointStaysJSON(t *testing.T) {
	s := newTestServer(t)

	wantError(t, do(t, s, http.MethodGet, "/api/nope", ""), http.StatusNotFound, "")
	wantError(t, do(t, s, http.MethodPost, "/api/rules/x/explode", ""), http.StatusNotFound, "")
}
