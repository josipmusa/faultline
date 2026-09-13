package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/compose"
)

const exampleCompose = `name: example
services:
  app:
    image: app
  db:
    image: postgres
`

func TestComposeInjectWritesAnOverrideForTheExample(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "compose.yaml", exampleCompose)

	out := runCmd(t, "compose", "inject", "--service", "app")
	if !strings.Contains(out, "docker compose -f compose.yaml -f "+compose.DefaultOutput) {
		t.Errorf("inject said %q, want it to print the command to run", out)
	}
	if !strings.Contains(out, "trust java") {
		t.Errorf("inject said %q, want it to say what a JVM needs", out)
	}

	written, err := os.ReadFile(filepath.Join(dir, compose.DefaultOutput)) //nolint:gosec // the test named this path
	if err != nil {
		t.Fatalf("reading the override: %v", err)
	}
	for _, want := range []string{"faultline-ca:", "http://faultline:9001", compose.CACertPath} {
		if !strings.Contains(string(written), want) {
			t.Errorf("the override does not contain %q:\n%s", want, written)
		}
	}
}

func TestComposeInjectRefusesToOverwriteAndSaysHow(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "compose.yaml", exampleCompose)
	writeFile(t, dir, compose.DefaultOutput, "mine\n")

	err := runCmdErr(t, "compose", "inject", "--service", "app")
	if err == nil {
		t.Fatal("inject overwrote an override that was already there")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("refusal = %q, want it to mention --force", err)
	}

	runCmd(t, "compose", "inject", "--service", "app", "--force")
	after, err := os.ReadFile(filepath.Join(dir, compose.DefaultOutput)) //nolint:gosec // the test named this path
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "mine") {
		t.Error("--force left the old file in place")
	}
}

func TestComposeInjectFindsTheSourceAndTakesOneNamed(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "stack.yaml", exampleCompose)

	if err := runCmdErr(t, "compose", "inject", "--service", "app"); err == nil {
		t.Fatal("inject found a Compose file where there is none")
	}

	runCmd(t, "compose", "inject", "-f", "stack.yaml", "--service", "app", "-o", "out.yml")
	if _, err := os.Stat(filepath.Join(dir, "out.yml")); err != nil {
		t.Fatalf("inject wrote no override to the name it was given: %v", err)
	}
}

func TestComposeInjectWarnsAboutAProxyTheServiceAlreadyHas(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, "compose.yaml", "services:\n  app:\n    environment:\n      HTTP_PROXY: http://elsewhere:3128\n")

	out := runCmd(t, "compose", "inject", "--service", "app")
	if !strings.Contains(out, "HTTP_PROXY") {
		t.Errorf("inject said %q, want a warning about the proxy the service already sets", out)
	}
}
