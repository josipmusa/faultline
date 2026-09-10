package forward

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/josipmusa/faultline/internal/faults"
	"github.com/josipmusa/faultline/internal/rules"
)

func mustBypass(t *testing.T, hosts ...string) *Bypass {
	t.Helper()
	b, err := NewBypass(hosts)
	if err != nil {
		t.Fatalf("NewBypass(%q): %v", hosts, err)
	}
	return b
}

func TestBypassMatches(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		host     string
		want     bool
	}{
		{"exact host", []string{"httpbin.org"}, "httpbin.org", true},
		{"exact host is case-insensitive", []string{"HTTPBin.org"}, "httpbin.ORG", true},
		{"exact host on any port", []string{"httpbin.org"}, "httpbin.org:8080", true},
		{"exact host does not cover subdomains", []string{"internal"}, "db.internal", false},
		{"exact host does not cover a longer name", []string{"bin.org"}, "httpbin.org", false},
		{"another host", []string{"httpbin.org"}, "api.stripe.com", false},

		{"wildcard covers one label", []string{"*.internal"}, "db.internal", true},
		{"wildcard covers deeper names", []string{"*.internal"}, "eu.db.internal", true},
		{"wildcard does not cover the apex", []string{"*.internal"}, "internal", false},
		{"wildcard needs the dot", []string{"*.internal"}, "notinternal", false},
		{"wildcard with a port", []string{"*.internal"}, "db.internal:5000", true},
		{"leading dot means the same as the wildcard", []string{".internal"}, "db.internal", true},
		{"leading dot does not cover the apex", []string{".internal"}, "internal", false},

		{"host and port", []string{"localhost:9000"}, "localhost:9000", true},
		{"host and port wants that port", []string{"localhost:9000"}, "localhost:9001", false},
		{"host and port wants a port", []string{"localhost:9000"}, "localhost", false},
		{"default port in the pattern is dropped like in the host", []string{"httpbin.org:443"}, "httpbin.org", true},
		{"ipv4", []string{"127.0.0.1"}, "127.0.0.1:9000", true},
		{"ipv6", []string{"::1"}, "[::1]:9000", true},
		{"ipv6 with port", []string{"[::1]:9000"}, "[::1]:9000", true},
		{"ipv6 with another port", []string{"[::1]:9000"}, "[::1]:9001", false},

		{"first of several", []string{"a.test", "b.test"}, "a.test", true},
		{"last of several", []string{"a.test", "b.test"}, "b.test", true},
		{"none of several", []string{"a.test", "b.test"}, "c.test", false},
		{"surrounding whitespace is trimmed", []string{" httpbin.org "}, "httpbin.org", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mustBypass(t, tt.patterns...).Matches(tt.host); got != tt.want {
				t.Errorf("Bypass(%q).Matches(%q) = %v, want %v", tt.patterns, tt.host, got, tt.want)
			}
		})
	}
}

func TestBypassRejectsBadPatterns(t *testing.T) {
	tests := []struct {
		pattern string
		want    string // a fragment of the error
	}{
		{"", "empty"},
		{"   ", "empty"},
		{"*", "wildcard"},
		{"*.", "wildcard"},
		{"api.*.com", "wildcard"},
		{"*internal", "wildcard"},
		{"foo*.com", "wildcard"},
		{"localhost:http", "port"},
		{"localhost:70000", "port"},
		{"localhost:0", "port"},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			_, err := NewBypass([]string{"ok.test", tt.pattern})
			if err == nil {
				t.Fatalf("NewBypass accepted %q", tt.pattern)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
			if tt.want != "empty" && !strings.Contains(err.Error(), tt.pattern) {
				t.Errorf("error = %q, want it to name the pattern %q", err, tt.pattern)
			}
		})
	}
}

func TestBypassPatternsAreReportedNormalized(t *testing.T) {
	b := mustBypass(t, " HTTPBin.org ", "*.Internal", "[::1]:9000")

	got := strings.Join(b.Patterns(), ",")
	if want := "httpbin.org,*.internal,[::1]:9000"; got != want {
		t.Errorf("Patterns() = %q, want %q", got, want)
	}
}

func TestBypassRemembersWhatItSaw(t *testing.T) {
	b := mustBypass(t, "*.internal", "httpbin.org")
	before := time.Now()

	b.Saw("db.internal")
	b.Saw("httpbin.org")
	b.Saw("db.internal")

	seen := b.Seen()
	if len(seen) != 2 {
		t.Fatalf("Seen() = %+v, want two hosts", seen)
	}
	if seen[0].Host != "db.internal" || seen[1].Host != "httpbin.org" {
		t.Errorf("hosts = %q, %q, want them sorted", seen[0].Host, seen[1].Host)
	}
	if seen[0].Requests != 2 || seen[1].Requests != 1 {
		t.Errorf("requests = %d, %d, want 2 and 1", seen[0].Requests, seen[1].Requests)
	}
	if seen[0].LastSeen.Before(before) {
		t.Errorf("last seen %v is before the test started at %v", seen[0].LastSeen, before)
	}
}

