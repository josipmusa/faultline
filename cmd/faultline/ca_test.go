package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateConfigDir points the OS config dir at a temp dir for the test, so
// the CA commands never touch the real per-user state.
func isolateConfigDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	return home
}

// runCmdIn is runCmd with a stdin.
func runCmdIn(t *testing.T, stdin string, args ...string) string {
	t.Helper()

	var out bytes.Buffer
	root := newRootCmd()
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)

	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return out.String()
}

func TestCAInitCreatesOnceAndReportsExisting(t *testing.T) {
	home := isolateConfigDir(t)

	first := runCmd(t, "ca", "init")
	if !strings.Contains(first, "created") {
		t.Errorf("first init output = %q, want it to say created", first)
	}

	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(base, home) {
		t.Fatalf("config dir %s is not under the isolated home %s", base, home)
	}
	if _, err := os.Stat(filepath.Join(base, "faultline", "ca.crt")); err != nil {
		t.Fatalf("ca.crt was not created in the config dir: %v", err)
	}

	second := runCmd(t, "ca", "init")
	if !strings.Contains(second, "already exists") {
		t.Errorf("second init output = %q, want it to say already exists", second)
	}
}

func TestCAPathPrintsCertificatePath(t *testing.T) {
	isolateConfigDir(t)
	runCmd(t, "ca", "init")

	got := strings.TrimSpace(runCmd(t, "ca", "path"))
	if filepath.Base(got) != "ca.crt" {
		t.Errorf("ca path = %q, want a path ending in ca.crt", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("printed path does not exist: %v", err)
	}
}

func TestCAPathBeforeInitPointsAtInit(t *testing.T) {
	isolateConfigDir(t)

	err := runCmdErr(t, "ca", "path")
	if err == nil || !strings.Contains(err.Error(), "faultline ca init") {
		t.Fatalf("ca path error = %v, want one suggesting faultline ca init", err)
	}
}

func TestCAInstallDeclinedPrintsInstructionsOnly(t *testing.T) {
	isolateConfigDir(t)
	runCmd(t, "ca", "init")
	certPath := strings.TrimSpace(runCmd(t, "ca", "path"))

	got := runCmdIn(t, "n\n", "ca", "install")
	if !strings.Contains(got, certPath) {
		t.Errorf("install output does not name the certificate %q:\n%s", certPath, got)
	}
	if !strings.Contains(got, "untouched") && !strings.Contains(got, "by hand") &&
		!strings.Contains(got, "certutil") && !strings.Contains(got, "documentation") {
		t.Errorf("install output neither declined nor explained a manual path:\n%s", got)
	}
	if strings.Contains(got, "now trusted") {
		t.Errorf("install claims the CA is trusted although the prompt was declined:\n%s", got)
	}
}

func TestCAInstallBeforeInitPointsAtInit(t *testing.T) {
	isolateConfigDir(t)

	err := runCmdErr(t, "ca", "install")
	if err == nil || !strings.Contains(err.Error(), "faultline ca init") {
		t.Fatalf("ca install error = %v, want one suggesting faultline ca init", err)
	}
}
