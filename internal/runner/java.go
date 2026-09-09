package runner

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// javaToolOptions is read by every JVM at startup, which is the only place
// Faultline can reach one it does not launch itself: `./mvnw spring-boot:run`
// and `./gradlew bootRun` both fork a JVM of their own. Unlike the variables
// Faultline owns outright, this one is appended to rather than replaced, so a
// developer's own -Xmx survives; the JVM lets the last -D win, so Faultline's
// settings still do.
const javaToolOptions = "JAVA_TOOL_OPTIONS"

// trustStorePassword is the password the JDK ships cacerts with. The trust
// store Faultline builds is a copy of that file, so it keeps it.
const trustStorePassword = "changeit"

// javaOptions renders the system properties a JVM needs to send its calls
// through Faultline: the JDK's own HTTP stack, and so RestClient and anything
// on HttpURLConnection, reads them directly. Reactor Netty, which WebClient
// is built on, only reads them when the client is built with
// proxyWithSystemProperties().
//
// trustStore is the JDK trust store with the Faultline CA added, empty when
// there is no CA and the certificates the child sees are the real ones.
func javaOptions(proxyURL string, noProxy []string, trustStore string) []string {
	host, port, err := hostPort(proxyURL)
	if err != nil {
		return nil
	}

	options := []string{
		"-Dhttp.proxyHost=" + host,
		"-Dhttp.proxyPort=" + port,
		"-Dhttps.proxyHost=" + host,
		"-Dhttps.proxyPort=" + port,
	}
	if hosts := nonProxyHosts(noProxy); hosts != "" {
		// http.nonProxyHosts covers https too; there is no https variant.
		options = append(options, "-Dhttp.nonProxyHosts="+hosts)
	}
	if trustStore != "" {
		// The JVM splits this variable on whitespace, and on macOS the trust
		// store lives under "Application Support". It reads a quoted value as
		// one option, so the path is always quoted rather than only when it
		// happens to need it.
		options = append(options,
			`-Djavax.net.ssl.trustStore="`+trustStore+`"`,
			"-Djavax.net.ssl.trustStorePassword="+trustStorePassword)
	}
	return options
}

// nonProxyHosts rewrites the bypass list from NO_PROXY's spelling into
// Java's: entries are separated by a pipe and a subdomain wildcard is written
// out as a star rather than left as a bare leading dot.
func nonProxyHosts(noProxy []string) string {
	entries := make([]string, 0, len(noProxy))
	for _, entry := range noProxy {
		if strings.HasPrefix(entry, ".") {
			entry = "*" + entry
		}
		entries = append(entries, entry)
	}
	return strings.Join(entries, "|")
}

// hostPort splits a proxy URL into the two halves Java wants separately.
func hostPort(proxyURL string) (string, string, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return "", "", fmt.Errorf("parse proxy url %q: %w", proxyURL, err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return "", "", fmt.Errorf("split proxy address %q: %w", u.Host, err)
	}
	return host, port, nil
}
