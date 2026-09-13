// Package compose reads a project's Compose file and renders the override that
// attaches Faultline to it as a companion container.
//
// It reads the source only far enough to validate what it is about to write:
// the names already taken, and which of the named services already configures
// a proxy of its own. Nothing here rewrites the source, and the override is
// rendered as text rather than marshalled, because a generated file someone
// has to read is worth its comments.
package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Source is the project's own Compose file, read for the names in it.
type Source struct {
	// Path is where the file was read from, used in messages.
	Path string

	services map[string]service
	volumes  map[string]yaml.Node
	order    []string
}

type service struct {
	// Environment is either a mapping or a list of KEY=VALUE, and Compose
	// accepts both, so it stays a node until something asks about it.
	Environment yaml.Node `yaml:"environment"`
}

type sourceFile struct {
	Services yaml.Node `yaml:"services"`
	Volumes  yaml.Node `yaml:"volumes"`
}

// ParseSource reads a Compose file. It fails on YAML it cannot read and on a
// file that declares no services, which is never the file the caller meant.
func ParseSource(path string, data []byte) (*Source, error) {
	var file sourceFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	src := &Source{
		Path:     path,
		services: map[string]service{},
		volumes:  map[string]yaml.Node{},
	}
	if file.Services.Kind != 0 {
		if err := file.Services.Decode(&src.services); err != nil {
			return nil, fmt.Errorf("reading the services in %s: %w", path, err)
		}
		src.order = mappingKeys(&file.Services)
	}
	if file.Volumes.Kind == yaml.MappingNode {
		if err := file.Volumes.Decode(&src.volumes); err != nil {
			return nil, fmt.Errorf("reading the volumes in %s: %w", path, err)
		}
	}
	if len(src.services) == 0 {
		return nil, fmt.Errorf("%s declares no services; name the Compose file of the project "+
			"with -f", path)
	}
	return src, nil
}

// ServiceNames are the services in the file, in the order they appear in it.
func (s *Source) ServiceNames() []string {
	return append([]string(nil), s.order...)
}

// HasService says whether the file declares a service of that name.
func (s *Source) HasService(name string) bool {
	_, ok := s.services[name]
	return ok
}

// HasVolume says whether the file declares a volume of that name.
func (s *Source) HasVolume(name string) bool {
	_, ok := s.volumes[name]
	return ok
}

// SetsProxy says whether the service already points at a proxy of its own. The
// override would take precedence over it, which is usually what was wanted and
// is always worth saying out loud.
func (s *Source) SetsProxy(name string) bool {
	svc, ok := s.services[name]
	if !ok {
		return false
	}
	for _, key := range environmentKeys(&svc.Environment) {
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY":
			return true
		}
	}
	return false
}

// environmentKeys are the variable names a service sets, whether its
// environment is written as a mapping or as a list of KEY=VALUE.
func environmentKeys(node *yaml.Node) []string {
	switch node.Kind {
	case yaml.MappingNode:
		return mappingKeys(node)
	case yaml.SequenceNode:
		keys := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			key, _, _ := strings.Cut(item.Value, "=")
			keys = append(keys, key)
		}
		return keys
	default:
		return nil
	}
}

func mappingKeys(node *yaml.Node) []string {
	keys := make([]string, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keys = append(keys, node.Content[i].Value)
	}
	return keys
}

// SourceNames are the file names Compose itself looks for, in the order it
// prefers them.
var SourceNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// Discover finds the project's Compose file in dir, the way Compose would.
func Discover(dir string) (string, error) {
	for _, name := range SourceNames {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("no Compose file here: looked for %s; name one with -f",
		strings.Join(SourceNames, ", "))
}
