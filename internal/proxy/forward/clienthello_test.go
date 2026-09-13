package forward

import (
	"crypto/tls"
	"io"
	"net"
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

	name, _ := peekClientHello(server)
	if name != "api.stripe.com" {
		t.Fatalf("server name is %q, want api.stripe.com", name)
	}
}

// A client connecting to an IP address sends no SNI, and there is nothing
// wrong with that: the caller falls back to the original destination.
func TestPeekClientHelloIsEmptyWithoutSNI(t *testing.T) {
	server := helloWriter(t, &tls.Config{ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12})
	defer func() { _ = server.Close() }()

	if name, _ := peekClientHello(server); name != "" {
		t.Fatalf("server name is %q, want empty", name)
	}
}

func TestPeekClientHelloLeavesTheHandshakeIntact(t *testing.T) {
	server := helloWriter(t, &tls.Config{ServerName: "api.stripe.com", MinVersion: tls.VersionTLS12})
	defer func() { _ = server.Close() }()

	name, peeked := peekClientHello(server)
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

	if name, _ := peekClientHello(server); name != "" {
		t.Fatalf("server name is %q, want empty", name)
	}
}
