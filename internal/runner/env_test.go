package runner

import (
	"slices"
	"strings"
	"testing"
)

func lookup(env []string, key string) (string, bool) {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

func TestEnvPointsEveryProxyVariableAtTheProxy(t *testing.T) {
	env := Env(nil, "http://127.0.0.1:9001", []string{"localhost", "127.0.0.1"}, "")

	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		got, ok := lookup(env, key)
		if !ok {
			t.Errorf("%s is not set", key)
			continue
		}
		if got != "http://127.0.0.1:9001" {
			t.Errorf("%s = %q, want the proxy url", key, got)
		}
	}
	for _, key := range []string{"NO_PROXY", "no_proxy"} {
		got, ok := lookup(env, key)
		if !ok {
			t.Errorf("%s is not set", key)
			continue
		}
		if got != "localhost,127.0.0.1" {
			t.Errorf("%s = %q, want the bypass list", key, got)
		}
	}
}

func TestEnvKeepsEverythingElse(t *testing.T) {
	env := Env([]string{"PATH=/bin", "HOME=/home/dev"}, "http://127.0.0.1:9001", nil, "")

	for _, want := range []string{"PATH=/bin", "HOME=/home/dev"} {
		if !slices.Contains(env, want) {
			t.Errorf("%q is missing from the child environment", want)
		}
	}
}

func TestEnvReplacesProxyVariablesTheChildAlreadyHad(t *testing.T) {
	base := []string{"HTTP_PROXY=http://corp:8080", "https_proxy=http://corp:8080", "no_proxy=example.com"}
	env := Env(base, "http://127.0.0.1:9001", []string{"localhost"}, "")

	for _, kv := range env {
		if strings.Contains(kv, "corp:8080") || kv == "no_proxy=example.com" {
			t.Errorf("the child inherited %q; Faultline's value must win", kv)
		}
	}
	// One entry per variable, whatever case the parent used.
	seen := make(map[string]int)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		seen[k]++
	}
	for k, n := range seen {
		if n != 1 {
			t.Errorf("%s appears %d times, want once", k, n)
		}
	}
}

func TestEnvPointsEveryTrustVariableAtTheCA(t *testing.T) {
	env := Env(nil, "http://127.0.0.1:9001", nil, "/home/dev/.config/faultline/ca.pem")

	for _, key := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO"} {
		got, ok := lookup(env, key)
		if !ok {
			t.Errorf("%s is not set", key)
			continue
		}
		if got != "/home/dev/.config/faultline/ca.pem" {
			t.Errorf("%s = %q, want the CA path", key, got)
		}
	}
}

func TestEnvReplacesTrustVariablesTheChildAlreadyHad(t *testing.T) {
	base := []string{"SSL_CERT_FILE=/etc/corp/bundle.pem", "NODE_EXTRA_CA_CERTS=/etc/corp/bundle.pem"}
	env := Env(base, "http://127.0.0.1:9001", nil, "/ca.pem")

	for _, kv := range env {
		if strings.Contains(kv, "/etc/corp/bundle.pem") {
			t.Errorf("the child inherited %q; Faultline's CA must win", kv)
		}
	}
	seen := make(map[string]int)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		seen[k]++
	}
	for k, n := range seen {
		if n != 1 {
			t.Errorf("%s appears %d times, want once", k, n)
		}
	}
}

// Without interception the upstream certificates are the real ones, so the
// developer's own bundle is still the right answer and Faultline says nothing.
func TestEnvWithoutACALeavesTrustVariablesAlone(t *testing.T) {
	base := []string{"SSL_CERT_FILE=/etc/corp/bundle.pem"}
	env := Env(base, "http://127.0.0.1:9001", nil, "")

	if got, _ := lookup(env, "SSL_CERT_FILE"); got != "/etc/corp/bundle.pem" {
		t.Errorf("SSL_CERT_FILE = %q, want the value the child came with", got)
	}
	if got, ok := lookup(env, "NODE_EXTRA_CA_CERTS"); ok {
		t.Errorf("NODE_EXTRA_CA_CERTS = %q, want it unset when there is no CA", got)
	}
}

// Node's native fetch ignores the proxy variables unless this is set. The
// variable exists in Node 24 and is backported to recent 22.x; older versions
// ignore it, so it is set whatever the child turns out to be.
func TestEnvTellsNodeToReadTheProxyVariables(t *testing.T) {
	env := Env(nil, "http://127.0.0.1:9001", nil, "")

	if got, ok := lookup(env, "NODE_USE_ENV_PROXY"); !ok || got != "1" {
		t.Errorf("NODE_USE_ENV_PROXY = %q (set: %t), want %q", got, ok, "1")
	}
}

func TestEnvReplacesNodeUseEnvProxyTheChildAlreadyHad(t *testing.T) {
	env := Env([]string{"NODE_USE_ENV_PROXY=0"}, "http://127.0.0.1:9001", nil, "")

	if got, _ := lookup(env, "NODE_USE_ENV_PROXY"); got != "1" {
		t.Errorf("NODE_USE_ENV_PROXY = %q, want Faultline's value to win", got)
	}
	seen := 0
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); k == "NODE_USE_ENV_PROXY" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("NODE_USE_ENV_PROXY appears %d times, want once", seen)
	}
}
