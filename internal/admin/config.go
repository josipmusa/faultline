package admin

import (
	"net/http"
	"path/filepath"
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
}

// ProxiesAt tells the server where the forward proxy ended up and whether it
// is intercepting HTTPS, so GET /api/config can answer an agent asking how to
// attach a child. A Server used as a plain handler is never told, and says so
// by reporting no proxy at all.
func (s *Server) ProxiesAt(proxyURL string, intercepting bool) {
	s.proxyURL, s.intercepting = proxyURL, intercepting
}

// getConfig says whether Faultline is holding its rules in memory or writing
// them to a file, so the UI can say which of the two a change will do.
func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	info := Config{
		ProxyURL:     s.proxyURL,
		NoProxy:      s.bypass.NoProxy(),
		Intercepting: s.intercepting,
	}
	if s.persist != nil {
		info.Persisted, info.Path = true, absolute(s.persist.Path())
	}
	s.writeJSON(w, http.StatusOK, info)
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
