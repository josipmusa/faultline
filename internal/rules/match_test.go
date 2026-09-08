package rules

import (
	"net/http"
	"testing"
)

// headerOf builds a header the way net/http does, canonicalising names.
func headerOf(pairs ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(pairs); i += 2 {
		h.Set(pairs[i], pairs[i+1])
	}
	return h
}

func delayRule(m Match) Rule {
	return Rule{
		ID:      "r1",
		Name:    "test",
		Enabled: true,
		Match:   m,
		Fault:   Fault{Type: FaultDelay, MS: 100},
	}
}

func TestMatchesHostOnly(t *testing.T) {
	r := delayRule(Match{Host: "api.stripe.com"})

	tests := []struct {
		name string
		host string
		want bool
	}{
		{"exact", "api.stripe.com", true},
		{"different case", "API.Stripe.COM", true},
		{"other host", "api.github.com", false},
		{"suffix is not a match", "evil-api.stripe.com", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Matches(tt.host, "GET", "/v1/charges", nil); got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestMatchesEmptyHostMatchesAnyHost(t *testing.T) {
	r := delayRule(Match{})
	if !r.Matches("anything.example", "GET", "/", nil) {
		t.Error("empty match should match any request")
	}
}

func TestMatchesHostAndMethod(t *testing.T) {
	r := delayRule(Match{Host: "api.stripe.com", Method: "POST"})

	tests := []struct {
		name   string
		host   string
		method string
		want   bool
	}{
		{"host and method", "api.stripe.com", "POST", true},
		{"method different case", "api.stripe.com", "post", true},
		{"wrong method", "api.stripe.com", "GET", false},
		{"wrong host", "api.github.com", "POST", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Matches(tt.host, tt.method, "/v1/charges", nil); got != tt.want {
				t.Errorf("Matches(%q, %q) = %v, want %v", tt.host, tt.method, got, tt.want)
			}
		})
	}
}

func TestMatchesPathGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"literal equal", "/orders", "/orders", true},
		{"literal different", "/orders", "/order", false},
		{"single star matches one segment", "/orders/*", "/orders/123", true},
		{"single star matches empty segment", "/orders/*", "/orders/", true},
		{"single star stops at slash", "/orders/*", "/orders/123/items", false},
		{"single star needs the prefix", "/orders/*", "/invoices/123", false},
		{"single star mid segment", "/v1/*/items", "/v1/42/items", true},
		{"double star crosses slashes", "/v1/**", "/v1/orders/123/items", true},
		{"double star matches one segment", "/v1/**", "/v1/orders", true},
		{"double star matches empty", "/v1/**", "/v1/", true},
		{"double star needs the prefix", "/v1/**", "/v2/orders", false},
		{"double star mid pattern", "/v1/**/items", "/v1/a/b/items", true},
		{"bare double star matches everything", "**", "/anything/at/all", true},
		{"dots are literal", "/a.b", "/axb", false},
		{"empty pattern matches any path", "", "/whatever", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := delayRule(Match{Host: "api.stripe.com", Path: tt.pattern})
			if got := r.Matches("api.stripe.com", "GET", tt.path, nil); got != tt.want {
				t.Errorf("path %q against pattern %q = %v, want %v", tt.path, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestMatchesHeader(t *testing.T) {
	r := delayRule(Match{
		Host:   "api.stripe.com",
		Header: map[string]string{"X-Test": "1", "X-Tenant": "acme"},
	})

	tests := []struct {
		name   string
		header http.Header
		want   bool
	}{
		{
			name:   "all headers present",
			header: headerOf("X-Test", "1", "X-Tenant", "acme"),
			want:   true,
		},
		{
			// net/http canonicalises header names as it parses them, so a
			// request sending `x-test: 1` on the wire arrives canonicalised.
			name:   "wire header name in another case",
			header: headerOf("x-test", "1", "x-tenant", "acme"),
			want:   true,
		},
		{
			name:   "extra headers are ignored",
			header: headerOf("X-Test", "1", "X-Tenant", "acme", "X-Other", "z"),
			want:   true,
		},
		{
			name:   "one header missing",
			header: headerOf("X-Test", "1"),
			want:   false,
		},
		{
			name:   "value differs",
			header: headerOf("X-Test", "2", "X-Tenant", "acme"),
			want:   false,
		},
		{
			name:   "value is case sensitive",
			header: headerOf("X-Test", "1", "X-Tenant", "ACME"),
			want:   false,
		},
		{
			name:   "no headers at all",
			header: nil,
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Matches("api.stripe.com", "GET", "/v1/charges", tt.header); got != tt.want {
				t.Errorf("Matches(header=%v) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}

func TestMatchesRuleHeaderNameInAnyCase(t *testing.T) {
	r := delayRule(Match{Host: "api.stripe.com", Header: map[string]string{"x-TEST": "1"}})

	if !r.Matches("api.stripe.com", "GET", "/v1/charges", headerOf("X-Test", "1")) {
		t.Error("the header name in the rule should be case insensitive too")
	}
}

func TestMatchesAllCriteriaTogether(t *testing.T) {
	r := delayRule(Match{
		Host:   "api.stripe.com",
		Method: "POST",
		Path:   "/v1/charges/*",
		Header: map[string]string{"X-Test": "1"},
	})
	header := headerOf("X-Test", "1")

	if !r.Matches("api.stripe.com", "POST", "/v1/charges/ch_1", header) {
		t.Error("want match when every criterion is satisfied")
	}
	if r.Matches("api.stripe.com", "POST", "/v1/charges/ch_1/refunds", header) {
		t.Error("want no match when the path glob fails")
	}
	if r.Matches("api.stripe.com", "POST", "/v1/charges/ch_1", nil) {
		t.Error("want no match when the header is absent")
	}
}

func TestDisabledRuleNeverMatches(t *testing.T) {
	r := delayRule(Match{Host: "api.stripe.com"})
	r.Enabled = false

	if r.Matches("api.stripe.com", "GET", "/v1/charges", nil) {
		t.Error("a disabled rule must not match its own host")
	}

	empty := Rule{Fault: Fault{Type: FaultDelay, MS: 1}}
	if empty.Matches("anything", "GET", "/", nil) {
		t.Error("a disabled empty-match rule must not match anything")
	}
}
