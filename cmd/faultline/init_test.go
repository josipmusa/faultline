package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWritesAConfigTheLoaderAccepts(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	out := runCmd(t, "init")
	if !strings.Contains(out, DefaultConfigFile) {
		t.Errorf("init said %q, want it to name the file it wrote", out)
	}

	cfg, err := loadConfig("")
	if err != nil {
		t.Fatalf("loading the file init wrote: %v", err)
	}
	if cfg == nil || len(cfg.Rules) == 0 || len(cfg.Scenarios) == 0 {
		t.Fatalf("the file init wrote loaded as %+v, want the example rule and scenario", cfg)
	}
}

func TestInitRefusesToOverwriteAndSaysHow(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	const mine = "rules: []\n"
	writeFile(t, dir, DefaultConfigFile, mine)

	err := runCmdErr(t, "init")
	if err == nil {
		t.Fatal("init overwrote a configuration that was already there")
	}
	for _, want := range []string{DefaultConfigFile, "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to mention %q", err, want)
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, DefaultConfigFile)) //nolint:gosec // the test wrote this path
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != mine {
		t.Errorf("the file on disk is now %q, want the refusal to have left it alone", after)
	}
}

func TestInitOverwritesWhenForced(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, DefaultConfigFile, "rules: []\n")

	runCmd(t, "init", "--force")

	cfg, err := loadConfig("")
	if err != nil {
		t.Fatalf("loading the file init --force wrote: %v", err)
	}
	if cfg == nil || len(cfg.Rules) == 0 {
		t.Fatalf("--force left %+v behind, want the starter file", cfg)
	}
}

func TestInitWritesTheFileNamedOnTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(t.TempDir())

	path := filepath.Join(dir, "ops.yaml")
	out := runCmd(t, "init", path)
	if !strings.Contains(out, path) {
		t.Errorf("init said %q, want it to name %q", out, path)
	}
	if _, err := loadConfig(path); err != nil {
		t.Fatalf("loading %s: %v", path, err)
	}
	if _, err := os.Stat(DefaultConfigFile); err == nil {
		t.Error("init wrote faultline.yaml in the working directory as well as the path it was given")
	}
}

func TestInitSaysWhenThePathIsADirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(t.TempDir())

	err := runCmdErr(t, "init", dir)
	if err == nil {
		t.Fatal("init accepted a directory as the file to write")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("err = %q, want it to say the path is a directory", err)
	}
}
