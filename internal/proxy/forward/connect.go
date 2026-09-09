package forward

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/josipmusa/faultline/internal/faults"
)

// tunnel serves a CONNECT request: it opens the upstream connection through the
// fault dialer, answers 200, and then copies bytes both ways until either side
// hangs up. Nothing inside the tunnel is read, so this is the encrypted tier:
// the host is known and everything else is opaque.
func (s *Server) tunnel(w http.ResponseWriter, r *http.Request) {
	addr := r.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		http.Error(w, "faultline: CONNECT needs a host:port target, like CONNECT api.stripe.com:443", http.StatusBadRequest)
		return
	}

	upstream, err := s.dialer.Dial(r.Context(), addr)
	if err != nil {
		s.answerDialError(w, addr, err)
		return
	}
	defer func() { _ = upstream.Close() }()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		s.log.Error("cannot tunnel: response writer does not support hijacking", "upstream", addr)
		http.Error(w, "faultline: this server cannot open tunnels", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		s.log.Error("hijacking the client connection", "upstream", addr, "err", err)
		http.Error(w, "faultline: could not take over the connection", http.StatusInternalServerError)
		return
	}
	defer func() { _ = client.Close() }()

	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		s.log.Debug("client went away before the tunnel opened", "upstream", addr, "err", err)
		return
	}

	pipe(client, buffered.Reader, upstream)
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
