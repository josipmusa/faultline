package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josipmusa/faultline/internal/config"
	"github.com/josipmusa/faultline/internal/rules"
)

func TestSaveWritesWhatTheRunningInstanceHolds(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	i := newInstance(t)
	if err := i.rules.Add(rules.Rule{
		ID: "slow-stripe", Name: "Stripe is slow", Enabled: true,
		Match: rules.Match{Host: "api.stripe.com"},
		Fault: rules.Fault{Type: "delay", Params: rules.Params{"ms": 2000, "jitter_ms": 500}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := i.scenarios.Add(rules.Scenario{Name: "payments-slow", Rules: []string{"slow-stripe"}}); err != nil {
		t.Fatal(err)
	}

	out := i.run(t, "save")

	if !strings.Contains(out, "wrote 1 rule and 1 scenario to "+DefaultConfigFile) {
		t.Errorf("save said %q, want it to say what it wrote and where", out)
	}
	cfg, err := config.Load(filepath.Join(dir, DefaultConfigFile))
	if err != nil {
		t.Fatalf("the saved file does not load: %v", err)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].ID != "slow-stripe" || !cfg.Rules[0].Enabled {
		t.Errorf("the saved file holds %+v, want the rule the instance had", cfg.Rules)
	}
	if len(cfg.Scenarios) != 1 || cfg.Scenarios[0].Name != "payments-slow" {
		t.Errorf("the saved file holds %+v, want the scenario the instance had", cfg.Scenarios)
	}
	body, _ := os.ReadFile(filepath.Join(dir, DefaultConfigFile))
	if strings.Index(string(body), "ms: 2000") > strings.Index(string(body), "jitter_ms") {
		t.Errorf("the parameters are not in the order a person writes them:\n%s", body)
	}
}

func TestSaveWritesTheFileItIsNamed(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	i := newInstance(t)
	if err := i.rules.Add(rules.Rule{ID: "r", Name: "r", Enabled: true, Fault: rules.Fault{Type: "delay", Params: rules.Params{"ms": 10}}}); err != nil {
		t.Fatal(err)
	}

	if err := i.runErr(t, "save", "ops/faults.yaml"); err == nil || !strings.Contains(err.Error(), "no directory ops") {
		t.Fatalf("err = %v, want it to say the directory is missing", err)
	}

	if err := os.Mkdir(filepath.Join(dir, "ops"), 0o750); err != nil {
		t.Fatal(err)
	}
	i.run(t, "save", "ops/faults.yaml")

	if _, err := os.Stat(filepath.Join(dir, "ops", "faults.yaml")); err != nil {
		t.Fatalf("save did not write the file it was named: %v", err)
	}
}

func TestSaveRefusesToWriteOverAFileThatIsThere(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, dir, DefaultConfigFile, "rules: []\n")
	i := newInstance(t)
	if err := i.rules.Add(rules.Rule{ID: "r", Name: "r", Enabled: true, Fault: rules.Fault{Type: "delay", Params: rules.Params{"ms": 10}}}); err != nil {
		t.Fatal(err)
	}

	err := i.runErr(t, "save")

	if err == nil || !strings.Contains(err.Error(), "already there") {
		t.Fatalf("err = %v, want a refusal that names the file in the way", err)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, DefaultConfigFile)); string(body) != "rules: []\n" {
		t.Errorf("the file was written over anyway:\n%s", body)
	}
}

func TestSaveHasNothingToDoWhenNothingIsHeld(t *testing.T) {
	t.Chdir(t.TempDir())
	i := newInstance(t)

	err := i.runErr(t, "save")

	if err == nil || !strings.Contains(err.Error(), "nothing to save") {
		t.Fatalf("err = %v, want it to say there is nothing to save", err)
	}
}
