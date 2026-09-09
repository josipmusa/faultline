package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/rules"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerWith(t, nil)
}

// newTestServerWith is newTestServer with the forward proxy's bypass list.
func newTestServerWith(t *testing.T, bypass *forward.Bypass) *Server {
	t.Helper()
	rec := events.NewRecorder(events.DefaultSize)
	t.Cleanup(rec.Close)
	s := NewServer(rules.New(), rec, bypass, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	return s
}

func do(t *testing.T, s *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(method, target, r))
	return w
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding %q: %v", w.Body.String(), err)
	}
	return v
}

// wantStatus fails unless the response carries the expected code, printing the
// body when it does not, because the body is where the reason lives.
func wantStatus(t *testing.T, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Fatalf("status = %d, want %d, body %s", w.Code, code, w.Body.String())
	}
}

func wantError(t *testing.T, w *httptest.ResponseRecorder, code int, field string) {
	t.Helper()
	wantStatus(t, w, code)
	got := decodeBody[apiError](t, w)
	if got.Message == "" {
		t.Error("error body has no message")
	}
	if got.Field != field {
		t.Errorf("field = %q, want %q (message %q)", got.Field, field, got.Message)
	}
}

func TestHealth(t *testing.T) {
	w := do(t, newTestServer(t), http.MethodGet, "/api/health", "")

	wantStatus(t, w, http.StatusOK)
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", got)
	}
	if got := decodeBody[map[string]string](t, w); got["status"] != "ok" {
		t.Errorf("body = %v, want status ok", got)
	}
}

func TestUnknownEndpointIsAJSON404(t *testing.T) {
	w := do(t, newTestServer(t), http.MethodGet, "/api/nope", "")

	wantError(t, w, http.StatusNotFound, "")
}
