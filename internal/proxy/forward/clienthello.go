package forward

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"
)

// recordHeaderLen is the TLS record header: content type, version, length.
const recordHeaderLen = 5

// maxClientHello bounds the buffer a ClientHello is read into: the largest a
// TLS record can be, plus its header. Post-quantum key shares have made a
// ClientHello several kilobytes, so the old assumption that one fits in a
// default bufio buffer no longer holds.
const maxClientHello = recordHeaderLen + 16384

// errEnoughOfTheHandshake stops the throwaway handshake once the ClientHello
// has been read. There is no upstream behind it and no certificate to present;
// only the name was ever wanted.
var errEnoughOfTheHandshake = errors.New("client hello read")

// peekClientHello reports the server name a client asked for, without
// consuming anything: the returned connection serves the ClientHello again, so
// the real handshake still sees it.
//
// It answers with an empty name whenever the name cannot be had - the
// connection does not begin with a TLS record, the client sent no SNI (which
// is what a client connecting to an IP address does), or the ClientHello is
// split across records. The caller falls back to the original destination,
// which is always known, so an empty name costs the identity of the upstream
// and never the connection.
//
// A client has patience to send its first byte. One that hangs up first, or
// says nothing in time, is an error rather than an empty name: there is no
// connection left worth serving.
func peekClientHello(conn net.Conn, patience time.Duration) (string, net.Conn, error) {
	r := bufio.NewReaderSize(conn, maxClientHello)
	out := &peekedConn{Conn: conn, reader: r}

	header, err := peekWithin(conn, r, recordHeaderLen, patience)
	if len(header) == 0 {
		return "", nil, err
	}
	// From here on a short read is an answer, not a failure: whatever the
	// client sent, it is not a ClientHello with a name in it.
	if len(header) < recordHeaderLen || header[0] != recordTypeHandshake {
		return "", out, nil
	}
	recordLen := recordHeaderLen + int(binary.BigEndian.Uint16(header[3:5]))
	record, _ := peekWithin(conn, r, recordLen, patience)
	if len(record) < recordLen {
		return "", out, nil
	}
	return serverNameIn(record), out, nil
}

// serverNameIn parses the record with the standard library's own handshake
// reader rather than a hand-written one: the config is asked which certificate
// to present, which is the point at which the ClientHello has been parsed, and
// it answers by stopping.
func serverNameIn(record []byte) string {
	var name string
	server := tls.Server(&replayConn{data: record}, &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			name = hello.ServerName
			return nil, errEnoughOfTheHandshake
		},
	})
	// The handshake is meant to fail; the name is what it was for.
	_ = server.HandshakeContext(context.Background())
	return name
}

// replayConn serves a recorded ClientHello to a handshake that will never be
// answered, and throws away whatever that handshake tries to say back.
type replayConn struct {
	data []byte
}

func (c *replayConn) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.data)
	c.data = c.data[n:]
	return n, nil
}

func (c *replayConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *replayConn) Close() error                     { return nil }
func (c *replayConn) LocalAddr() net.Addr              { return nowhere{} }
func (c *replayConn) RemoteAddr() net.Addr             { return nowhere{} }
func (c *replayConn) SetDeadline(time.Time) error      { return nil }
func (c *replayConn) SetReadDeadline(time.Time) error  { return nil }
func (c *replayConn) SetWriteDeadline(time.Time) error { return nil }

// nowhere is the address of a connection that is not one.
type nowhere struct{}

func (nowhere) Network() string { return "tcp" }
func (nowhere) String() string  { return "" }
