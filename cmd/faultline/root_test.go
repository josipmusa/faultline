package main

import (
	"bytes"
	"strings"
	"testing"
)

// runCmd executes the root command with args and returns its combined output.
func runCmd(t *testing.T, args ...string) string {
	t.Helper()

	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)

	if err := root.Execute(); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}

	return out.String()
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	got := runCmd(t, "version")
	if want := "faultline " + version; !strings.Contains(got, want) {
		t.Errorf("version output = %q, want it to contain %q", got, want)
	}
}

func TestRootHelpListsSubcommands(t *testing.T) {
	got := runCmd(t, "--help")
	for _, name := range []string{"serve", "run", "rule", "scenario", "ca", "mcp", "version"} {
		if !strings.Contains(got, name) {
			t.Errorf("help output is missing subcommand %q:\n%s", name, got)
		}
	}
}
