package client_test

import (
	"context"
	"errors"
	"testing"
	"time"

	client "github.com/josipmusa/faultline/clients/go"
	"github.com/josipmusa/faultline/internal/admin"
)

// TestStreamDeliversEventsLive also pins that the client sends an Origin the
// server accepts: the admin socket checks it against the Host, and a client
// that sends a wrong one is refused at the handshake.
func TestStreamDeliversEventsLive(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got := make(chan client.Message, 4)
	done := make(chan error, 1)
	go func() {
		done <- h.client.Stream(ctx, func(m client.Message) error {
			got <- m
			return nil
		})
	}()

	// The subscription is made during the handshake, so keep recording until
	// one arrives rather than racing the connection.
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()

	for {
		record(t, h, "1", "httpbin.org", true)
		select {
		case m := <-got:
			if m.Type != admin.MessageEvent || m.Event == nil || m.Event.Host != "httpbin.org" {
				t.Fatalf("message = %+v, want an event for httpbin.org", m)
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("Stream() = %v, want context.Canceled after the cancel", err)
			}
			return
		case err := <-done:
			t.Fatalf("Stream returned early: %v", err)
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("no event arrived on the stream")
		}
	}
}

func TestStreamAgainstNothingSaysTheInstanceIsNotRunning(t *testing.T) {
	c, err := client.New("localhost:1")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = c.Stream(context.Background(), func(client.Message) error { return nil })

	var unreachable *client.UnreachableError
	if !errors.As(err, &unreachable) {
		t.Fatalf("Stream() error = %v (%T), want *client.UnreachableError", err, err)
	}
}
