// Package runner starts an application as a child of Faultline with an
// environment that sends its outbound HTTP and HTTPS calls through the
// forward proxy, and mirrors the child's exit code back to the caller.
package runner

import (
	"strings"
)

// proxyVars are the variables Faultline owns in the child environment. Both
// cases are set because runtimes disagree about which they read, and both are
// removed from the parent environment first so a value the developer already
// had cannot shadow ours.
var proxyVars = []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "NO_PROXY", "no_proxy"}

// trustVars point a runtime at a CA bundle: Go and OpenSSL read
// SSL_CERT_FILE, Python's requests REQUESTS_CA_BUNDLE, curl CURL_CA_BUNDLE,
// Node NODE_EXTRA_CA_CERTS, and git GIT_SSL_CAINFO. Faultline owns them only
// while it is intercepting; otherwise the certificates the child sees are the
// real ones and its own bundle is still the right answer.
var trustVars = []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO"}

// nodeEnvProxy is Node's opt-in to reading the proxy variables from its
// native fetch. Node's own http and https modules, and everything built on
// them, need nothing; undici behind fetch ignores the variables until this is
// set. It landed in Node 24 and is backported to recent 22.x, and versions
// without it ignore an unknown variable, so it is always set and the value is
// always 1: there is nothing to decide per child, whose runtime is unknown
// anyway.
const nodeEnvProxy = "NODE_USE_ENV_PROXY"

// Env builds the child environment from base, usually os.Environ(). proxyURL
// is the address of the forward proxy, noProxy the hosts the child should
// reach directly, written the way NO_PROXY wants them, and caPath the
// certificate the intercepting CA signs with, empty when HTTPS is passed
// through untouched.
func Env(base []string, proxyURL string, noProxy []string, caPath string) []string {
	names := append(append([]string{}, proxyVars...), nodeEnvProxy)
	if caPath != "" {
		names = append(names, trustVars...)
	}

	owned := make(map[string]bool, len(names))
	for _, name := range names {
		owned[strings.ToLower(name)] = true
	}

	env := make([]string, 0, len(base)+len(names))
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if ok && owned[strings.ToLower(key)] {
			continue
		}
		env = append(env, kv)
	}

	bypass := strings.Join(noProxy, ",")
	for _, name := range proxyVars {
		value := proxyURL
		if strings.EqualFold(name, "NO_PROXY") {
			value = bypass
		}
		env = append(env, name+"="+value)
	}
	env = append(env, nodeEnvProxy+"=1")
	if caPath != "" {
		for _, name := range trustVars {
			env = append(env, name+"="+caPath)
		}
	}

	return env
}
