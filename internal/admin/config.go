package admin

import (
	"net/http"
	"path/filepath"
)

// configInfo is what `GET /api/config` reports: whether a change made through
// the API outlives the process, and where it goes when it does. Persisted is
// the answer, not an empty Path: a persister that writes somewhere with no
// single path on disk would still not be running in memory.
type configInfo struct {
	Persisted bool   `json:"persisted"`
	Path      string `json:"path,omitempty"`
}

// getConfig says whether Faultline is holding its rules in memory or writing
// them to a file, so the UI can say which of the two a change will do.
func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	info := configInfo{}
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
