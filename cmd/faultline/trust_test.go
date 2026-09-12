package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/runner"
)

// tryCmd runs the root command and hands back both halves, so a test can
// decide what an error means instead of failing on it.
func tryCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)

	err := root.Execute()
	return out.String(), err
}

// skipWithoutJDK turns the one failure that is about the machine rather than
// the code into a skip: the trust store is built with the JDK's own keytool,
// so there is nothing to test without one.
func skipWithoutJDK(t *testing.T, err error) {
	t.Helper()

	if errors.Is(err, runner.ErrNoJDK) {
		t.Skipf("no JDK on this machine: %v", err)
	}
}

func TestTrustJavaBuildsTheStoreAndPrintsTheOptions(t *testing.T) {
	isolateConfigDir(t)
	out := t.TempDir()
	t.Setenv("HTTPS_PROXY", "http://faultline:9001")
	t.Setenv("NO_PROXY", "localhost,.internal")

	runCmd(t, "ca", "init")
	printed, err := tryCmd(t, "trust", "java", "--out", out)
	skipWithoutJDK(t, err)
	if err != nil {
		t.Fatalf("trust java: %v\n%s", err, printed)
	}

	store := filepath.Join(out, "java-truststore.p12")
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("no trust store was built: %v", err)
	}
	for _, want := range []string{
		"JAVA_TOOL_OPTIONS=",
		"-Dhttp.proxyHost=faultline",
		"-Dhttps.proxyPort=9001",
		"-Dhttp.nonProxyHosts=localhost|*.internal",
		store,
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("output = %q, want it to contain %q", printed, want)
		}
	}
}

// As an entrypoint the command runs the application itself, so the JVM gets
// the options without an image needing a shell to export them in.
func TestTrustJavaRunsTheCommandWithTheOptionsSet(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("HTTPS_PROXY", "http://faultline:9001")

	runCmd(t, "ca", "init")

	out, err := tryCmd(t, "trust", "java", "--out", t.TempDir(), "--", "sh", "-c", "echo $JAVA_TOOL_OPTIONS")
	skipWithoutJDK(t, err)
	if err != nil {
		t.Fatalf("trust java -- sh: %v\n%s", err, out)
	}

	if !strings.Contains(out, "-Dhttps.proxyHost=faultline") {
		t.Errorf("the child saw JAVA_TOOL_OPTIONS = %q", out)
	}
}

// The child's exit code is the command's own, so a failing application still
// stops the container.
func TestTrustJavaMirrorsTheChildExitCode(t *testing.T) {
	isolateConfigDir(t)
	runCmd(t, "ca", "init")

	_, err := tryCmd(t, "trust", "java", "--out", t.TempDir(), "--", "sh", "-c", "exit 3")
	skipWithoutJDK(t, err)
	var exit exitError
	if !errors.As(err, &exit) || int(exit) != 3 {
		t.Errorf("error = %v, want exit status 3", err)
	}
}

// Without the CA mounted there is nothing to trust, and the reason is the
// mount rather than anything the application did.
func TestTrustJavaWithoutACASaysWhereItLooked(t *testing.T) {
	isolateConfigDir(t)

	out, err := tryCmd(t, "trust", "java", "--out", t.TempDir())
	if err == nil {
		t.Fatalf("trust java with no CA succeeded: %s", out)
	}
	if !strings.Contains(err.Error(), "ca.crt") {
		t.Errorf("error = %v, want it to name the certificate it looked for", err)
	}
}
