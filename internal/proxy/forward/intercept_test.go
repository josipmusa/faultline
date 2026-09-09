package forward

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

// interceptFixture is a forward proxy with TLS interception on: a CA of its
// own, and an upstream whose certificate the intercepted pipeline trusts.
type interceptFixture struct {
	*proxyFixture
	ca       *tlsmitm.CA
	upstream *recordingUpstream
}

func newInterceptFixture(t *testing.T) *interceptFixture {
	t.Helper()
	return newInterceptFixtureWith(t, nil)
}

// newInterceptFixtureWith is newInterceptFixture with a bypass list.
func newInterceptFixtureWith(t *testing.T, bypass *Bypass) *interceptFixture {
	t.Helper()
	ca, err := tlsmitm.Create(t.TempDir())
	if err != nil {
		t.Fatalf("creating CA: %v", err)
	}
	issuer, err := tlsmitm.NewIssuer(ca)
	if err != nil {
		t.Fatalf("creating issuer: %v", err)
	}

	up := &recordingUpstream{}
	up.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.hits.Add(1)
		up.lastHost.Store(r.Host)
		up.lastHeader.Store(r.Header.Clone())
		up.lastPath.Store(r.URL.RequestURI())
		w.Header().Set("X-Upstream", "yes")
		_, _ = io.WriteString(w, "hello over tls")
	}))
	t.Cleanup(up.Close)

	store := rules.New()
	rec := events.NewRecorder(events.DefaultSize)
	t.Cleanup(rec.Close)

	// The intercepted pipeline dials the real upstream itself, so it needs to
	// trust the test upstream's certificate the way the real one trusts the
	// system roots.
	upstreamTransport, ok := up.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("upstream client transport is %T, want *http.Transport", up.Client().Transport)
	}
	t.Cleanup(upstreamTransport.CloseIdleConnections)

	plain := faults.New(nil, store, rec, events.TierPlain)
	intercepted := faults.New(upstreamTransport, store, rec, events.TierIntercepted)
	interceptor := NewInterceptor(issuer, intercepted, rec, quietLogger())
	srv := httptest.NewServer(NewServer(plain, faults.NewDialer(store, rec), interceptor, bypass, quietLogger()))
	t.Cleanup(srv.Close)

	return &interceptFixture{
		proxyFixture: &proxyFixture{proxy: srv, store: store, recorder: rec},
		ca:           ca,
		upstream:     up,
	}
}

// trustingClient sends everything through the proxy and trusts the Faultline
// CA, like an application that has been pointed at the CA certificate.
func (f *interceptFixture) trustingClient(t *testing.T) *http.Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(f.ca.Cert)
	return f.clientWith(t, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12})
}

// distrustingClient sends everything through the proxy but only trusts the
// system roots, like an application nobody told about the CA.
func (f *interceptFixture) distrustingClient(t *testing.T) *http.Client {
	t.Helper()
	return f.clientWith(t, &tls.Config{MinVersion: tls.VersionTLS12})
}

