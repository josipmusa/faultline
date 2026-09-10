//go:build ui

package admin

import (
	"net/http"
	"strings"
	"testing"
)

// With the ui tag the binary must carry a real export, not an empty directory
// that would compile and then serve nothing.
func TestEmbeddedUIIsServed(t *testing.T) {
	w := getUI(t, uiHandler(), "/")

	wantStatus(t, w, http.StatusOK)
	if !strings.Contains(strings.ToLower(w.Body.String()), "<!doctype html>") {
		t.Errorf("body = %q, want the exported index document", w.Body.String())
	}
}

func TestEmbeddedUIAnswersUnknownPaths(t *testing.T) {
	w := getUI(t, uiHandler(), "/nope")

	wantStatus(t, w, http.StatusNotFound)
	if w.Body.Len() == 0 {
		t.Error("body is empty, want the exported 404 document")
	}
}
