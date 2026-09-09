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
	env := Env(nil, "http://127.0.0.1:9001", []string{"localhost", "127.0.0.1"}, "", "")

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
	env := Env([]string{"PATH=/bin", "HOME=/home/dev"}, "http://127.0.0.1:9001", nil, "", "")

	for _, want := range []string{"PATH=/bin", "HOME=/home/dev"} {
		if !slices.Contains(env, want) {
			t.Errorf("%q is missing from the child environment", want)
		}
	}
}

func TestEnvReplacesProxyVariablesTheChildAlreadyHad(t *testing.T) {
	base := []string{"HTTP_PROXY=http://corp:8080", "https_proxy=http://corp:8080", "no_proxy=example.com"}
	env := Env(base, "http://127.0.0.1:9001", []string{"localhost"}, "", "")

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
	env := Env(nil, "http://127.0.0.1:9001", nil, "/home/dev/.config/faultline/ca.pem", "")

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
	env := Env(base, "http://127.0.0.1:9001", nil, "/ca.pem", "")

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
	env := Env(base, "http://127.0.0.1:9001", nil, "", "")

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
	env := Env(nil, "http://127.0.0.1:9001", nil, "", "")

	if got, ok := lookup(env, "NODE_USE_ENV_PROXY"); !ok || got != "1" {
		t.Errorf("NODE_USE_ENV_PROXY = %q (set: %t), want %q", got, ok, "1")
	}
}

func TestEnvReplacesNodeUseEnvProxyTheChildAlreadyHad(t *testing.T) {
	env := Env([]string{"NODE_USE_ENV_PROXY=0"}, "http://127.0.0.1:9001", nil, "", "")

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

// The JVM reads its proxy settings from system properties, and
// JAVA_TOOL_OPTIONS is the only way to hand them to a JVM someone else
// starts, which is what `./mvnw spring-boot:run` is.
func TestEnvSetsJavaProxyProperties(t *testing.T) {
	env := Env(nil, "http://127.0.0.1:9001", []string{"localhost", "127.0.0.1", ".example.com"}, "", "")

	got, ok := lookup(env, "JAVA_TOOL_OPTIONS")
	if !ok {
		t.Fatal("JAVA_TOOL_OPTIONS is not set")
	}
	for _, want := range []string{
		"-Dhttp.proxyHost=127.0.0.1",
		"-Dhttp.proxyPort=9001",
		"-Dhttps.proxyHost=127.0.0.1",
		"-Dhttps.proxyPort=9001",
		// Java spells the list with pipes and keeps the star NO_PROXY drops.
		"-Dhttp.nonProxyHosts=localhost|127.0.0.1|*.example.com",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("JAVA_TOOL_OPTIONS = %q, want it to contain %q", got, want)
		}
	}
}

func TestEnvPointsJavaAtTheTrustStore(t *testing.T) {
	env := Env(nil, "http://127.0.0.1:9001", nil, "/ca.pem", "/home/dev/.config/faultline/java-truststore.p12")

	got, _ := lookup(env, "JAVA_TOOL_OPTIONS")
	for _, want := range []string{
		`-Djavax.net.ssl.trustStore="/home/dev/.config/faultline/java-truststore.p12"`,
		"-Djavax.net.ssl.trustStorePassword=changeit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("JAVA_TOOL_OPTIONS = %q, want it to contain %q", got, want)
		}
	}
}

// Without a trust store there is nothing to say about trust, and saying it
// wrong would leave the JVM trusting nothing at all.
func TestEnvLeavesJavaTrustAloneWithoutATrustStore(t *testing.T) {
	env := Env(nil, "http://127.0.0.1:9001", nil, "", "")

	if got, _ := lookup(env, "JAVA_TOOL_OPTIONS"); strings.Contains(got, "trustStore") {
		t.Errorf("JAVA_TOOL_OPTIONS = %q, want no trust store when there is none", got)
	}
}

// JAVA_TOOL_OPTIONS is where a developer keeps -Xmx and the like, so
// Faultline's options are appended rather than substituted: the JVM lets the
// last -D win, so ours still do.
func TestEnvAppendsToTheChildsOwnJavaToolOptions(t *testing.T) {
	env := Env([]string{"JAVA_TOOL_OPTIONS=-Xmx512m"}, "http://127.0.0.1:9001", nil, "", "")

	got, _ := lookup(env, "JAVA_TOOL_OPTIONS")
	if !strings.HasPrefix(got, "-Xmx512m ") {
		t.Errorf("JAVA_TOOL_OPTIONS = %q, want it to keep -Xmx512m first", got)
	}
	if !strings.Contains(got, "-Dhttp.proxyHost=") {
		t.Errorf("JAVA_TOOL_OPTIONS = %q, want Faultline's options after it", got)
	}
	seen := 0
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); k == "JAVA_TOOL_OPTIONS" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("JAVA_TOOL_OPTIONS appears %d times, want once", seen)
	}
}

// The JVM splits JAVA_TOOL_OPTIONS on whitespace, and on macOS the config
// directory Faultline keeps the trust store in is under "Application
// Support". Unquoted, the JVM reads the half after the space as an option of
// its own and refuses to start at all.
func TestEnvQuotesTheTrustStorePath(t *testing.T) {
	store := "/Users/dev/Library/Application Support/faultline/java-truststore.p12"
	env := Env(nil, "http://127.0.0.1:9001", nil, "/ca.pem", store)

	got, _ := lookup(env, "JAVA_TOOL_OPTIONS")
	if !strings.Contains(got, `-Djavax.net.ssl.trustStore="`+store+`"`) {
		t.Errorf("JAVA_TOOL_OPTIONS = %q, want the trust store path quoted", got)
	}
}

// The diagnostics for a client that rejected the interception certificate
// name the variables it was handed, so what TrustVars reports has to be
// exactly what Env sets.
func TestTrustVarsReportsWhatEnvSet(t *testing.T) {
	tests := []struct {
		name       string
		caPath     string
		trustStore string
		want       []string
	}{
		{"nothing without a CA", "", "", nil},
		{"the runtime variables with a CA", "/ca.pem", "", trustVars},
		{"java too with a trust store", "/ca.pem", "/store.p12", append(slices.Clone(trustVars), javaToolOptions)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrustVars(tt.caPath, tt.trustStore)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("TrustVars = %v, want %v", got, tt.want)
			}

			env := Env(nil, "http://127.0.0.1:9001", nil, tt.caPath, tt.trustStore)
			for _, name := range got {
				if _, ok := lookup(env, name); !ok {
					t.Errorf("TrustVars names %s but Env does not set it", name)
				}
			}
		})
	}
}
