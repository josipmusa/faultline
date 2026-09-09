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
	env := Env(nil, "http://127.0.0.1:9001", []string{"localhost", "127.0.0.1"})

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
	env := Env([]string{"PATH=/bin", "HOME=/home/dev"}, "http://127.0.0.1:9001", nil)

	for _, want := range []string{"PATH=/bin", "HOME=/home/dev"} {
		if !slices.Contains(env, want) {
			t.Errorf("%q is missing from the child environment", want)
		}
	}
}

func TestEnvReplacesProxyVariablesTheChildAlreadyHad(t *testing.T) {
	base := []string{"HTTP_PROXY=http://corp:8080", "https_proxy=http://corp:8080", "no_proxy=example.com"}
	env := Env(base, "http://127.0.0.1:9001", []string{"localhost"})

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
