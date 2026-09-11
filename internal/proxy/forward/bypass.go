package forward

import (
	"cmp"
	"errors"
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
// The list can be changed while Faultline runs, through the API or a saved
// edit to the configuration file, so a host can be taken out of the way
// without a restart. The entries from DefaultBypass are the exception: they
// cannot be removed, because Faultline must never proxy itself.
//
// A nil *Bypass bypasses nothing.
type Bypass struct {
	mu       sync.Mutex
	patterns []pattern
	seen     map[string]*Bypassed
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
func (b *Bypass) Matches(host string) bool { return b.Covering(host) != "" }

// Covering is the entry that bypasses host, or the empty string when nothing
// does. It is not always the host: entries are patterns, so a portless entry
// covers every port and a wildcard covers every subdomain, and a caller that
// wants a host proxied again has to know which entry to take off the list.
//
// The first entry that covers the host wins, which is the same one Matches
// stopped at.
func (b *Bypass) Covering(host string) string {
	if b == nil {
		return ""
	}
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		name = host
	}
	name = strings.ToLower(name)

	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.patterns {
		if p.matches(name, port) {
			return p.text
		}
	}
	return ""
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
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]string, len(b.patterns))
	for i, p := range b.patterns {
		out[i] = p.text
	}
	return out
}

// ErrNotBypassed says the entry asked about is not on the list.
var ErrNotBypassed = errors.New("not on the bypass list")

// ErrBypassLocked says the entry is one of the defaults, which stay on the
// list: loopback is how Faultline reaches itself, so proxying it is never
// something a caller can ask for.
var ErrBypassLocked = errors.New("a default bypass entry cannot be removed")

// Add puts one entry on the list. An entry already there is left alone, so
// adding twice is the same as adding once.
func (b *Bypass) Add(host string) error {
	p, err := parsePattern(host)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if slices.ContainsFunc(b.patterns, func(q pattern) bool { return q.text == p.text }) {
		return nil
	}
	b.patterns = append(b.patterns, p)
	return nil
}

// Remove takes one entry off the list and forgets what was passed through for
// it, so the host starts being recorded again from nothing rather than
// carrying counts from when it was skipped.
func (b *Bypass) Remove(host string) error {
	p, err := parsePattern(host)
	if err != nil {
		return err
	}
	if isDefault(p) {
		return fmt.Errorf("%q: %w", p.text, ErrBypassLocked)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	at := slices.IndexFunc(b.patterns, func(q pattern) bool { return q.text == p.text })
	if at < 0 {
		return fmt.Errorf("%q: %w", p.text, ErrNotBypassed)
	}
	b.patterns = slices.Delete(b.patterns, at, at+1)

	for name := range b.seen {
		if p.matchesHost(name) {
			delete(b.seen, name)
		}
	}
	return nil
}

// Replace installs hosts as the whole list, in one step. A list with an entry
// that does not parse is refused and the list in force is left alone, so a bad
// edit to the configuration file cannot empty it. What has already been passed
// through is kept: it says what happened, not what the list says now.
func (b *Bypass) Replace(hosts []string) error {
	next := make([]pattern, 0, len(hosts))
	for _, raw := range hosts {
		p, err := parsePattern(raw)
		if err != nil {
			return err
		}
		next = append(next, p)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.patterns = next
	return nil
}

// Configured returns the entries that came from a flag, the configuration file
// or the API, leaving out the defaults every Bypass starts with. It is what is
// written back to the file: the defaults are Faultline's own doing and do not
// belong in somebody's configuration.
func (b *Bypass) Configured() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]string, 0, len(b.patterns))
	for _, p := range b.patterns {
		if !isDefault(p) {
			out = append(out, p.text)
		}
	}
	return out
}

// isDefault reports whether the entry is one of the defaults. Comparing the
// normalized text is enough: parsePattern produces it for both sides, so
// `LOCALHOST` and `localhost:80` are recognized as the default they are.
func isDefault(p pattern) bool {
	for _, raw := range DefaultBypass {
		if d, err := parsePattern(raw); err == nil && d.text == p.text {
			return true
		}
	}
	return false
}

// matchesHost reports whether the entry covers a host as the proxies name
// them, which is how a removed entry finds what it had been skipping.
func (p pattern) matchesHost(host string) bool {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		name = host
	}
	return p.matches(strings.ToLower(name), port)
}

// NoProxy is the bypass list spelled the way the NO_PROXY variable wants it,
// for handing to a child process so it skips the proxy for exactly the hosts
// the proxy would have passed through anyway, the admin port among them.
func (b *Bypass) NoProxy() []string {
	patterns := b.Patterns()
	entries := make([]string, 0, len(patterns))
	for _, p := range patterns {
		// NO_PROXY spells a subdomain wildcard as a bare leading dot.
		entries = append(entries, strings.TrimPrefix(p, "*"))
	}
	return entries
}
