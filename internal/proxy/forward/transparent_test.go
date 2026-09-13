package forward

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
	"github.com/josipmusa/faultline/internal/tlsmitm"
)

// transparentFixture is a forward proxy with the two redirect listeners up and
// a stand-in for the kernel's original-destination lookup, so the transparent
// paths can be exercised on any platform: nothing here needs iptables, and
// every connection is a real one.
type transparentFixture struct {
	server   *Server
	store    *rules.Store
	recorder *events.Recorder
	ca       *tlsmitm.CA
	upstream *httptest.Server
}

// dst is what the fake lookup answers: the upstream's real address, which is
// where a redirect would genuinely have come from.
func (f *transparentFixture) dst(t *testing.T) netip.AddrPort {
	t.Helper()
	addr, err := netip.ParseAddrPort(upstreamHost(t, f.upstream))
	if err != nil {
		t.Fatalf("parsing the upstream address: %v", err)
	}
	return addr
}

func (f *transparentFixture) httpAddr() string  { return f.server.transparent[0].Addr().String() }
func (f *transparentFixture) httpsAddr() string { return f.server.transparent[1].Addr().String() }

func upstreamHost(t *testing.T, up *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatalf("parsing the upstream url: %v", err)
	}
	return u.Host
}

// toUpstream is a transport that dials the upstream whatever address it is
// asked for, which is what makes a name like api.example.test resolvable in a
// test the way it resolves inside a container.
func toUpstream(addr string, tlsConfig *tls.Config) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: tlsConfig,
	}
}

// transparentOpts is how a test varies the fixture. The lookup is settled
// before anything listens, because the proxy reads it from whatever goroutine
// accepted the connection.
type transparentOpts struct {
	intercept bool
	bypass    *Bypass
	// origDst replaces the original-destination lookup. Nil means the
	// upstream's own address, which is what a redirect would report.
	origDst func(net.Conn) (netip.AddrPort, error)
	// dstPort is the port that lookup reports, the upstream's own when it is
	// zero. A redirect only ever reports 80 or 443, because those are the only
	// ports redirected, and a test that cares what the event host looks like
	// has to say so.
	dstPort uint16
}

// newTransparentFixture brings up a proxy in transparent mode. intercept says
// whether a CA is loaded, which is the whole difference between the
// intercepted and the encrypted tier.
func newTransparentFixture(t *testing.T, opts transparentOpts) *transparentFixture {
	t.Helper()
	intercept, bypass := opts.intercept, opts.bypass

	f := &transparentFixture{store: rules.New()}
	f.recorder = events.NewRecorder(events.DefaultSize)
	t.Cleanup(f.recorder.Close)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "yes")
		_, _ = io.WriteString(w, "hello from "+r.Host)
	})

	var interceptor *Interceptor
	if intercept {
		f.upstream = httptest.NewTLSServer(handler)
		ca, err := tlsmitm.Create(t.TempDir())
		if err != nil {
			t.Fatalf("creating CA: %v", err)
		}
		issuer, err := tlsmitm.NewIssuer(ca)
		if err != nil {
			t.Fatalf("creating issuer: %v", err)
		}
		f.ca = ca

		roots := x509.NewCertPool()
		roots.AddCert(f.upstream.Certificate())
		upstreamTransport := toUpstream(upstreamHost(t, f.upstream), &tls.Config{
			RootCAs: roots,
			// The test upstream's certificate names example.com whatever name
			// the request was addressed to, so that is the name it is verified
			// against; a real upstream is verified against the name in the URL.
			ServerName: "example.com",
			MinVersion: tls.VersionTLS12,
		})
		t.Cleanup(upstreamTransport.CloseIdleConnections)

		pipeline := faults.New(upstreamTransport, f.store, f.recorder, events.TierIntercepted, nil)
		interceptor = NewInterceptor(issuer, pipeline, f.recorder, quietLogger())
	} else {
		f.upstream = httptest.NewServer(handler)
	}
	t.Cleanup(f.upstream.Close)

	plainTransport := toUpstream(upstreamHost(t, f.upstream), nil)
	t.Cleanup(plainTransport.CloseIdleConnections)
	plain := faults.New(plainTransport, f.store, f.recorder, events.TierPlain, nil)

	f.server = NewServer(plain, faults.NewDialer(f.store, f.recorder, nil), interceptor, bypass, quietLogger())
	f.server.origDst = opts.origDst
	if f.server.origDst == nil {
		f.server.origDst = func(net.Conn) (netip.AddrPort, error) {
			dst := f.dst(t)
			if opts.dstPort != 0 {
				dst = netip.AddrPortFrom(dst.Addr(), opts.dstPort)
			}
			return dst, nil
		}
	}

	if err := f.server.StartTransparent("127.0.0.1", 0, 0); err != nil {
		t.Fatalf("StartTransparent: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = f.server.Shutdown(ctx)
	})
	return f
}

