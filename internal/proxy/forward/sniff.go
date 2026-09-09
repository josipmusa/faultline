package forward

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
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

// sniffTunnel looks at the start of an open tunnel without consuming it, and
// reports what is inside. The returned connection serves those bytes again,
// so the caller hands it on as if nothing had read from it.
//
// A client that opens a tunnel and sends nothing blocks here, exactly as it
// blocks in the TLS handshake that used to come first.
func sniffTunnel(client net.Conn) (tunnelKind, net.Conn) {
	r := bufio.NewReader(client)
	head, err := r.Peek(sniffLen)
	// Fewer bytes than asked for still classify: a TLS record is recognised
	// by its first byte, and anything too short to hold a request line is not
	// one. A broken connection classifies as unreadable and fails downstream,
	// where the reason is already reported.
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return kindOther, &peekedConn{Conn: client, reader: r}
	}
	return classifyTunnel(head), &peekedConn{Conn: client, reader: r}
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

// classifyTunnel names what head is the beginning of.
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
