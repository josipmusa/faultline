package forward

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/events"
	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
)

func TestClassifyTunnel(t *testing.T) {
	cases := []struct {
		name  string
		first string
		want  tunnelKind
	}{
		{"tls handshake record", "\x16\x03\x01\x02\x00\x01", kindTLS},
		{"get", "GET /orders HTTP/1.1\r\n", kindHTTP1},
		{"post", "POST /orders HTTP/1.1\r\n", kindHTTP1},
		{"options, the longest method", "OPTIONS * HTTP/1.1\r\n", kindHTTP1},
		{"connect, tunnel through a tunnel", "CONNECT host:443 HTTP/1.1\r\n", kindHTTP1},
		{"h2c preface is not http/1", "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n", kindOther},
		{"a method-like token that is not one", "GETTY /x HTTP/1.1\r\n", kindOther},
		{"a method with no space after it", "GET/orders HTTP/1.1\r\n", kindOther},
		{"lowercase is not a method", "get /orders HTTP/1.1\r\n", kindOther},
		{"some other protocol entirely", "SSH-2.0-OpenSSH_9.0\r\n", kindOther},
		{"too few bytes to tell", "GE", kindOther},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyTunnel([]byte(c.first)); got != c.want {
				t.Errorf("classifyTunnel(%q) = %v, want %v", c.first, got, c.want)
			}
		})
	}
}

// tunnelTo opens a CONNECT tunnel to target through the proxy and hands back
// the raw connection, the way a client that tunnels everything does. Nothing
// has been written into the tunnel yet, so the caller decides what is inside.
func (f *interceptFixture) tunnelTo(t *testing.T, target string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(f.proxy.URL, "http://"), 5*time.Second)
	if err != nil {
		t.Fatalf("dialing the proxy: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting a deadline: %v", err)
	}

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("sending CONNECT: %v", err)
	}
	return conn
}

// getThroughTunnel writes one plaintext request into an open tunnel and reads
// the response back off it, so the same connection can be used again.
func getThroughTunnel(t *testing.T, conn net.Conn, r *bufio.Reader, host, path string) *http.Response {
	t.Helper()
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\n\r\n", path, host)
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("sending the tunnelled request: %v", err)
	}
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatalf("reading the tunnelled response: %v", err)
	}
	return resp
}

// readTunnelOpened consumes the proxy's answer to the CONNECT.
func readTunnelOpened(t *testing.T, r *bufio.Reader) {
	t.Helper()
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatalf("reading the CONNECT response: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT answered %d, want 200", resp.StatusCode)
	}
}

// plainUpstream is a plain HTTP upstream the intercepting proxy can reach, for
// the clients that tunnel their http:// calls instead of sending them in
// absolute form.
func (f *interceptFixture) plainUpstream(t *testing.T) *recordingUpstream {
	t.Helper()
	return newUpstream(t)
}

// Node's native fetch tunnels http:// through CONNECT, so an intercepted
// tunnel can hold an ordinary unencrypted request. It is fully visible, so it
// gets the plain tier and the whole response-fault pipeline.
func TestTunnelledPlaintextIsServedAsPlainHTTP(t *testing.T) {
	f := newInterceptFixture(t)
	up := f.plainUpstream(t)
	target := up.Listener.Addr().String()

	conn := f.tunnelTo(t, target)
	r := bufio.NewReader(conn)
	readTunnelOpened(t, r)

	resp := getThroughTunnel(t, conn, r, target, "/orders?id=7")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Upstream"); got != "yes" {
		t.Errorf("X-Upstream = %q, want the upstream's own headers", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from upstream" {
		t.Errorf("body = %q, want the upstream's body", body)
	}
	if got := up.hits.Load(); got != 1 {
		t.Errorf("upstream hits = %d, want 1", got)
	}
	if got, want := up.lastPath.Load(), "/orders?id=7"; got != want {
		t.Errorf("upstream saw %v, want %q", got, want)
	}

	e := f.waitForEvent(t)
	if e.Tier != events.TierPlain {
		t.Errorf("tier = %q, want %q: nothing was encrypted, so nothing was intercepted", e.Tier, events.TierPlain)
	}
	if e.Method != http.MethodGet || e.Path != "/orders" {
		t.Errorf("method/path = %s %q, want GET /orders", e.Method, e.Path)
	}
	if e.Host != target {
		t.Errorf("host = %q, want %q", e.Host, target)
	}
	if e.Status != http.StatusOK || e.Faulted || e.Error != "" {
		t.Errorf("event = %+v, want status 200, no fault, no error", e)
	}
}