func (f *transparentFixture) waitForEvent(t *testing.T) events.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if recorded := f.recorder.Events(); len(recorded) > 0 {
			return recorded[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no event was recorded")
	return events.Event{}
}

// A redirected plain request never says where it is going in its request line,
// the way a proxied one does. The Host header is the only name there is, and it
// has to be the name the event and the rules see.
func TestTransparentPlainRequestIsRecordedByItsHostHeader(t *testing.T) {
	f := newTransparentFixture(t, transparentOpts{})

	resp := transparentGet(t, f.httpAddr(), "api.example.test")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "api.example.test") {
		t.Errorf("the upstream saw a different Host: %q", body)
	}

	e := f.waitForEvent(t)
	if e.Host != "api.example.test" {
		t.Errorf("event host is %q, want api.example.test", e.Host)
	}
	if e.Tier != events.TierPlain {
		t.Errorf("tier is %q, want %q", e.Tier, events.TierPlain)
	}
}

// The point of the mode: a rule matching the host applies without the
// application having been told about Faultline at all.
func TestTransparentPlainRequestIsFaulted(t *testing.T) {
	f := newTransparentFixture(t, transparentOpts{})
	f.store.Replace([]rules.Rule{{
		ID:      "down",
		Enabled: true,
		Match:   rules.Match{Host: "api.example.test"},
		Fault:   rules.Fault{Type: "status", Params: rules.Params{"code": 503}},
	}})

	resp := transparentGet(t, f.httpAddr(), "api.example.test")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", resp.StatusCode)
	}
	if got := resp.Header.Get(faults.FaultHeader); got != "down" {
		t.Errorf("%s is %q, want down", faults.FaultHeader, got)
	}
	if e := f.waitForEvent(t); !e.Faulted {
		t.Errorf("event is not marked faulted: %+v", e)
	}
}

