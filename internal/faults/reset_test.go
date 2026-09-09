package faults

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/josipmusa/faultline/internal/rules"
)

// withAborter installs an aborter that only records that it ran, so a fault's
// own behaviour can be tested without a socket.
func withAborter(t *testing.T, aborted *bool) context.Context {
	t.Helper()
	return context.WithValue(t.Context(), aborterKey{}, func() { *aborted = true })
}

func buildReset(t *testing.T, params rules.Params) Applier {
	t.Helper()
	applier, err := Build(rules.Fault{Type: "reset", Params: params})
	if err != nil {
		t.Fatalf("building the reset fault: %v", err)
	}
	return applier
}

func TestResetAnswersNothingAndNeverReachesTheUpstream(t *testing.T) {
	var aborted bool
	req := httptest.NewRequest(http.MethodGet, "http://api.stripe.com/v1/charges", nil).
		WithContext(withAborter(t, &aborted))

	called := false
	next := roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil //nolint:nilnil // the call must not happen at all
	})

	resp, err := buildReset(t, rules.Params{}).Respond("kill", req, next) //nolint:bodyclose // there is no response
	if resp != nil {
		t.Fatal("a reset produced a response")
	}
	if err != ErrClientReset { //nolint:errorlint // the sentinel travels bare
		t.Fatalf("err = %v, want ErrClientReset", err)
	}
	if !aborted {
		t.Error("the client connection was not reset")
	}
	if called {
		t.Error("the upstream was called")
	}
}

func TestResetAfterBytesCutsTheBodyAndResetsTheClient(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "0123456789")
	}))
	defer upstream.Close()

	var aborted bool
	req := httptest.NewRequest(http.MethodGet, upstream.URL, nil).
		WithContext(withAborter(t, &aborted))

	resp, err := buildReset(t, rules.Params{"after_bytes": 4}).Respond("kill", req, http.DefaultTransport)
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: the response is real up to the cut", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if string(body) != "0123" {
		t.Errorf("body = %q, want %q", body, "0123")
	}
	if err != ErrClientReset { //nolint:errorlint // the sentinel travels bare
		t.Fatalf("reading past the cut: %v, want ErrClientReset", err)
	}
	if !aborted {
		t.Error("the client connection was not reset")
	}
}

func TestResetDialCutsTheTunnelAfterItsBytes(t *testing.T) {
	client, upstream := net.Pipe()
	defer func() { _ = client.Close(); _ = upstream.Close() }()
	go func() { _, _ = io.WriteString(upstream, "0123456789") }()

	next := func(context.Context, string, string) (net.Conn, error) { return client, nil }

	conn, err := buildReset(t, rules.Params{"after_bytes": 4}).(Tunneler).
		Dial(t.Context(), "kill", "api.stripe.com:443", next)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	got, err := io.ReadAll(conn)
	if string(got) != "0123" {
		t.Errorf("tunnel carried %q, want %q", got, "0123")
	}
	if !IsReset(err) {
		t.Fatalf("reading past the cut: %v, want a *ResetError", err)
	}
}

func TestResetDialWithoutBytesCutsTheTunnelAtOnce(t *testing.T) {
	client, upstream := net.Pipe()
	defer func() { _ = client.Close(); _ = upstream.Close() }()

	next := func(context.Context, string, string) (net.Conn, error) { return client, nil }

	conn, err := buildReset(t, rules.Params{}).(Tunneler).
		Dial(t.Context(), "kill", "api.stripe.com:443", next)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	if _, err := conn.Read(make([]byte, 8)); !IsReset(err) {
		t.Fatalf("reading a reset tunnel: %v, want a *ResetError", err)
	}
}
