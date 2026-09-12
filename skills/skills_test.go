// Package skills_test guards the shipped agent skills. A skill reaches an
// agent as a name and a description in a list, and that description is the
// whole of what decides whether the skill is ever consulted. When the
// frontmatter does not parse, the loader drops the description and the skill
// is listed as a bare name: it looks installed, it is named correctly, and it
// can never trigger. Nothing in the repository notices, because the file is
// documentation and every test still passes. This is that notice.
package skills_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// frontmatter is what a harness reads. Unknown fields are refused so a typo in
// a key is a failure here rather than a silently ignored line.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// minDescription is a floor, not a target. A description this short cannot
// carry both what the skill does and when to reach for it, which is what a
// model matches a request against.
const minDescription = 120

func TestEverySkillHasParseableFrontmatter(t *testing.T) {
	found, err := filepath.Glob(filepath.Join("*", "SKILL.md"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("no skills found; this test is guarding nothing")
	}

	for _, path := range found {
		t.Run(filepath.Dir(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			block, ok := yamlFrontmatter(string(raw))
			if !ok {
				t.Fatalf("%s does not open with a --- delimited frontmatter block", path)
			}

			dec := yaml.NewDecoder(strings.NewReader(block))
			dec.KnownFields(true)
			var fm frontmatter
			if err := dec.Decode(&fm); err != nil {
				// The usual cause is ": " inside an unquoted value, which YAML
				// reads as a nested mapping. Quote the value or rewrite the
				// colon away.
				t.Fatalf("%s frontmatter does not parse, so the description is dropped and the skill cannot trigger: %v", path, err)
			}

			if want := filepath.Dir(path); fm.Name != want {
				t.Errorf("name is %q but the directory is %q; a harness addresses the skill by directory", fm.Name, want)
			}
			if n := len(fm.Description); n < minDescription {
				t.Errorf("description is %d characters, under the %d floor; it has to say what the skill does and when to use it", n, minDescription)
			}
		})
	}
}

// yamlFrontmatter returns the block between the opening --- and the next one.
func yamlFrontmatter(s string) (string, bool) {
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return "", false
	}
	block, _, ok := strings.Cut(rest, "\n---")
	if !ok {
		return "", false
	}
	return block, true
}
