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

// Covers reports whether every request other matches, m matches too. A rule
// whose match covers a later rule's, and which has no behavior to ever decline
// a request, takes every request the later rule was written for, so the later
// rule never fires. The answer is conservative: a pattern is only known to cover
// a path it matches outright or one written identically, so a rule this says is
// covered really is, while one it says nothing about may still be.
func (m Match) Covers(other Match) bool {
	if m.Host != "" && !strings.EqualFold(m.Host, other.Host) {
		return false
	}
	if m.Method != "" && !strings.EqualFold(m.Method, other.Method) {
		return false
	}
	if m.Path != "" && !coversPath(m.Path, other.Path) {
		return false
	}
	for name, want := range m.Header {
		if got, ok := headerValue(other.Header, name); !ok || got != want {
			return false
		}
	}
	return true
}

// coversPath reports whether every path other selects, pattern selects too.
// A literal other is covered when pattern matches it; a pattern is covered
// only by itself, since deciding whether one glob contains another is more
// than a warning is worth.
func coversPath(pattern, other string) bool {
	if other == "" {
		return false
	}
	if pattern == other {
		return true
	}
	return !strings.Contains(other, "*") && MatchPath(pattern, other)
}

// headerValue looks a header name up the way requests carry them, without
// regard to case.
func headerValue(header map[string]string, name string) (string, bool) {
	for k, v := range header {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return "", false
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