func (f *interceptFixture) clientWith(t *testing.T, tlsConfig *tls.Config) *http.Client {
	t.Helper()
	proxyURL, err := url.Parse(f.proxy.URL)
	if err != nil {
		t.Fatalf("parsing proxy url: %v", err)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: tlsConfig}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

// waitForEvent returns the single recorded event once it exists.
func (f *interceptFixture) waitForEvent(t *testing.T) events.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if recorded := f.recorder.Events(); len(recorded) > 0 {
			if len(recorded) != 1 {
				t.Fatalf("recorded %d events, want 1: %+v", len(recorded), recorded)
			}
			return recorded[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no event was recorded")
	return events.Event{}
}

func TestInterceptsHTTPSAndRecordsTheFullRequest(t *testing.T) {
	f := newInterceptFixture(t)

	resp, err := f.trustingClient(t).Get(f.upstream.URL + "/orders?id=7")
	if err != nil {
		t.Fatalf("intercepted request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Upstream"); got != "yes" {
		t.Errorf("X-Upstream = %q, want the upstream's own headers", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello over tls" {
		t.Errorf("body = %q, want the upstream's body", body)
	}
	if got, want := f.upstream.lastPath.Load(), "/orders?id=7"; got != want {
		t.Errorf("upstream saw %v, want %q", got, want)
	}
	if got, want := f.upstream.lastHost.Load(), f.upstream.Listener.Addr().String(); got != want {
		t.Errorf("upstream Host = %v, want %q, the host the client asked for", got, want)
	}

	recorded := f.recorder.Events()
	if len(recorded) != 1 {
		t.Fatalf("recorded %d events, want exactly 1 for the request, none for the tunnel: %+v", len(recorded), recorded)
	}
	e := recorded[0]
	if e.Tier != events.TierIntercepted {
		t.Errorf("tier = %q, want %q", e.Tier, events.TierIntercepted)
	}
	if e.Method != http.MethodGet || e.Path != "/orders" {
		t.Errorf("method/path = %s %q, want GET /orders: interception sees the whole request", e.Method, e.Path)
	}
	if e.Host != f.upstream.Listener.Addr().String() {
		t.Errorf("host = %q, want %q", e.Host, f.upstream.Listener.Addr().String())
	}
	if e.Status != http.StatusOK || e.Faulted || e.Error != "" {
		t.Errorf("event = %+v, want status 200, no fault, no error", e)
	}
}

func TestInterceptedRequestsGetResponseFaults(t *testing.T) {
	f := newInterceptFixture(t)
	if err := f.store.Add(rules.Rule{
		ID:      "orders-down",
		Enabled: true,
		Match:   rules.Match{Host: f.upstream.Listener.Addr().String(), Path: "/orders"},
		Fault:   rules.Fault{Type: "status", Params: rules.Params{"code": http.StatusServiceUnavailable}},
	}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}

	resp, err := f.trustingClient(t).Get(f.upstream.URL + "/orders")
	if err != nil {
		t.Fatalf("intercepted request: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 from the rule", resp.StatusCode)
	}
	if got := resp.Header.Get(faults.FaultHeader); got != "orders-down" {
		t.Errorf("%s = %q, want the rule id", faults.FaultHeader, got)
	}
	if got := f.upstream.hits.Load(); got != 0 {
		t.Errorf("upstream hits = %d, want 0: a status fault answers without forwarding", got)
	}

	recorded := f.recorder.Events()
	if len(recorded) != 1 || !recorded[0].Faulted || recorded[0].RuleID != "orders-down" || recorded[0].Tier != events.TierIntercepted {
		t.Errorf("events = %+v, want one intercepted event faulted by orders-down", recorded)
	}
}

func TestInterceptedRequestsReuseTheTunnelForKeepAlive(t *testing.T) {
	f := newInterceptFixture(t)
	client := f.trustingClient(t)

	for _, path := range []string{"/one", "/two"} {
		resp, err := client.Get(f.upstream.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	recorded := f.recorder.Events()
	if len(recorded) != 2 {
		t.Fatalf("recorded %d events, want one per request: %+v", len(recorded), recorded)
	}
	if recorded[0].Path != "/one" || recorded[1].Path != "/two" {
		t.Errorf("paths = %q, %q, want /one then /two", recorded[0].Path, recorded[1].Path)
	}
}

func TestRecordsAClearErrorWhenTheClientRejectsTheCertificate(t *testing.T) {
	f := newInterceptFixture(t)

	resp, err := f.distrustingClient(t).Get(f.upstream.URL + "/orders")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("a client that does not trust the CA completed the request")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("client error = %v, want a certificate error", err)
	}
	if got := f.upstream.hits.Load(); got != 0 {
		t.Errorf("upstream hits = %d, want 0", got)
	}

	// The client hears the failure before the proxy does: its alert is still
	// in flight when the request returns, so the event follows a moment later.
	e := f.waitForEvent(t)
	if e.Error != ErrClientRejectedCertificate {
		t.Errorf("error = %q, want %q", e.Error, ErrClientRejectedCertificate)
	}
	if e.Tier != events.TierIntercepted || e.Method != http.MethodConnect || e.Path != "" {
		t.Errorf("event = %+v, want an intercepted CONNECT with no path", e)
	}
	if e.Host != f.upstream.Listener.Addr().String() {
		t.Errorf("host = %q, want %q", e.Host, f.upstream.Listener.Addr().String())
	}
	if e.Status != 0 || e.Faulted {
		t.Errorf("event = %+v, want no status and no fault: Faultline did not cause this", e)
	}
}

func TestTunnelsBlindlyWithoutAnInterceptor(t *testing.T) {
	f := newFixture(t)
	up, client := f.tlsUpstream(t)

	resp, err := client.Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("tunneled request: %v", err)
	}
	_ = resp.Body.Close()

	recorded := f.recorder.Events()
	if len(recorded) != 1 || recorded[0].Tier != events.TierEncrypted {
		t.Errorf("events = %+v, want one encrypted event: no interceptor means no interception", recorded)
	}
}

func TestDescribeHandshakeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"certificate alert", &net.OpError{Op: "remote error", Err: errors.New("tls: bad certificate")}, ErrClientRejectedCertificate},
		{"unknown ca alert", &net.OpError{Op: "remote error", Err: errors.New("tls: unknown certificate authority")}, ErrClientRejectedCertificate},
		{"client hung up", io.EOF, ErrClientRejectedCertificate},
		{"alert we cannot decrypt", &net.OpError{Op: "local error", Err: errors.New("tls: bad record MAC")}, ErrClientRejectedCertificate},
		{"timed out", context.DeadlineExceeded, "client did not complete the TLS handshake in time"},
		{"not tls at all", errors.New("tls: first record does not look like a TLS handshake"), "TLS handshake failed: tls: first record does not look like a TLS handshake"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeHandshakeError(tt.err); got != tt.want {
				t.Errorf("describeHandshakeError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// TestInterceptReusedConnection sends two requests through one intercepted
// tunnel, the way any client with keep-alive enabled does. The second request
// travels on the connection the first one opened, so it must succeed on its
// own terms rather than inherit anything the first request finished with.
func TestInterceptReusedConnection(t *testing.T) {
	f := newInterceptFixture(t)
	client := f.trustingClient(t)

	for n := 1; n <= 2; n++ {
		resp, err := client.Get(f.upstream.URL + "/get")
		if err != nil {
			t.Fatalf("request %d: %v", n, err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("request %d reading body: %v", n, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status %d, body %q, want 200", n, resp.StatusCode, body)
		}
	}

	if hits := f.upstream.hits.Load(); hits != 2 {
		t.Fatalf("upstream saw %d requests, want 2", hits)
	}
}
