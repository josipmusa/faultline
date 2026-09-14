package admin

import (
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
)

// Config is what `GET /api/config` reports: whether a change made through
// the API outlives the process, and where it goes when it does. Persisted is
// the answer, not an empty Path: a persister that writes somewhere with no
// single path on disk would still not be running in memory.
type Config struct {
	Persisted bool   `json:"persisted"`
	Path      string `json:"path,omitempty"`

	// The three below say how to send a child process through this instance.
	// ProxyURL is what HTTP_PROXY points at, NoProxy the hosts the child should
	// reach directly, spelled the way NO_PROXY wants them, and Intercepting
	// whether HTTPS is terminated with the local CA. A child handed a CA the
	// proxy is not using fails every HTTPS call it makes, so the last one
	// decides whether the trust variables are set at all.
	ProxyURL     string   `json:"proxy_url,omitempty"`
	NoProxy      []string `json:"no_proxy,omitempty"`
	Intercepting bool     `json:"intercepting"`

	// Bypass is the bypass list as somebody configured it, without the entries
	// Faultline keeps for itself. It is what a file written from this instance
	// should say the list is; NoProxy above is what a child should be told.
	Bypass []string `json:"bypass,omitempty"`

	// Routes are the explicit routes, each an address to point a client at
	// that ignores proxy settings, and the upstream it stands in for.
	Routes []Route `json:"routes,omitempty"`
}

// Route is one explicit route as the config endpoint reports it: Addr is the
// URL a client sends its requests to, Upstream where they go.
type Route struct {
	Name     string `json:"name"`
	Addr     string `json:"addr"`
	Upstream string `json:"upstream"`
}

// ProxiesAt tells the server where the forward proxy ended up and whether it
// is intercepting HTTPS, so GET /api/config can answer an agent asking how to
// attach a child. A Server used as a plain handler is never told, and says so
// by reporting no proxy at all.
func (s *Server) ProxiesAt(proxyURL string, intercepting bool) {
	s.proxyURL, s.intercepting = proxyURL, intercepting
}

// RoutesAt tells the server which explicit routes are listening and where, so
// GET /api/config can name them. Routes are fixed for the life of the process.
func (s *Server) RoutesAt(routes []Route) {
	s.explicitRoutes = slices.Clone(routes)
}

// getConfig says whether Faultline is holding its rules in memory or writing
// them to a file, so the UI can say which of the two a change will do.
func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	info := Config{
		ProxyURL:     reachableProxyURL(s.proxyURL, r.Host),
		NoProxy:      s.bypass.NoProxy(),
		Intercepting: s.intercepting,
		Bypass:       s.bypass.Configured(),
		Routes:       reachableRoutes(s.explicitRoutes, r.Host),
	}
	if s.persist != nil {
		info.Persisted, info.Path = true, absolute(s.persist.Path())
	}
	s.writeJSON(w, http.StatusOK, info)
}

// reachableRoutes is the routes with each address rewritten the way
// reachableProxyURL rewrites the proxy's. Nil when there are none, so the
// field is left out rather than published empty.
func reachableRoutes(routes []Route, adminHost string) []Route {
	if len(routes) == 0 {
		return nil
	}
	out := make([]Route, 0, len(routes))
	for _, route := range routes {
		route.Addr = reachableProxyURL(route.Addr, adminHost)
		out = append(out, route)
	}
	return out
}

// reachableProxyURL is the proxy address as the caller can use it. A proxy
// bound to every interface reports itself as 0.0.0.0 or [::], which is where
// it listens and not somewhere anything can connect to. The caller reached the
// admin server by some name, and the proxy is listening on the same
// interfaces, so that name with the proxy's port is an address that works: the
// Compose service name from another container, localhost from the machine the
// ports are published on. A proxy bound to one address is reported as it is.
func reachableProxyURL(proxyURL, adminHost string) string {
	u, err := url.Parse(proxyURL)
	if err != nil || u.Host == "" {
		return proxyURL
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsUnspecified() {
		return proxyURL
	}
	host := adminHost
	if h, _, err := net.SplitHostPort(adminHost); err == nil {
		host = h
	}
	if host == "" {
		return proxyURL
	}
	u.Host = net.JoinHostPort(host, u.Port())
	return u.String()
}

// absolute resolves the file against the working directory. The banner can
// print what was typed, because a terminal knows where it is; this path is
// read in a browser, where a relative one names nothing. A path that will not
// resolve is reported as it stands, since saying where the file is badly beats
// not saying at all.
func absolute(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