func TestBypassStartsWithNothingSeen(t *testing.T) {
	if seen := mustBypass(t, "httpbin.org").Seen(); len(seen) != 0 {
		t.Errorf("Seen() = %+v, want nothing", seen)
	}
}

// A nil Bypass is how the proxy says "bypass nothing", so every method has to
// be safe on one.
func TestNilBypassBypassesNothing(t *testing.T) {
	var b *Bypass
	if b.Matches("localhost") {
		t.Error("nil bypass matched a host")
	}
	b.Saw("localhost")
	if seen := b.Seen(); len(seen) != 0 {
		t.Errorf("Seen() = %+v, want nothing", seen)
	}
	if p := b.Patterns(); len(p) != 0 {
		t.Errorf("Patterns() = %q, want nothing", p)
	}
}

func TestDefaultBypassCoversLoopback(t *testing.T) {
	b := mustBypass(t, DefaultBypass...)
	for _, host := range []string{"localhost", "localhost:9000", "127.0.0.1:9000", "[::1]:9000", "127.0.0.1:8080"} {
		if !b.Matches(host) {
			t.Errorf("default bypass does not cover %q", host)
		}
	}
	if b.Matches("httpbin.org") {
		t.Error("default bypass covers httpbin.org")
	}
}

// A bypassed CONNECT is tunneled to the real upstream with no rule consulted
// and no event recorded, even when a rule would have refused it.
func TestBypassedConnectTunnelsWithoutRulesOrEvents(t *testing.T) {
	bypass := mustBypass(t, "127.0.0.1")
	f := newFixtureWith(t, bypass)
	up, client := f.tlsUpstream(t)
	host := up.Listener.Addr().String()

	if err := f.store.Add(rules.Rule{
		ID:      "down",
		Enabled: true,
		Match:   rules.Match{Host: host},
		Fault:   rules.Fault{Type: "refuse"},
	}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}

	resp, err := client.Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("tunneled request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: a bypassed host is out of reach of rules", resp.StatusCode)
	}
	if recorded := f.recorder.Events(); len(recorded) != 0 {
		t.Errorf("events = %+v, want none for a bypassed host", recorded)
	}
	seen := bypass.Seen()
	if len(seen) != 1 || seen[0].Host != host || seen[0].Requests != 1 {
		t.Errorf("Seen() = %+v, want one request for %s", seen, host)
	}
}

// A bypassed plain request is forwarded untouched: no faults, no event.
func TestBypassedPlainRequestIsForwardedWithoutRulesOrEvents(t *testing.T) {
	bypass := mustBypass(t, "127.0.0.1")
	f := newFixtureWith(t, bypass)
	up := newUpstream(t)
	host := up.Listener.Addr().String()

	if err := f.store.Add(rules.Rule{
		ID:      "down",
		Enabled: true,
		Match:   rules.Match{Host: host},
		Fault:   rules.Fault{Type: "status", Params: rules.Params{"code": 503}},
	}); err != nil {
		t.Fatalf("adding rule: %v", err)
	}

	resp, err := f.client(t).Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK || string(body) != "hello from upstream" {
		t.Errorf("got %d %q, want the upstream's own 200", resp.StatusCode, body)
	}
	if resp.Header.Get(faults.FaultHeader) != "" {
		t.Errorf("%s = %q, want none: no fault applied", faults.FaultHeader, resp.Header.Get(faults.FaultHeader))
	}
	if got := up.header(t).Get("Proxy-Connection"); got != "" {
		t.Errorf("upstream saw Proxy-Connection %q, want hop headers stripped on the bypass path too", got)
	}
	if recorded := f.recorder.Events(); len(recorded) != 0 {
		t.Errorf("events = %+v, want none for a bypassed host", recorded)
	}
	if seen := bypass.Seen(); len(seen) != 1 || seen[0].Host != host {
		t.Errorf("Seen() = %+v, want %s", seen, host)
	}
}

// A host that is not on the list is unaffected by the list being there.
func TestUnlistedHostIsProxiedNormally(t *testing.T) {
	bypass := mustBypass(t, "*.internal")
	f := newFixtureWith(t, bypass)
	up := newUpstream(t)

	resp, err := f.client(t).Get(up.URL + "/orders")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()

	if recorded := f.recorder.Events(); len(recorded) != 1 {
		t.Errorf("events = %+v, want the usual one", recorded)
	}
	if seen := bypass.Seen(); len(seen) != 0 {
		t.Errorf("Seen() = %+v, want nothing", seen)
	}
}

