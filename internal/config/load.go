package config

import (
	"fmt"
	"os"

	yaml "go.yaml.in/yaml/v3"
)

// topLevel is every key a configuration file may have.
var topLevel = []string{"routes", "bypass", "rules", "scenarios"}

// Load reads and checks a configuration file. A missing file is reported as
// such, wrapping os.ErrNotExist, so a caller can treat "no file" as "nothing
// configured" without reading the message.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the path is the operator's own config file
	if err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}
	return Parse(path, data)
}

// Parse checks a configuration file that has already been read. The name is
// used in error messages and recorded as the config's path.
func Parse(name string, data []byte) (*Config, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, syntaxError(name, err)
	}

	cfg := &Config{Path: name}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return cfg, nil // an empty file, or nothing but comments
	}

	root := cursor{file: name, path: "", node: resolve(doc.Content[0])}
	m, err := root.mapping("a configuration file")
	if err != nil {
		return nil, err
	}
	if err := m.only("a configuration file", topLevel...); err != nil {
		return nil, err
	}

	if c, ok := m.value("routes"); ok {
		if cfg.Routes, err = decodeRoutes(c); err != nil {
			return nil, err
		}
	}
	if c, ok := m.value("bypass"); ok {
		if cfg.Bypass, err = decodeBypass(c); err != nil {
			return nil, err
		}
	}

	// Rules are read before scenarios so a scenario can name any of them,
	// wherever it sits in the file, and inline rules join the same set.
	set := &ruleSet{ids: map[string]bool{}}
	if c, ok := m.value("rules"); ok {
		if err := decodeRules(c, set); err != nil {
			return nil, err
		}
	}
	if c, ok := m.value("scenarios"); ok {
		if cfg.Scenarios, err = decodeScenarios(c, set); err != nil {
			return nil, err
		}
	}
	cfg.Rules = set.rules

	return cfg, nil
}
