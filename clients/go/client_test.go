package client_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/admin"
	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/proxy/forward"
	"github.com/josipmusa/faultline/internal/rules"
)

// harness is a real admin server behind a real socket, so the tests exercise
// the wire and not a hand-written stand-in for it.
type harness struct {
	client    *client.Client
	rules     *rules.Store
	scenarios *rules.Scenarios
	events    *events.Recorder
	url       string
}

func newHarness(t *testing.T) harness {
	t.Helper()

	rec := events.NewRecorder(events.DefaultSize)
	t.Cleanup(rec.Close)

	store := rules.New()
	scenarios := rules.NewScenarios(store)

	bypass, err := forward.NewBypass(nil)
	if err != nil {
		t.Fatalf("bypass: %v", err)
	}

	api := admin.NewServer(store, scenarios, rec, bypass, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { _ = api.Shutdown(context.Background()) })

	ts := httptest.NewServer(api)
	t.Cleanup(ts.Close)

	c, err := client.New(ts.URL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return harness{client: c, rules: store, scenarios: scenarios, events: rec, url: ts.URL}
}

func TestNewAcceptsAnAddressWithoutAScheme(t *testing.T) {
	c, err := client.New("localhost:9000")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got, want := c.Addr(), "http://localhost:9000"; got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
}

func TestNewRejectsAnAddressItCannotUse(t *testing.T) {
	for _, addr := range []string{"", "   ", "ftp://localhost:9000", "http://"} {
		if _, err := client.New(addr); err == nil {
			t.Errorf("New(%q) = nil error, want one", addr)
		}
	}
}

func TestAnInstanceThatIsNotRunningSaysSo(t *testing.T) {
	// A closed listener gives a port nothing is on, without guessing one.
	ts := httptest.NewServer(http.NotFoundHandler())
	addr := ts.URL
	ts.Close()

	c, err := client.New(addr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.Rules(context.Background())
	var unreachable *client.UnreachableError
	if !errors.As(err, &unreachable) {
		t.Fatalf("Rules() error = %v (%T), want *client.UnreachableError", err, err)
	}
	if !strings.Contains(err.Error(), addr) || strings.Contains(err.Error(), "dial tcp") {
		t.Errorf("error = %q, want one clear line naming %s and no dial detail", err, addr)
	}
}

func TestAPIErrorsCarryTheMessageAndField(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.AddRule(context.Background(), client.Rule{
		Name:  "no such fault",
		Fault: client.Fault{Type: "nonesuch"},
	})

	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("AddRule() error = %v (%T), want *client.APIError", err, err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", apiErr.Status, http.StatusBadRequest)
	}
	if apiErr.Field != "fault.type" {
		t.Errorf("field = %q, want %q", apiErr.Field, "fault.type")
	}
	if !strings.Contains(apiErr.Message, "nonesuch") {
		t.Errorf("message = %q, want it to name the unknown type", apiErr.Message)
	}
}

func TestARequestRespectsACancelledContext(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := h.client.Rules(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Rules() error = %v, want context.Canceled", err)
	}
}
