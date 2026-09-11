package client_test

import (
	"context"
	"testing"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/events"
)

func TestResetSessionClearsWhatWasObserved(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	record(t, h, events.NextID(), "api.stripe.com", true)

	if err := h.client.ResetSession(ctx); err != nil {
		t.Fatalf("ResetSession: %v", err)
	}

	left, err := h.client.Events(ctx, client.EventQuery{})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d events after ResetSession, want 0", len(left))
	}
}