// undici keeps the tunnel open and sends the next request down it, which is
// where the interception path broke once before.
func TestTunnelledPlaintextSurvivesAReusedConnection(t *testing.T) {
	f := newInterceptFixture(t)
	up := f.plainUpstream(t)
	target := up.Listener.Addr().String()

	conn := f.tunnelTo(t, target)
	r := bufio.NewReader(conn)
	readTunnelOpened(t, r)

	for n := 1; n <= 3; n++ {
		resp := getThroughTunnel(t, conn, r, target, fmt.Sprintf("/call/%d", n))
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("call %d: status = %d, want 200", n, resp.StatusCode)
		}
		if string(body) != "hello from upstream" {
			t.Fatalf("call %d: body = %q, want the upstream's body", n, body)
		}
	}

	if got := up.hits.Load(); got != 3 {
		t.Errorf("upstream hits = %d, want 3", got)
	}
	if got := len(f.recorder.Events()); got != 3 {
		t.Errorf("recorded %d events, want 3, one per request", got)
	}
}

func TestTunnelledPlaintextGetsResponseFaults(t *testing.T) {
	f := newInterceptFixture(t)
	up := f.plainUpstream(t)
	target := up.Listener.Addr().String()
	if err := f.store.Add(rules.Rule{
		ID:      "orders-down",
		Enabled: true,
		Match:   rules.Match{Host: target, Path: "/orders"},
		Fault:   rules.Fault{Type: rules.FaultStatus, Code: http.StatusServiceUnavailable},
	}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}

	conn := f.tunnelTo(t, target)
	r := bufio.NewReader(conn)
	readTunnelOpened(t, r)

	resp := getThroughTunnel(t, conn, r, target, "/orders")
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 from the rule", resp.StatusCode)
	}
	if got := resp.Header.Get(faults.FaultHeader); got != "orders-down" {
		t.Errorf("%s = %q, want the rule id", faults.FaultHeader, got)
	}
	if got := up.hits.Load(); got != 0 {
		t.Errorf("upstream hits = %d, want 0: a status fault answers without forwarding", got)
	}
}

// Bytes that are neither TLS nor an HTTP/1 request line keep the behaviour
// they had before sniffing existed: the interceptor tries to handshake and
// says plainly why it could not.
func TestTunnelWithUnrecognizedBytesReportsTheHandshakeFailure(t *testing.T) {
	f := newInterceptFixture(t)
	up := f.plainUpstream(t)
	target := up.Listener.Addr().String()

	conn := f.tunnelTo(t, target)
	r := bufio.NewReader(conn)
	readTunnelOpened(t, r)

	if _, err := io.WriteString(conn, "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"); err != nil {
		t.Fatalf("writing into the tunnel: %v", err)
	}

	e := f.waitForEvent(t)
	if e.Tier != events.TierIntercepted {
		t.Errorf("tier = %q, want %q", e.Tier, events.TierIntercepted)
	}
	if !strings.Contains(e.Error, "TLS handshake") {
		t.Errorf("error = %q, want it to name the failed handshake", e.Error)
	}
	if got := up.hits.Load(); got != 0 {
		t.Errorf("upstream hits = %d, want 0", got)
	}
}