// A redirected TLS connection carries its name in the ClientHello, and with a
// CA that is the name the intercepted request is recorded under.
func TestTransparentTLSIsInterceptedUnderItsSNI(t *testing.T) {
	// 443 is what a redirect reports, and the port a transparent listener
	// never has to say out loud: the event is the host on its own.
	f := newTransparentFixture(t, transparentOpts{intercept: true, dstPort: 443})

	roots := x509.NewCertPool()
	roots.AddCert(f.ca.Cert)
	resp := transparentTLSGet(t, f.httpsAddr(), &tls.Config{
		ServerName: "api.example.test",
		RootCAs:    roots,
		MinVersion: tls.VersionTLS12,
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	e := f.waitForEvent(t)
	if e.Host != "api.example.test" {
		t.Errorf("event host is %q, want api.example.test", e.Host)
	}
	if e.Tier != events.TierIntercepted {
		t.Errorf("tier is %q, want %q", e.Tier, events.TierIntercepted)
	}
}

// Without a CA the connection is tunnelled blindly, and a client addressing an
// IP sends no SNI, so the only identity left is the original destination.
func TestTransparentTLSWithoutACAIsEncryptedAtItsOriginalDestination(t *testing.T) {
	f := newTransparentFixture(t, transparentOpts{})

	// The upstream is plain HTTP here, so the connection is piped rather than
	// spoken to; what matters is the event the dial recorded.
	conn, err := net.DialTimeout("tcp", f.httpsAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dialing the transparent https listener: %v", err)
	}
	defer func() { _ = conn.Close() }()
	// A ClientHello with no SNI, which is what a client talking to an address
	// sends.
	go func() { _ = tls.Client(conn, &tls.Config{InsecureSkipVerify: true}).Handshake() }() //nolint:gosec // no upstream to verify; the handshake is only there to produce a ClientHello

	e := f.waitForEvent(t)
	if e.Tier != events.TierEncrypted {
		t.Errorf("tier is %q, want %q", e.Tier, events.TierEncrypted)
	}
	if e.Host != faults.StripDefaultPort(upstreamHost(t, f.upstream)) {
		t.Errorf("event host is %q, want the original destination %q", e.Host, upstreamHost(t, f.upstream))
	}
}

// A bypassed host is passed through with nothing of Faultline's in the way,
// and the redirect is no reason to make an exception.
func TestTransparentTLSHonoursTheBypassList(t *testing.T) {
	bypass, err := NewBypass([]string{"example.com"})
	if err != nil {
		t.Fatalf("NewBypass: %v", err)
	}
	f := newTransparentFixture(t, transparentOpts{intercept: true, bypass: bypass})

	roots := x509.NewCertPool()
	roots.AddCert(f.upstream.Certificate())
	// Verifying against the upstream's own roots is the assertion: a leaf
	// Faultline had minted would not pass, so a handshake that succeeds proves
	// the connection was never terminated on the way.
	resp := transparentTLSGet(t, f.httpsAddr(), &tls.Config{
		ServerName: "example.com",
		RootCAs:    roots,
		MinVersion: tls.VersionTLS12,
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	// The upstream's own certificate was accepted, so nothing was intercepted.
	if recorded := f.recorder.Events(); len(recorded) != 0 {
		t.Errorf("a bypassed connection was recorded: %+v", recorded)
	}
}

// Without the original destination there is nowhere to forward to, and
// guessing would send an application's traffic somewhere nobody asked for.
func TestTransparentDropsAConnectionWithNoOriginalDestination(t *testing.T) {
	f := newTransparentFixture(t, transparentOpts{
		origDst: func(net.Conn) (netip.AddrPort, error) {
			return netip.AddrPort{}, errors.New("getsockopt: protocol not available")
		},
	})

	conn, err := net.DialTimeout("tcp", f.httpAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dialing the transparent listener: %v", err)
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("connection was not closed: %v", err)
	}
	if recorded := f.recorder.Events(); len(recorded) != 0 {
		t.Errorf("something was recorded for a connection going nowhere: %+v", recorded)
	}
}

// transparentGet sends an origin-form request, the way a client that believes
// it is talking to the upstream does.
func transparentGet(t *testing.T, addr, host string) *http.Response {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dialing the transparent listener: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: "+host+"\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("writing the request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("reading the response: %v", err)
	}
	return resp
}

// transparentTLSGet handshakes with the transparent listener as if it were the
// upstream, then sends one request inside.
func transparentTLSGet(t *testing.T, addr string, cfg *tls.Config) *http.Response {
	t.Helper()
	raw, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dialing the transparent https listener: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	_ = raw.SetDeadline(time.Now().Add(5 * time.Second))

	conn := tls.Client(raw, cfg)
	if err := conn.Handshake(); err != nil {
		t.Fatalf("handshaking with the transparent listener: %v", err)
	}
	req, err := http.NewRequest(http.MethodGet, "https://"+cfg.ServerName+"/", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	req.Close = true
	if err := req.Write(conn); err != nil {
		t.Fatalf("writing the request: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		t.Fatalf("reading the response: %v", err)
	}
	return resp
}
