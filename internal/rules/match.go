package rules

import (
	"net/http"
	"strings"
)

// Matches reports whether the rule applies to a request with the given
// upstream host, method and path. A disabled rule never matches. The caller
// decides how the host is derived, because that differs per proxy mode.
func (r Rule) Matches(host, method, path string, header http.Header) bool {
	if !r.Enabled {
		return false
	}
	if r.Match.Host != "" && !strings.EqualFold(r.Match.Host, host) {
		return false
	}
	if r.Match.Method != "" && !strings.EqualFold(r.Match.Method, method) {
		return false
	}
	if r.Match.Path != "" && !MatchPath(r.Match.Path, path) {
		return false
	}
	for name, want := range r.Match.Header {
		if header.Get(name) != want {
			return false
		}
	}
	return true
}

// MatchesConnection reports whether the rule applies to a connection to the
// given host on which nothing else can be seen. Only a rule that asks for
// nothing but a host can say yes: a method, path or header condition can
// neither be checked nor assumed, so such a rule stays out of the way.
func (r Rule) MatchesConnection(host string) bool {
	if !r.Enabled || !r.Match.hostOnly() {
		return false
	}
	return r.Match.Host == "" || strings.EqualFold(r.Match.Host, host)
}

// hostOnly reports whether the match constrains nothing but the host.
func (m Match) hostOnly() bool {
	return m.Method == "" && m.Path == "" && len(m.Header) == 0
}

// MatchPath reports whether path satisfies the glob pattern, which must match
// the whole path. `*` matches any run of characters within one path segment,
// `**` matches any run of characters including `/`. Everything else is literal.
func MatchPath(pattern, path string) bool {
	for {
		switch {
		case pattern == "":
			return path == ""

		case strings.HasPrefix(pattern, "**"):
			rest := pattern[2:]
			for i := 0; i <= len(path); i++ {
				if MatchPath(rest, path[i:]) {
					return true
				}
			}
			return false

		case pattern[0] == '*':
			rest := pattern[1:]
			for i := 0; i <= len(path); i++ {
				if i > 0 && path[i-1] == '/' {
					break // `*` does not cross a segment boundary
				}
				if MatchPath(rest, path[i:]) {
					return true
				}
			}
			return false

		default:
			if path == "" || path[0] != pattern[0] {
				return false
			}
			pattern, path = pattern[1:], path[1:]
		}
	}
}
