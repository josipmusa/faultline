package faults

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

func buildHang(t *testing.T, params rules.Params) Applier {
	t.Helper()
	applier, err := Build(rules.Fault{Type: "hang", Params: params})
	if err != nil {
		t.Fatalf("building the hang fault: %v", err)
	}
	return applier
}

func TestHangHoldsUntilTheClientGivesUp(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodGet, "http://api.stripe.com/v1/charges", nil).WithContext(ctx)

	called := false
	next := roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil //nolint:nilnil // the call must not happen at all
	})

	fault := buildHang(t, rules.Params{})

	done := make(chan error, 1)
	go func() {
		_, err := fault.Respond("stuck", req, next) //nolint:bodyclose // there is no response
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("Respond returned while the client was still waiting: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if called {
		t.Error("the upstream was called")
	}
}

func TestHangAbandonsTheConnectionAtItsCap(t *testing.T) {
	var aborted bool
	req := httptest.NewRequest(http.MethodGet, "http://api.stripe.com/v1/charges", nil).
		WithContext(withAborter(t, &aborted))

	start := time.Now()
	resp, err := buildHang(t, rules.Params{"max_ms": 30}).Respond("stuck", req, http.DefaultTransport) //nolint:bodyclose // there is no response
	if resp != nil {
		t.Fatal("a hang produced a response")
	}
	if err != ErrClientReset { //nolint:errorlint // the sentinel travels bare
		t.Fatalf("err = %v, want ErrClientReset", err)
	}
	if !aborted {
		t.Error("the client connection was left open at the cap")
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("gave up after %v, want at least the cap", elapsed)
	}
}

func TestHangDialHoldsTheTunnelOpenUntilItsCap(t *testing.T) {
	dialed := false
	next := func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, nil //nolint:nilnil // the dial must not happen at all
	}

	start := time.Now()
	conn, err := buildHang(t, rules.Params{"max_ms": 30}).(Tunneler).
		Dial(t.Context(), "stuck", "api.stripe.com:443", next)
	if conn != nil {
		t.Fatal("a hang opened a tunnel")
	}
	if err != ErrClientReset { //nolint:errorlint // the sentinel travels bare
		t.Fatalf("err = %v, want ErrClientReset", err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("gave up after %v, want at least the cap", elapsed)
	}
	if dialed {
		t.Error("the upstream was dialed")
	}
}
