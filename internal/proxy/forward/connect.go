package forward

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/josipmusa/faultline/internal/faults"
)

// tunnel serves a CONNECT request. With an interceptor, the tunnel is opened
// towards the client and the TLS inside it is terminated, so every request
// goes through the intercepted-tier pipeline. Without one, the upstream is
// dialed through the fault dialer, 200 is answered, and bytes are copied both
// ways until either side hangs up: the encrypted tier, where the host is known
// and everything else is opaque.
func (s *Server) tunnel(w http.ResponseWriter, r *http.Request) {
	addr := r.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		http.Error(w, "faultline: CONNECT needs a host:port target, like CONNECT api.stripe.com:443", http.StatusBadRequest)
		return
	}

	if s.bypassed(addr) {
		s.tunnelUntouched(w, r, addr)
		return
	}

	if s.intercept != nil {
		client, ok := s.open(w, addr)
		if !ok {
			return
		}
		defer func() { _ = client.Close() }()
		s.intercept.serve(r.Context(), client, addr)
		return
	}

	upstream, err := s.dialer.Dial(r.Context(), addr)
	if err != nil {
		s.answerDialError(w, addr, err)
		return
	}
	defer func() { _ = upstream.Close() }()

	client, ok := s.open(w, addr)
	if !ok {
		return
	}
	defer func() { _ = client.Close() }()

	pipe(client, upstream)
}

// tunnelUntouched is the bypass path for CONNECT: dial the upstream directly,
// no rules, no interception, no event, and copy bytes until one side is done.
func (s *Server) tunnelUntouched(w http.ResponseWriter, r *http.Request, addr string) {
	upstream, err := bareDialer.DialContext(r.Context(), "tcp", addr)
	if err != nil {
		s.answerDialError(w, addr, err)
		return
	}
	defer func() { _ = upstream.Close() }()

	client, ok := s.open(w, addr)
	if !ok {
		return
	}
	defer func() { _ = client.Close() }()

	pipe(client, upstream)
}

// bareDialer opens bypassed tunnels, with the same patience as the fault dialer
// and none of its rules.
var bareDialer = &net.Dialer{Timeout: 30 * time.Second}

// open takes over the client connection and tells the client its tunnel is
// ready. It reports false, having already answered, when that is not possible.
//
// The returned connection is the hijacked one with any bytes the client sent
// right behind its CONNECT put back in front of it, so callers read from a
// plain net.Conn and never from the reader Hijack handed over. That reader is
// still plumbed through the serving server's own connection reader, which
// reads a deliberate read-deadline abort as the client having gone away and
// cancels the context this request descends from; a tunnel outlives its
// CONNECT request, so it must not depend on that reader at all.
func (s *Server) open(w http.ResponseWriter, addr string) (net.Conn, bool) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		s.log.Error("cannot tunnel: response writer does not support hijacking", "upstream", addr)
		http.Error(w, "faultline: this server cannot open tunnels", http.StatusInternalServerError)
		return nil, false
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		s.log.Error("hijacking the client connection", "upstream", addr, "err", err)
		http.Error(w, "faultline: could not take over the connection", http.StatusInternalServerError)
		return nil, false
	}

	pending, err := drainBuffered(buffered.Reader)
	if err != nil {
		s.log.Error("reading what the client sent behind its CONNECT", "upstream", addr, "err", err)
		_ = client.Close()
		return nil, false
	}

	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		s.log.Debug("client went away before the tunnel opened", "upstream", addr, "err", err)
		_ = client.Close()
		return nil, false
	}
	return &bufferedConn{Conn: client, pending: pending}, true
}

// drainBuffered takes the bytes already sitting in r without reading anything
// new. bufio.Reader serves a read from its buffer whenever the buffer holds
// something, so asking for exactly what it has buffered never reaches the
// reader underneath.
func drainBuffered(r *bufio.Reader) ([]byte, error) {
	n := r.Buffered()
	if n == 0 {
		return nil, nil
	}
	pending := make([]byte, n)
	if _, err := io.ReadFull(r, pending); err != nil {
		return nil, fmt.Errorf("draining %d buffered bytes: %w", n, err)
	}
	return pending, nil
}

// bufferedConn is the hijacked client connection with the bytes that arrived
// behind the CONNECT served first.
type bufferedConn struct {
	net.Conn
	pending []byte
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// answerDialError tells the client why its tunnel did not open. A refusal by a
// rule is a synthetic answer and carries the rule id; a genuine dial failure
// carries nothing but the fact.
func (s *Server) answerDialError(w http.ResponseWriter, addr string, err error) {
	var refused *faults.RefusedError
	if errors.As(err, &refused) {
		w.Header().Set(faults.FaultHeader, refused.RuleID)
		http.Error(w, "faultline: "+refused.Error(), http.StatusBadGateway)
		return
	}
	s.log.Error("upstream unreachable", "upstream", addr, "method", http.MethodConnect, "err", err)
	http.Error(w, "faultline: upstream unreachable", http.StatusBadGateway)
}

// pipe copies bytes between the two ends of a tunnel until one of them closes,
// then closes the other so the remaining copy ends too.
func pipe(client, upstream net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()

	<-done
	_ = client.Close()
	_ = upstream.Close()
	<-done
}
