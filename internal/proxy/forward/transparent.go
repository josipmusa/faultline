package forward

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"

	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/transparent"
)

// TransparentHTTPPort and TransparentHTTPSPort are where redirected outbound
// 80 and 443 arrive. They are not ports anybody points a client at: the
// packet filter sends traffic here, and the client goes on believing it is
// talking to the upstream.
const (
	TransparentHTTPPort  = 9002
	TransparentHTTPSPort = 9003
)

// StartTransparent binds the two transparent listeners and serves them in the
// background. Nothing reaches them until the redirect rules are installed,
// which is the caller's to do and deliberately happens after this: a rule that
// points at a port nothing is listening on breaks every outbound call in the
// namespace.
func (s *Server) StartTransparent(host string, httpPort, httpsPort int) error {
	s.mu.Lock()
	if s.tcancel == nil {
		s.tctx, s.tcancel = context.WithCancel(context.Background())
	}
	ctx := s.tctx
	s.mu.Unlock()

	for _, l := range []struct {
		port int
		tls  bool
	}{{httpPort, false}, {httpsPort, true}} {
		addr := net.JoinHostPort(host, strconv.Itoa(l.port))
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("transparent listener on %s: %w", addr, err)
		}

		s.mu.Lock()
		s.transparent = append(s.transparent, ln)
		s.mu.Unlock()

		s.log.Info("transparent listener", "addr", ln.Addr().String(), "carries", carries(l.tls))
		s.wg.Go(func() { s.acceptTransparent(ctx, ln, l.tls) })
	}
	return nil
}

func carries(isTLS bool) string {
	if isTLS {
		return "redirected https"
	}
	return "redirected http"
}

// acceptTransparent serves one transparent listener until it is closed.
func (s *Server) acceptTransparent(ctx context.Context, ln net.Listener, isTLS bool) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.log.Error("transparent listener stopped", "addr", ln.Addr().String(), "err", err)
			}
			return
		}
		go s.serveTransparent(ctx, conn, isTLS)
	}
}

// serveTransparent handles one redirected connection. Where it was going is
// the first thing to establish, because nothing in the connection itself says
// so: the client addressed the upstream, and the kernel quietly sent it here.
func (s *Server) serveTransparent(ctx context.Context, conn net.Conn, isTLS bool) {
	defer func() { _ = conn.Close() }()

	dst, err := s.originalDst(conn)
	if err != nil {
		// Without the destination there is nothing to forward to and no
		// honest way to guess, so the connection is dropped and said so.
		s.log.Error("dropping a redirected connection", "from", conn.RemoteAddr().String(), "err", err)
		return
	}

	if isTLS {
		s.transparentTLS(ctx, conn, dst)
		return
	}
	s.transparentHTTP(ctx, conn, dst)
}

// originalDst is the lookup, through whatever the Server was given: the real
// syscall normally, and a stand-in in tests, which is what lets the transparent
// paths be tested anywhere rather than only on Linux.
func (s *Server) originalDst(conn net.Conn) (netip.AddrPort, error) {
	if s.origDst != nil {
		return s.origDst(conn)
	}
	return transparent.OriginalDst(conn)
}

// transparentHTTP serves redirected plaintext requests. They arrive in origin
// form, as they would at the upstream itself, so the destination is put back
// into the request line and the connection is then served exactly as one that
// arrived at the forward proxy in absolute form: same pipeline, same bypass
// list, same tier.
func (s *Server) transparentHTTP(ctx context.Context, conn net.Conn, dst netip.AddrPort) {
	serveOneConn(ctx, conn, s.transparentHandler(dst.String()), s.log)
}

func (s *Server) transparentHandler(dst string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The Host header is the upstream's name, and the name is what rules
		// match on. A request without one - HTTP/1.0, or a client talking to
		// an address - leaves the original destination as the only identity
		// there is.
		host := r.Host
		if host == "" {
			host = dst
		}
		r.URL.Scheme = "http"
		r.URL.Host = host
		s.ServeHTTP(w, r)
	})
}

// transparentTLS serves a redirected TLS connection, which is the CONNECT
// path with the CONNECT taken away: the name comes from the ClientHello
// instead of a request line, and there is no client waiting to be told its
// tunnel is open, because as far as the client is concerned it already has
// one.
func (s *Server) transparentTLS(ctx context.Context, conn net.Conn, dst netip.AddrPort) {
	name, client := peekClientHello(conn)
	target := dst.String()
	if name != "" {
		target = net.JoinHostPort(name, strconv.Itoa(int(dst.Port())))
	}

	if s.bypassed(target) {
		s.pipeUntouched(ctx, client, dst.String())
		return
	}

	if s.intercept != nil {
		s.intercept.serve(ctx, client, target)
		return
	}

	upstream, err := s.dialer.Dial(ctx, target)
	if err != nil {
		// There is no response to write on a connection the client believes
		// is a socket to its upstream, so a refusal reaches it as the broken
		// connection its upstream would have given it.
		s.logDialFailure(target, err)
		return
	}
	defer func() { _ = upstream.Close() }()

	pipe(client, upstream)
}

// pipeUntouched is the bypass path for a redirected connection: straight to
// the original destination, no rules, no interception, no event. The address
// is used rather than the name, because a bypassed connection should not even
// depend on Faultline resolving it.
func (s *Server) pipeUntouched(ctx context.Context, client net.Conn, dst string) {
	upstream, err := bareDialer.DialContext(ctx, "tcp", dst)
	if err != nil {
		s.log.Error("upstream unreachable", "upstream", dst, "err", err)
		return
	}
	defer func() { _ = upstream.Close() }()
	pipe(client, upstream)
}

// logDialFailure says why a redirected connection got nowhere, separating a
// rule that refused it from an upstream that really is unreachable.
func (s *Server) logDialFailure(target string, err error) {
	var refused *faults.RefusedError
	switch {
	case errors.Is(err, faults.ErrClientReset):
		s.log.Debug("a rule cut the connection", "upstream", target)
	case errors.As(err, &refused):
		s.log.Debug("a rule refused the connection", "upstream", target, "rule", refused.RuleID)
	case errors.Is(err, context.Canceled):
		s.log.Debug("client gave up before the connection opened", "upstream", target)
	default:
		s.log.Error("upstream unreachable", "upstream", target, "err", err)
	}
}

// stopTransparent closes the transparent listeners and cancels the context the
// connections they accepted descend from.
func (s *Server) stopTransparent() {
	s.mu.Lock()
	listeners, cancel := s.transparent, s.tcancel
	s.transparent, s.tcancel, s.tctx = nil, nil, nil
	s.mu.Unlock()

	for _, ln := range listeners {
		_ = ln.Close()
	}
	if cancel != nil {
		cancel()
	}
}
