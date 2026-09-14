package forward

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// helloWriter starts a TLS handshake against one end of a pipe and abandons
// it, which is all that is needed to put a real ClientHello on the wire.
func helloWriter(t *testing.T, cfg *tls.Config) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	go func() {
		_ = tls.Client(client, cfg).Handshake()
	}()
	return server
}

func TestPeekClientHelloReadsTheServerName(t *testing.T) {
	server := helloWriter(t, &tls.Config{ServerName: "api.stripe.com", MinVersion: tls.VersionTLS12})
	defer func() { _ = server.Close() }()

	name, _, err := peekClientHello(server, time.Second)
	if err != nil {
		t.Fatalf("peekClientHello: %v", err)
	}
	if name != "api.stripe.com" {
		t.Fatalf("server name is %q, want api.stripe.com", name)
	}
}

// A client connecting to an IP address sends no SNI, and there is nothing
// wrong with that: the caller falls back to the original destination.
func TestPeekClientHelloIsEmptyWithoutSNI(t *testing.T) {
	server := helloWriter(t, &tls.Config{ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12})
	defer func() { _ = server.Close() }()

	if name, _, err := peekClientHello(server, time.Second); name != "" || err != nil {
		t.Fatalf("server name is %q, err %v, want empty and no error", name, err)
	}
}

func TestPeekClientHelloLeavesTheHandshakeIntact(t *testing.T) {
	server := helloWriter(t, &tls.Config{ServerName: "api.stripe.com", MinVersion: tls.VersionTLS12})
	defer func() { _ = server.Close() }()

	name, peeked, err := peekClientHello(server, time.Second)
	if err != nil {
		t.Fatalf("peekClientHello: %v", err)
	}
	if name != "api.stripe.com" {
		t.Fatalf("server name is %q, want api.stripe.com", name)
	}

	// Everything the client sent is still there to be read, starting with the
	// record header the peek looked at.
	_ = peeked.SetReadDeadline(time.Now().Add(2 * time.Second))
	head := make([]byte, recordHeaderLen)
	if _, err := io.ReadFull(peeked, head); err != nil {
		t.Fatalf("reading the record back: %v", err)
	}
	if head[0] != recordTypeHandshake {
		t.Fatalf("first byte is %#x, want a handshake record", head[0])
	}
}

func TestPeekClientHelloIsEmptyForSomethingThatIsNotTLS(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	go func() { _, _ = client.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n")) }()

	if name, _, err := peekClientHello(server, time.Second); name != "" || err != nil {
		t.Fatalf("server name is %q, err %v, want empty and no error", name, err)
	}
}

// tcpPair is a connected loopback socket pair. net.Pipe will not do here: it
// refuses a deadline once its peer is closed, where a real socket accepts one
// and reports the close on the next read, which is the case under test.
func tcpPair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	defer func() { _ = ln.Close() }()
	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dialing: %v", err)
	}
	server, err = ln.Accept()
	if err != nil {
		t.Fatalf("accepting: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client, server
}

func TestPeekClientHelloReportsAClientThatHangsUpFirst(t *testing.T) {
	client, server := tcpPair(t)
	_ = client.Close()

	if _, _, err := peekClientHello(server, time.Second); !errors.Is(err, errNothingSent) {
		t.Fatalf("err = %v, want errNothingSent", err)
	}
}

func TestPeekClientHelloGivesUpOnASilentClient(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })

	_, _, err := peekClientHello(server, 50*time.Millisecond)
	if err == nil || errors.Is(err, errNothingSent) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("err = %v, want it to wrap the deadline", err)
	}
}
