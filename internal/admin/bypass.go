package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/josipmusa/faultline/internal/proxy/forward"
)

// bypassEntry is one host on the forward proxy's bypass list, as the API takes
// it and gives it back. The host comes back normalized, so a caller can see
// what the entry became.
type bypassEntry struct {
	Host string `json:"host"`
}

// addBypass puts a host on the list the forward proxy passes through
// untouched. Adding a host already there succeeds: the caller asked for it to
// be bypassed, and it is.
func (s *Server) addBypass(w http.ResponseWriter, r *http.Request) {
	entry, err := decodeBypass(w, r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if s.bypass == nil {
		s.fail(w, noForwardProxy())
		return
	}

	if err := s.change(func() error { return s.bypass.Add(entry.Host) }); err != nil {
		s.fail(w, s.bypassError(entry.Host, err))
		return
	}
	s.writeJSON(w, http.StatusCreated, bypassEntry{Host: normalizeHost(entry.Host)})
}

// removeBypass takes a host off the list, so its traffic is proxied and
// recorded again.
func (s *Server) removeBypass(w http.ResponseWriter, r *http.Request) {
	host, err := url.PathUnescape(r.PathValue("host"))
	if err != nil {
		s.fail(w, invalid("host", "host %q is not a valid path segment", r.PathValue("host")))
		return
	}
	if s.bypass == nil {
		s.fail(w, noForwardProxy())
		return
	}

	if err := s.change(func() error { return s.bypass.Remove(host) }); err != nil {
		s.fail(w, s.bypassError(host, err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// bypassError turns what a change to the list came back with into an answer.
// What the list itself rejected is the caller's to fix; what is left is the
// configuration file standing in the way, which saved explains.
func (s *Server) bypassError(host string, err error) error {
	switch {
	case errors.Is(err, forward.ErrNotBypassed):
		return missing("%s is not on the bypass list", normalizeHost(host))
	case errors.Is(err, forward.ErrBypassLocked):
		return conflict("host", "%s stays bypassed: Faultline reaches itself over loopback "+
			"and must never proxy it", normalizeHost(host))
	case isBadEntry(err):
		return invalid("host", "%v", err)
	}
	return s.saved(err)
}

// isBadEntry reports whether the bypass list rejected the entry itself, as
// opposed to something going wrong on the way to the file. The list's parse
// errors carry no sentinel, so what is known to be something else is ruled out
// instead.
func isBadEntry(err error) bool {
	var apiErr apiError
	return !errors.As(err, &apiErr) && !errors.Is(err, forward.ErrNotBypassed) &&
		!errors.Is(err, forward.ErrBypassLocked)
}

func noForwardProxy() error {
	return conflict("", "there is no forward proxy running, so there is no bypass list to change")
}

func decodeBypass(w http.ResponseWriter, r *http.Request) (bypassEntry, error) {
	var entry bypassEntry

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&entry); err != nil {
		if errors.Is(err, io.EOF) {
			return entry, invalid("", "body is empty, want an object with a host")
		}
		return entry, invalid("host", "body is not a bypass entry: %v", err)
	}
	if strings.TrimSpace(entry.Host) == "" {
		return entry, invalid("host", "host is required")
	}
	return entry, nil
}

func normalizeHost(host string) string { return strings.ToLower(strings.TrimSpace(host)) }
