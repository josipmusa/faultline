package forward

import (
	"io"
	"log"
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
)

// tlsUpstream is a TLS server that reports what it saw, plus a client that
// trusts it and sends everything through the proxy as CONNECT tunnels.
func (f *proxyFixture) tlsUpstream(t *testing.T) (*recordingUpstream, *http.Client) {
	t.Helper()
	up := &recordingUpstream{}
	up.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.hits.Add(1)
		up.lastHost.Store(r.Host)
		up.lastHeader.Store(r.Header.Clone())
		up.lastPath.Store(r.URL.RequestURI())
		w.Header().Set("X-Upstream", "yes")
		_, _ = io.WriteString(w, "hello over tls")
	}))
	up.Config.ErrorLog = log.New(io.Discard, "", 0) // a tunnel a rule cut is the point, not a failure
	t.Cleanup(up.Close)

	proxyURL, err := url.Parse(f.proxy.URL)
	if err != nil {
		t.Fatalf("parsing proxy url: %v", err)
	}
	client := up.Client() // trusts the test certificate
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("test client transport is %T, want *http.Transport", client.Transport)
	}
	transport.Proxy = http.ProxyURL(proxyURL)
	client.Timeout = 5 * time.Second
	t.Cleanup(transport.CloseIdleConnections)
	return up, client
}

func TestTunnelsHTTPSThroughConnect(t *testing.T) {
	f := newFixture(t)
	up, client := f.tlsUpstream(t)

	resp, err := client.Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("tunneled request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Upstream"); got != "yes" {
		t.Errorf("X-Upstream = %q, want the upstream's own headers, untouched", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello over tls" {
		t.Errorf("body = %q, want the upstream's body", body)
	}
	if got, want := up.lastPath.Load(), "/orders"; got != want {
		t.Errorf("upstream path = %v, want %q", got, want)
	}

	recorded := f.recorder.Events()
	if len(recorded) != 1 {
		t.Fatalf("recorded %d events, want 1 for the tunnel: %+v", len(recorded), recorded)
	}
	e := recorded[0]
	if e.Tier != events.TierEncrypted {
		t.Errorf("tier = %q, want %q", e.Tier, events.TierEncrypted)
	}
	if e.Method != http.MethodConnect || e.Path != "" {
		t.Errorf("method/path = %s %q, want CONNECT and no path", e.Method, e.Path)
	}
	if e.Host != up.Listener.Addr().String() {
		t.Errorf("host = %q, want %q", e.Host, up.Listener.Addr().String())
	}
	if e.Status != http.StatusOK || e.Faulted {
		t.Errorf("event = %+v, want status 200 and no fault", e)
	}
}

func TestDelaysATunnelForAHostOnlyDelayRule(t *testing.T) {
	f := newFixture(t)
	up, client := f.tlsUpstream(t)

	if err := f.store.Add(rules.Rule{
		ID:      "slow-tls",
		Enabled: true,
		Match:   rules.Match{Host: up.Listener.Addr().String()},
		Fault:   rules.Fault{Type: "delay", Params: rules.Params{"ms": 300}},
	}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}

	start := time.Now()
	resp, err := client.Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("tunneled request: %v", err)
	}
	_ = resp.Body.Close()

	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Errorf("request took %v, want at least the 300ms delay", elapsed)
	}
	recorded := f.recorder.Events()
	if len(recorded) != 1 || !recorded[0].Faulted || recorded[0].RuleID != "slow-tls" {
		t.Errorf("events = %+v, want one faulted by slow-tls", recorded)
	}
}

func TestRefusesATunnelWhenARuleSaysSo(t *testing.T) {
	f := newFixture(t)
	if err := f.store.Add(rules.Rule{
		ID:      "stripe-down",
		Enabled: true,
		Match:   rules.Match{Host: "api.stripe.invalid"},
		Fault:   rules.Fault{Type: "refuse"},
	}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}

	status, header, body := f.rawFull(t, "CONNECT api.stripe.invalid:443 HTTP/1.1\r\nHost: api.stripe.invalid:443\r\n\r\n")

	if status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502: the client hears what it would from an upstream that is not there", status)
	}
	if got := header.Get(faults.FaultHeader); got != "stripe-down" {
		t.Errorf("%s = %q, want the rule id on the synthetic answer", faults.FaultHeader, got)
	}
	if !strings.Contains(body, "refused") {
		t.Errorf("body = %q, want it to say the connection was refused", body)
	}

	recorded := f.recorder.Events()
	if len(recorded) != 1 || !recorded[0].Faulted || recorded[0].Tier != events.TierEncrypted {
		t.Errorf("events = %+v, want one faulted encrypted event", recorded)
	}
}

func TestAnswers502WhenTheTunnelTargetIsUnreachable(t *testing.T) {
	f := newFixture(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	closed := ln.Addr().String()
	_ = ln.Close()

	status, header, _ := f.rawFull(t, "CONNECT "+closed+" HTTP/1.1\r\nHost: "+closed+"\r\n\r\n")

	if status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", status)
	}
	if got := header.Get(faults.FaultHeader); got != "" {
		t.Errorf("%s = %q on a real failure, want none: Faultline did not cause this", faults.FaultHeader, got)
	}
}

func TestRejectsConnectWithoutAPort(t *testing.T) {
	f := newFixture(t)

	status, body := f.raw(t, "CONNECT example.com HTTP/1.1\r\nHost: example.com\r\n\r\n")

	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(body, "port") {
		t.Errorf("body = %q, want a message saying CONNECT needs host:port", body)
	}
}

// TestTunnelCarriesBytesSentBehindConnect writes the CONNECT request and the
// first bytes of what follows it in a single write, so both land in the
// serving server's read buffer together. Those bytes belong to the tunnel, and
// the tunnel has to deliver them rather than lose them with the reader they
// arrived in.
func TestTunnelCarriesBytesSentBehindConnect(t *testing.T) {
	f := newFixture(t)

	// A plain upstream, so what travels through the tunnel is readable here
	// and the test does not depend on TLS framing.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello through the tunnel")
	}))
	t.Cleanup(up.Close)
	upAddr := up.Listener.Addr().String()

	proxyURL, err := url.Parse(f.proxy.URL)
	if err != nil {
		t.Fatalf("parsing proxy url: %v", err)
	}
	conn, err := net.Dial("tcp", proxyURL.Host)
	if err != nil {
		t.Fatalf("dialing the proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting deadline: %v", err)
	}

	if _, err := io.WriteString(conn, "CONNECT "+upAddr+" HTTP/1.1\r\nHost: "+upAddr+"\r\n\r\n"+
		"GET /orders HTTP/1.1\r\nHost: "+upAddr+"\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("writing CONNECT and the request behind it: %v", err)
	}

	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("reading the tunnel: %v", err)
	}
	if !strings.Contains(string(got), "200 Connection established") {
		t.Fatalf("no tunnel was opened, got %q", got)
	}
	if !strings.Contains(string(got), "hello through the tunnel") {
		t.Errorf("the request sent behind CONNECT never reached the upstream, got %q", got)
	}
}