// Bypass beats interception: the client ends up talking TLS to the real
// upstream, so a client that does not trust the Faultline CA still succeeds and
// nothing is recorded.
func TestBypassWinsOverInterception(t *testing.T) {
	bypass := mustBypass(t, "127.0.0.1")
	f := newInterceptFixtureWith(t, bypass)

	// Trusts the upstream's own certificate and not the Faultline CA, like an
	// application nobody told about the CA.
	roots := x509.NewCertPool()
	roots.AddCert(f.upstream.Certificate())
	client := f.clientWith(t, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12})

	resp, err := client.Get(f.upstream.URL + "/orders")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 from the real upstream", resp.StatusCode)
	}
	if recorded := f.recorder.Events(); len(recorded) != 0 {
		t.Errorf("events = %+v, want none: a bypassed tunnel is not intercepted", recorded)
	}
	if seen := bypass.Seen(); len(seen) != 1 {
		t.Errorf("Seen() = %+v, want the upstream", seen)
	}
}

func TestBypassAdd(t *testing.T) {
	b := mustBypass(t, DefaultBypass...)

	if err := b.Add("API.Stripe.com:443"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !b.Matches("api.stripe.com") {
		t.Error("the host added is not matched")
	}
	if got := b.Configured(); len(got) != 1 || got[0] != "api.stripe.com" {
		t.Errorf("Configured() = %q, want the one host added, normalized", got)
	}

	// Adding the same entry again is what a second click on the panel's
	// toggle would do, and it must not stack up entries.
	if err := b.Add("api.stripe.com"); err != nil {
		t.Fatalf("Add again: %v", err)
	}
	if got := b.Configured(); len(got) != 1 {
		t.Errorf("Configured() = %q, want the entry only once", got)
	}

	if err := b.Add("*.*.bad"); err == nil {
		t.Error("Add accepted a malformed entry")
	}
}

func TestBypassRemove(t *testing.T) {
	b := mustBypass(t, append(DefaultBypass, "httpbin.org")...)
	b.Saw("httpbin.org")

	if err := b.Remove("httpbin.org"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if b.Matches("httpbin.org") {
		t.Error("the host removed is still matched")
	}
	if seen := b.Seen(); len(seen) != 0 {
		t.Errorf("Seen() = %+v, want the host forgotten so it can be recorded afresh", seen)
	}

	if err := b.Remove("httpbin.org"); !errors.Is(err, ErrNotBypassed) {
		t.Errorf("removing an entry that is not there = %v, want ErrNotBypassed", err)
	}
	if err := b.Remove("localhost"); !errors.Is(err, ErrBypassLocked) {
		t.Errorf("removing localhost = %v, want ErrBypassLocked: faultline must not proxy itself", err)
	}
	if !b.Matches("localhost") {
		t.Error("localhost stopped being bypassed")
	}
}

func TestBypassReplace(t *testing.T) {
	b := mustBypass(t, append(DefaultBypass, "httpbin.org")...)
	b.Saw("httpbin.org")

	if err := b.Replace(append(slices.Clone(DefaultBypass), "api.stripe.com")); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if b.Matches("httpbin.org") {
		t.Error("a host the new list leaves out is still matched")
	}
	if !b.Matches("api.stripe.com") || !b.Matches("localhost") {
		t.Error("the new list is not in force")
	}
	if seen := b.Seen(); len(seen) != 1 {
		t.Errorf("Seen() = %+v, want what was already passed through to stand", seen)
	}

	if err := b.Replace([]string{"api.stripe.com", "*.*.bad"}); err == nil {
		t.Fatal("Replace accepted a list with a malformed entry")
	}
	if !b.Matches("api.stripe.com") || !b.Matches("localhost") {
		t.Error("a rejected Replace changed the list; the old one must stay in force")
	}
}

// Entries are patterns, so what bypasses a host is often not the host: a
// portless entry covers every port, and a wildcard covers every subdomain.
// Naming the entry is what lets a caller say which one to take off the list.
func TestBypassCovering(t *testing.T) {
	b := mustBypass(t, append(DefaultBypass, "*.internal", "httpbin.org")...)

	tests := map[string]string{
		"127.0.0.1:8777":  "127.0.0.1",
		"db.internal":     "*.internal",
		"httpbin.org":     "httpbin.org",
		"httpbin.org:444": "httpbin.org",
		"api.stripe.com":  "",
	}
	for host, want := range tests {
		if got := b.Covering(host); got != want {
			t.Errorf("Covering(%q) = %q, want %q", host, got, want)
		}
	}
}

// Removing one host must not take a whole wildcard with it: the entry covers
// names nobody asked about.
func TestBypassRemoveDoesNotReachThroughAWildcard(t *testing.T) {
	b := mustBypass(t, "*.internal")

	if err := b.Remove("db.internal"); !errors.Is(err, ErrNotBypassed) {
		t.Errorf("Remove(db.internal) = %v, want ErrNotBypassed", err)
	}
	if !b.Matches("db.internal") {
		t.Error("the wildcard was removed")
	}
}
