package forward

import (
	"cmp"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/josipmusa/faultline/internal/faults"
)

// DefaultBypass is the list every forward proxy starts from: loopback in all
// its spellings, so Faultline never proxies itself and local services stay
// out of the way, the same as the NO_PROXY the run wrapper hands its child.
var DefaultBypass = []string{"localhost", "127.0.0.1", "::1"}

// Bypass is the list of hosts the forward proxy passes through untouched: no
// rules, no events, no interception, in the manner of NO_PROXY. It also
// remembers which of those hosts it has actually seen, so the upstreams API
// can say why nothing is being recorded for them.
//
// An entry is a host (`httpbin.org`), a host and port (`localhost:9000`), or
// a domain suffix written `*.internal` or `.internal`. Hosts compare
// case-insensitively; an entry with no port covers every port; default ports
// are dropped from both sides the way rules and events drop them.
//
// A nil *Bypass bypasses nothing.
type Bypass struct {
	patterns []pattern

	mu   sync.Mutex
	seen map[string]*Bypassed
}

// Bypassed is one host the forward proxy passed through untouched.
type Bypassed struct {
	Host     string    `json:"host"`
	Requests int       `json:"requests"`
	LastSeen time.Time `json:"last_seen"`
}

// pattern is one parsed bypass entry.
type pattern struct {
	text   string // normalized, for reporting
	name   string // lowercase host, or the suffix after `*.`
	port   string // empty means any port
	suffix bool   // name is a suffix: match subdomains, not the apex
}

// NewBypass parses the entries. It fails on the first one that is empty,
// misuses the wildcard, or names a port that is not one; the error names the
// entry and leaves the caller to say where it came from.
func NewBypass(hosts []string) (*Bypass, error) {
	b := &Bypass{seen: make(map[string]*Bypassed)}
	for _, raw := range hosts {
		p, err := parsePattern(raw)
		if err != nil {
			return nil, err
		}
		b.patterns = append(b.patterns, p)
	}
	return b, nil
}

func parsePattern(raw string) (pattern, error) {
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return pattern{}, fmt.Errorf("an entry is empty")
	}

	p := pattern{text: text}
	rest := text
	switch {
	case strings.HasPrefix(rest, "*."):
		p.suffix, rest = true, rest[2:]
	case strings.HasPrefix(rest, "."):
		p.suffix, rest = true, rest[1:]
		p.text = "*" + text // report both spellings the same way
	}
	if rest == "" || strings.Contains(rest, "*") {
		return pattern{}, fmt.Errorf("%q: a wildcard can only stand in for the subdomains of a name, like *.internal", text)
	}

	name, port, err := net.SplitHostPort(rest)
	if err != nil {
		name = rest // no port, or a bare IPv6 address
	} else {
		n, convErr := strconv.Atoi(port)
		if convErr != nil || n < 1 || n > 65535 {
			return pattern{}, fmt.Errorf("%q: port %q is not a number between 1 and 65535", text, port)
		}
		if port == "80" || port == "443" {
			port = "" // default ports are dropped from hosts, so they mean "any" here
			p.text = faults.StripDefaultPort(text)
		}
	}
	p.name, p.port = name, port
	return p, nil
}

// Matches reports whether host, as the proxies name upstreams (default port
// dropped), is on the list.
func (b *Bypass) Matches(host string) bool {
	if b == nil {
		return false
	}
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		name = host
	}
	name = strings.ToLower(name)
	for _, p := range b.patterns {
		if p.matches(name, port) {
			return true
		}
	}
	return false
}

func (p pattern) matches(name, port string) bool {
	if p.port != "" && p.port != port {
		return false
	}
	if p.suffix {
		return strings.HasSuffix(name, "."+p.name)
	}
	return name == p.name
}

// Saw records that a request for host was passed through.
func (b *Bypass) Saw(host string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	u, ok := b.seen[host]
	if !ok {
		u = &Bypassed{Host: host}
		b.seen[host] = u
	}
	u.Requests++
	u.LastSeen = time.Now()
}

// Seen lists the bypassed hosts that have actually been requested, sorted by
// host.
func (b *Bypass) Seen() []Bypassed {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Bypassed, 0, len(b.seen))
	for _, u := range b.seen {
		out = append(out, *u)
	}
	slices.SortFunc(out, func(a, b Bypassed) int { return cmp.Compare(a.Host, b.Host) })
	return out
}

// Patterns returns the entries as they were understood, for telling the
// operator what is being skipped.
func (b *Bypass) Patterns() []string {
	if b == nil {
		return nil
	}
	out := make([]string, len(b.patterns))
	for i, p := range b.patterns {
		out[i] = p.text
	}
	return out
}
