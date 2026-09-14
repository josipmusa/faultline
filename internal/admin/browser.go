package admin

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// guardBrowsers is what stands between a web page and an admin port that has
// no authentication. Two checks, both aimed at a browser and invisible to
// anything else (curl, the CLI, the clients, an agent):
//
// Host. A page can defeat the same-origin policy by pointing a name it
// controls at 127.0.0.1 (DNS rebinding); its requests then arrive here with
// that name in Host and look same-origin to the browser. So while the server
// is bound to loopback, a Host that is not a loopback name is refused: it can
// only be a rebound one. --bind 0.0.0.0 is a choice to be reached by name from
// other machines and containers, so there the Host check stands down.
//
// Origin. A browser sends Origin on every cross-site request and on every
// POST, and a page cannot forge it. When it is there and is not this server's
// own address, the request came from another site, and is refused whether or
// not the browser would have let the page read the answer: a POST that
// creates a rule needs no answer.
func (s *Server) guardBrowsers(bindHost string) http.Handler {
	strict := isLoopbackName(bindHost)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := checkBrowser(r, strict); err != nil {
			s.fail(w, err)
			return
		}
		s.ServeHTTP(w, r)
	})
}

func checkBrowser(r *http.Request, strict bool) error {
	name := hostnameOf(r.Host)
	if name == "" {
		return forbidden("the request names no host")
	}
	if strict && !isLoopbackName(name) {
		return forbidden("%q is not a loopback name; the admin server is bound to localhost and answers only to localhost, 127.0.0.1 or ::1", name)
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) {
		return forbidden("cross-site request from origin %q refused; the admin API takes requests from its own pages and from tools, not from other sites", origin)
	}
	return nil
}

// isLoopbackName reports whether name can only mean this machine: localhost,
// a name under .localhost (which resolvers answer with loopback and never
// forward), or a loopback address literal. A name a stranger's DNS could point
// anywhere is none of these.
func isLoopbackName(name string) bool {
	name = strings.ToLower(name)
	if name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && ip.IsLoopback()
}

// hostnameOf strips the port and IPv6 brackets off a Host header.
func hostnameOf(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}

// forbidden reports a request the server will not take from where it came.
func forbidden(format string, args ...any) apiError {
	return apiError{Message: fmt.Sprintf(format, args...), status: http.StatusForbidden}
}
