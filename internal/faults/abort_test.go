package faults

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
)

// dialLoopback returns the two ends of a real TCP connection, since a reset is
// something only a real socket can do.
func dialLoopback(t *testing.T) (client, server net.Conn) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	defer func() { _ = ln.Close() }()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			accepted <- nil
			return
		}
		accepted <- conn
	}()

	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dialing: %v", err)
	}
	server = <-accepted
	if server == nil {
		t.Fatal("nothing was accepted")
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client, server
}

func TestAbortResetsTheConnection(t *testing.T) {
	client, server := dialLoopback(t)

	Abort(server)

	if _, err := io.ReadAll(client); !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("reading a reset connection: %v, want ECONNRESET", err)
	}
}

func TestAbortUnwrapsToTheSocketUnderneath(t *testing.T) {
	client, server := dialLoopback(t)

	Abort(wrappedConn{Conn: server})

	if _, err := io.ReadAll(client); !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("reading a reset connection: %v, want ECONNRESET", err)
	}
}

// wrappedConn stands in for the layers a client connection wears by the time a
// fault sees it: a buffered connection, or a TLS one.
type wrappedConn struct{ net.Conn }

func (c wrappedConn) NetConn() net.Conn { return c.Conn }

func TestWithAbortLetsAFaultResetTheClient(t *testing.T) {
	reset := make(chan bool, 1)
	srv := httptest.NewServer(WithAbort(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		reset <- abortClient(r.Context())
	})))
	defer srv.Close()

	_, err := srv.Client().Get(srv.URL) //nolint:bodyclose // there is no response to close
	if err == nil {
		t.Fatal("the request succeeded, want a reset connection")
	}
	if !<-reset {
		t.Fatal("abortClient reported no aborter in the request context")
	}
}

func TestAbortClientWithoutAnAborterReportsFalse(t *testing.T) {
	if abortClient(t.Context()) {
		t.Fatal("abortClient reported an aborter where none was installed")
	}
}
