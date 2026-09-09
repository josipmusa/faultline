package config

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

const defaultMode fs.FileMode = 0o644

// render writes the tree back out at the two space indent the examples and the
// docs use, restoring the blank lines the encoder drops.
func render(doc *yaml.Node, original []byte) ([]byte, error) {
	newSpacer(original, doc).space(doc)

	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("config: writing the file: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("config: writing the file: %w", err)
	}
	return emptyBlanks(out.Bytes()), nil
}

// emptyBlanks turns the indentation-only lines the encoder leaves behind back
// into empty ones. A gap above a node is asked for with a newline at the front
// of its head comment, and the encoder indents every line of a comment block,
// that empty one included, so a gap inside a sequence comes out as spaces.
//
// The value of a block scalar is left exactly as it is, because spaces on a
// line there are part of what the rule says, not layout.
func emptyBlanks(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	block := -1 // the indent of the key that opened a block scalar, or -1 outside one

	for i, line := range lines {
		bare := bytes.TrimLeft(line, " ")
		indent := len(line) - len(bare)

		if block >= 0 {
			if len(bare) == 0 || indent > block {
				continue // still inside the value
			}
			block = -1
		}
		switch {
		case len(bytes.TrimRight(line, " ")) == 0:
			lines[i] = nil
		case opensBlockScalar(bare):
			block = indent
		}
	}
	return bytes.Join(lines, []byte("\n"))
}

// opensBlockScalar reports whether a line ends in a block scalar indicator, so
// that the lines under it are a value rather than more structure. The key it
// belongs to has to be there as well: a path pattern ending in > is a value on
// one line, not the start of a folded one.
func opensBlockScalar(line []byte) bool {
	space := bytes.LastIndexByte(line, ' ')
	if space < 0 || !bytes.HasSuffix(bytes.TrimRight(line[:space], " "), []byte(":")) {
		return false
	}
	switch string(line[space+1:]) {
	case "|", "|-", "|+", ">", ">-", ">+":
		return true
	}
	return false
}

// spacer puts back the empty lines the encoder drops. The encoder writes
// comments but no blank lines, so without this every save would close the gaps
// in a file a person spaced out on purpose. A leading newline on a head comment
// is how the encoder is asked for a blank line, an otherwise empty head comment
// asks for one on its own, and the parser strips both again on the way back in,
// so this holds over any number of saves.
type spacer struct {
	source []string
	done   map[int]bool
	// The encoder already writes a blank line after the comment at the head of
	// the file, so whatever comes first must not ask for a second one.
	exempt *yaml.Node
}

func newSpacer(original []byte, doc *yaml.Node) *spacer {
	s := &spacer{source: strings.Split(string(original), "\n"), done: map[int]bool{}}
	if doc.HeadComment != "" && len(doc.Content) > 0 {
		s.exempt = doc.Content[0]
		if root := doc.Content[0]; root.Kind == yaml.MappingNode && len(root.Content) > 0 {
			s.exempt = root.Content[0]
		}
	}
	return s
}

// space walks the places a comment can be written: the key of a mapping entry
// and an item of a list.
func (s *spacer) space(n *yaml.Node) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			s.above(n.Content[i])
			s.space(n.Content[i+1])
		}
	case yaml.SequenceNode:
		for _, item := range n.Content {
			s.above(item)
			s.space(item)
		}
	default:
		for _, c := range n.Content {
			s.space(c)
		}
	}
}

func (s *spacer) above(n *yaml.Node) {
	if n == s.exempt || n.Line <= 0 || s.done[n.Line] {
		return
	}
	s.done[n.Line] = true // an item and its first key sit on one line and share the gap above it

	first := n.Line - strings.Count(n.HeadComment, "\n") - 1 // the line the head comment starts on
	if n.HeadComment == "" {
		first = n.Line
	}
	if above := first - 2; above >= 0 && above < len(s.source) && strings.TrimSpace(s.source[above]) == "" {
		n.HeadComment = "\n" + n.HeadComment
	}
}

// replaceFile writes data over path in one step: a temporary file in the same
// directory, then a rename, so a reader either sees the old file or the new one
// and never half of either.
func replaceFile(path string, data []byte) error {
	mode := defaultMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".faultline-*.yaml")
	if err != nil {
		return fmt.Errorf("config: creating a temporary file next to %s: %w", path, err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // does nothing once the rename has happened

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: writing %s: %w", name, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: setting the mode of %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: closing %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("config: replacing %s: %w", path, err)
	}
	return nil
}
