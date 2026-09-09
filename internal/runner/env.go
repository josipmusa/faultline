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

// Env builds the child environment from base, usually os.Environ(). proxyURL
// is the address of the forward proxy, and noProxy the hosts the child should
// reach directly, written the way NO_PROXY wants them.
func Env(base []string, proxyURL string, noProxy []string) []string {
	owned := make(map[string]bool, len(proxyVars))
	for _, name := range proxyVars {
		owned[strings.ToLower(name)] = true
	}

	env := make([]string, 0, len(base)+len(proxyVars))
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

	return env
}
