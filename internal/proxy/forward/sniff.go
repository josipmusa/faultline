package forward

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// tunnelKind is what a client actually put inside its CONNECT tunnel. CONNECT
// only asks for a byte pipe to a host and port; TLS is the convention, not a
// promise, and Node's native fetch tunnels its http:// calls in the clear.
type tunnelKind int

const (
	// kindTLS is a TLS record, which is what almost every client sends.
	kindTLS tunnelKind = iota
	// kindHTTP1 is an unencrypted HTTP/1 request.
	kindHTTP1
	// kindOther is anything Faultline cannot read.
	kindOther
)

// sniffLen is how much of the tunnel has to be seen to classify it: enough
// for the longest method Faultline recognises plus the space after it.
const sniffLen = len(http.MethodOptions) + 1

// errNothingSent reports a tunnel the client closed without putting a byte
// into it. That is what a browser does after a preconnect it turned out not to
// need, and what a pool does to an idle connection; nothing about the upstream
// or the CA was ever in play.
var errNothingSent = errors.New("client closed the tunnel without sending anything")

// sniffTunnel looks at the start of an open tunnel without consuming it, and
// reports what is inside. The returned connection serves those bytes again,
// so the caller hands it on as if nothing had read from it.
//
// A client has patience to send its first byte. One that hangs up first, or
// says nothing in time, is reported as an error and the tunnel is the caller's
// to close: there is nothing to classify and nobody to serve.
func sniffTunnel(client net.Conn, patience time.Duration) (tunnelKind, net.Conn, error) {
	r := bufio.NewReader(client)
	head, err := peekWithin(client, r, sniffLen, patience)
	if err != nil {
		return kindOther, nil, err
	}
	return classifyTunnel(head), &peekedConn{Conn: client, reader: r}, nil
}

// peekWithin is r.Peek(n) with a bound on how long the first byte may take.
// Fewer bytes than asked for still come back, with the same error Peek gives
// for them: a TLS record is recognised by its first byte, and anything too
// short to hold a request line is not one. No bytes at all is the error:
// errNothingSent for a client that hung up, the deadline for one that went
// quiet. The deadline is cleared again on the way out, so whoever serves the
// connection next starts with its own patience and not what is left of this.
func peekWithin(conn net.Conn, r *bufio.Reader, n int, patience time.Duration) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(patience)); err != nil {
		return nil, fmt.Errorf("bounding the wait for the client's first bytes: %w", err)
	}
	head, err := r.Peek(n)
	if clearErr := conn.SetReadDeadline(time.Time{}); clearErr != nil && err == nil {
		return nil, fmt.Errorf("clearing the read deadline: %w", clearErr)
	}
	if len(head) == 0 && err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errNothingSent
		}
		return nil, fmt.Errorf("waiting for the client's first bytes: %w", err)
	}
	return head, err
}

// tunnelMethods are the request methods that mark the start of a plaintext
// HTTP/1 request. The h2c preface (`PRI * HTTP/2.0`) deliberately is not one:
// Faultline cannot serve h2c, so it is better left to fail as an unreadable
// tunnel than misread as HTTP/1.
var tunnelMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
	http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions,
	http.MethodTrace,
}

// classifyTunnel names what head is the beginning of. A broken connection
// behind a readable head classifies by the head and fails downstream, where
// the reason is already reported.
func classifyTunnel(head []byte) tunnelKind {
	// A TLS record starts with the handshake content type, which no HTTP
	// method can begin with: methods are letters.
	if len(head) > 0 && head[0] == recordTypeHandshake {
		return kindTLS
	}
	for _, method := range tunnelMethods {
		if bytes.HasPrefix(head, []byte(method+" ")) {
			return kindHTTP1
		}
	}
	return kindOther
}

// recordTypeHandshake is the TLS content type every handshake begins with.
const recordTypeHandshake = 0x16

// peekedConn is a connection whose first bytes have already been read, and
// which serves them again before anything new.
type peekedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

// Unwrap names the connection underneath, so a reset finds the socket.
func (c *peekedConn) Unwrap() net.Conn { return c.Conn }
