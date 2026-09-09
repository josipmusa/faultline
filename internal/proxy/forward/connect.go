package forward

import (
	"bufio"
	"errors"
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
		client, buffered, ok := s.open(w, addr)
		if !ok {
			return
		}
		defer func() { _ = client.Close() }()
		s.intercept.serve(r.Context(), client, buffered, addr)
		return
	}

	upstream, err := s.dialer.Dial(r.Context(), addr)
	if err != nil {
		s.answerDialError(w, addr, err)
		return
	}
	defer func() { _ = upstream.Close() }()

	client, buffered, ok := s.open(w, addr)
	if !ok {
		return
	}
	defer func() { _ = client.Close() }()

	pipe(client, buffered, upstream)
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

	client, buffered, ok := s.open(w, addr)
	if !ok {
		return
	}
	defer func() { _ = client.Close() }()

	pipe(client, buffered, upstream)
}

// bareDialer opens bypassed tunnels, with the same patience as the fault dialer
// and none of its rules.
var bareDialer = &net.Dialer{Timeout: 30 * time.Second}

// open takes over the client connection and tells the client its tunnel is
// ready. It reports false, having already answered, when that is not possible.
// The reader may hold bytes the client sent right behind its CONNECT, so
// callers read through it rather than from the connection.
func (s *Server) open(w http.ResponseWriter, addr string) (net.Conn, *bufio.Reader, bool) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		s.log.Error("cannot tunnel: response writer does not support hijacking", "upstream", addr)
		http.Error(w, "faultline: this server cannot open tunnels", http.StatusInternalServerError)
		return nil, nil, false
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		s.log.Error("hijacking the client connection", "upstream", addr, "err", err)
		http.Error(w, "faultline: could not take over the connection", http.StatusInternalServerError)
		return nil, nil, false
	}

	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		s.log.Debug("client went away before the tunnel opened", "upstream", addr, "err", err)
		_ = client.Close()
		return nil, nil, false
	}
	return client, buffered.Reader, true
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
// then closes the other so the remaining copy ends too. clientReader is used in
// place of client for reading, because the server may already have buffered
// bytes the client sent right behind its CONNECT.
func pipe(client net.Conn, clientReader *bufio.Reader, upstream net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, clientReader)
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
