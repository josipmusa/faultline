package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/rules"
)

// addConnFault puts one connection fault on the upstream the fixture proxies
// to. hostOf keeps a non-default port, so the rule matches host and port.
func addConnFault(t *testing.T, f *proxyFixture, up *recordingUpstream, id string, fault rules.Fault) {
	t.Helper()
	if err := f.store.Add(rules.Rule{
		ID:      id,
		Enabled: true,
		Match:   rules.Match{Host: up.Listener.Addr().String()},
		Fault:   fault,
	}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}
}

func TestResetBreaksTheClientConnection(t *testing.T) {
	up := newUpstream(t)
	f := newFixture(t)
	addConnFault(t, f, up, "cut", rules.Fault{Type: "reset"})

	resp, err := f.client(t).Get(up.URL + "/orders") //nolint:bodyclose // there is no response to close
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the request succeeded, want a reset connection")
	}
	if !errors.Is(err, syscall.ECONNRESET) {
		t.Errorf("err = %v, want a connection reset", err)
	}
	if got := up.hits.Load(); got != 0 {
		t.Errorf("upstream hits = %d, want 0: a reset with no bytes never calls upstream", got)
	}
}

func TestResetAfterBytesDeliversThemAndThenBreaks(t *testing.T) {
	up := newUpstream(t)
	f := newFixture(t)
	addConnFault(t, f, up, "cut", rules.Fault{Type: "reset", Params: rules.Params{"after_bytes": 5}})

	resp, err := f.client(t).Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: the answer is real until it is cut", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err == nil {
		t.Fatalf("the whole body arrived (%q), want the transfer to break", body)
	}
	if got := up.hits.Load(); got != 1 {
		t.Errorf("upstream hits = %d, want 1: the bytes delivered are real ones", got)
	}
}

func TestThrottlePacesWhatTheClientReceives(t *testing.T) {
	up := newUpstream(t)
	f := newFixture(t)
	// "hello from upstream" is 19 bytes, so 100 a second is about 190ms.
	addConnFault(t, f, up, "slow", rules.Fault{Type: "throttle", Params: rules.Params{"bytes_per_sec": 100}})

	start := time.Now()
	resp, err := f.client(t).Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	elapsed := time.Since(start)

	if string(body) != "hello from upstream" {
		t.Errorf("body = %q, want the whole answer: a throttle delivers everything", body)
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("the body arrived in %v, want it paced", elapsed)
	}
}

func TestHangNeverAnswersUntilTheClientGivesUp(t *testing.T) {
	up := newUpstream(t)
	f := newFixture(t)
	addConnFault(t, f, up, "stuck", rules.Fault{Type: "hang"})

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, up.URL+"/orders", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}

	resp, err := f.client(t).Do(req) //nolint:bodyclose // there is no response to close
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the request was answered, want it to hang until the client gave up")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the client's own deadline", err)
	}
	if got := up.hits.Load(); got != 0 {
		t.Errorf("upstream hits = %d, want 0: a hang never sends the request", got)
	}
}

func TestResetCutsAnEncryptedTunnel(t *testing.T) {
	f := newFixture(t)
	up, client := f.tlsUpstream(t)
	addConnFault(t, f, up, "cut", rules.Fault{Type: "reset"})

	resp, err := client.Get(up.URL + "/orders") //nolint:bodyclose // there is no response to close
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the tunnelled request succeeded, want a reset connection")
	}
	// The tunnel is cut while the client is still sending its handshake, and a
	// reset that arrives during a write is reported as a broken pipe; both are
	// the same event seen from different sides of the socket.
	if !errors.Is(err, syscall.ECONNRESET) && !errors.Is(err, syscall.EPIPE) {
		t.Errorf("err = %v, want a connection reset", err)
	}
	if got := up.hits.Load(); got != 0 {
		t.Errorf("upstream hits = %d, want 0: the tunnel was cut before anything got through", got)
	}
}

func TestThrottlePacesAnEncryptedTunnel(t *testing.T) {
	f := newFixture(t)
	up, client := f.tlsUpstream(t)
	// The handshake alone is a few hundred bytes, so 2000 a second is visible.
	addConnFault(t, f, up, "slow", rules.Fault{Type: "throttle", Params: rules.Params{"bytes_per_sec": 2000}})

	start := time.Now()
	resp, err := client.Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("tunneled request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	elapsed := time.Since(start)

	if string(body) != "hello over tls" {
		t.Errorf("body = %q, want the whole answer through the tunnel", body)
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("the answer arrived in %v, want it paced", elapsed)
	}
}

func TestResetBreaksAnInterceptedConnection(t *testing.T) {
	f := newInterceptFixture(t)
	addConnFault(t, f.proxyFixture, f.upstream, "cut", rules.Fault{Type: "reset"})

	resp, err := f.trustingClient(t).Get(f.upstream.URL + "/orders") //nolint:bodyclose // there is no response to close
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the intercepted request succeeded, want a reset connection")
	}
	// The socket is closed under the TLS layer, so the client sees a reset
	// rather than the close_notify a polite shutdown would send.
	if !errors.Is(err, syscall.ECONNRESET) && !errors.Is(err, syscall.EPIPE) {
		t.Errorf("err = %v, want a connection reset", err)
	}
	if got := f.upstream.hits.Load(); got != 0 {
		t.Errorf("upstream hits = %d, want 0: a reset with no bytes never calls upstream", got)
	}
}
