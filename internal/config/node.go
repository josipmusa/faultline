package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// cursor is one node of the parsed file together with where it sits: the file
// it came from and the path a user would use to talk about it. Every error
// raised while reading a file comes from a cursor, so no error can be written
// without a line and a field.
type cursor struct {
	file string
	path string
	node *yaml.Node
}

func (c cursor) errf(format string, args ...any) *Error {
	return &Error{File: c.file, Line: c.node.Line, Path: c.path, Message: fmt.Sprintf(format, args...)}
}

// field returns the cursor for a named child, whether or not the child exists;
// when it does not, the node stays put and only the path grows, so an error
// about a missing field still points at the mapping that should have held it.
func (c cursor) field(name string, node *yaml.Node) cursor {
	path := name
	if c.path != "" {
		path = c.path + "." + name
	}
	if node == nil {
		node = c.node
	}
	return cursor{file: c.file, path: path, node: resolve(node)}
}

func (c cursor) index(i int, node *yaml.Node) cursor {
	return cursor{file: c.file, path: c.path + "[" + strconv.Itoa(i) + "]", node: resolve(node)}
}

// at keeps the path and moves to another node, for pointing an error at the key
// rather than at the value.
func (c cursor) at(node *yaml.Node) cursor {
	return cursor{file: c.file, path: c.path, node: resolve(node)}
}

// resolve follows aliases, so a value written once with an anchor and used
// again with an alias reads as the value it stands for.
func resolve(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// mapping is a YAML mapping whose keys have been indexed. Keys keep their order
// so errors come out in the order they are written.
type mapping struct {
	c      cursor
	names  []string
	keys   map[string]*yaml.Node
	values map[string]*yaml.Node
}

func (c cursor) mapping(what string) (*mapping, error) {
	if c.node.Kind != yaml.MappingNode {
		return nil, c.errf("%s must be a mapping of names to values, this is %s", what, describe(c.node))
	}

	m := &mapping{c: c, keys: map[string]*yaml.Node{}, values: map[string]*yaml.Node{}}
	for i := 0; i+1 < len(c.node.Content); i += 2 {
		key, value := resolve(c.node.Content[i]), c.node.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			return nil, c.at(key).errf("%s has a key that is not a name", what)
		}
		if _, taken := m.values[key.Value]; taken {
			return nil, c.at(key).errf("%s names %q twice", what, key.Value)
		}
		m.names = append(m.names, key.Value)
		m.keys[key.Value] = key
		m.values[key.Value] = value
	}
	return m, nil
}

// only rejects keys the caller did not name, pointing at the key itself. It is
// how a typo in a field name becomes a line number rather than a silently
// ignored setting.
func (m *mapping) only(what string, allowed ...string) error {
	for _, name := range m.names {
		if !slices.Contains(allowed, name) {
			return m.c.field(name, m.keys[name]).
				errf("unknown key %q; %s takes %s", name, what, strings.Join(allowed, ", "))
		}
	}
	return nil
}

// value returns the cursor for a key's value, and whether the key is there.
func (m *mapping) value(name string) (cursor, bool) {
	node, ok := m.values[name]
	return m.c.field(name, node), ok
}

// require is value for a key that has to be written.
func (m *mapping) require(name, what string) (cursor, error) {
	c, ok := m.value(name)
	if !ok {
		return c, c.errf("%s is required", what)
	}
	return c, nil
}

// locate walks a dotted field path as far as the file goes and returns the
// deepest cursor it reaches, extending the path the whole way. A validator that
// names a field it did not find, like a required parameter that is absent, so
// still gets a line: the one holding the mapping the field belongs to.
func (m *mapping) locate(path string) cursor {
	c := m.c
	current := m
	for _, name := range strings.Split(path, ".") {
		next, ok := current.values[name]
		c = c.field(name, next)
		if !ok {
			return c
		}
		child, err := c.mapping("")
		if err != nil {
			return c
		}
		current = child
	}
	return c
}

func (c cursor) sequence(what string) ([]cursor, error) {
	if c.node.Kind != yaml.SequenceNode {
		return nil, c.errf("%s must be a list, this is %s", what, describe(c.node))
	}
	out := make([]cursor, 0, len(c.node.Content))
	for i, item := range c.node.Content {
		out = append(out, c.index(i, item))
	}
	return out, nil
}

func (c cursor) text(what string) (string, error) {
	if c.node.Kind != yaml.ScalarNode || c.node.Tag == "!!null" {
		return "", c.errf("%s must be text, this is %s", what, describe(c.node))
	}
	return c.node.Value, nil
}

func (c cursor) number(what string) (int, error) {
	var n int
	if c.node.Kind != yaml.ScalarNode || c.node.Tag != "!!int" || c.node.Decode(&n) != nil {
		return 0, c.errf("%s must be a whole number, this is %s", what, describe(c.node))
	}
	return n, nil
}

func (c cursor) boolean(what string) (bool, error) {
	var b bool
	if c.node.Kind != yaml.ScalarNode || c.node.Tag != "!!bool" || c.node.Decode(&b) != nil {
		return false, c.errf("%s must be true or false, this is %s", what, describe(c.node))
	}
	return b, nil
}

func (c cursor) textMap(what string) (map[string]string, error) {
	m, err := c.mapping(what)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(m.names))
	for _, name := range m.names {
		value, valueErr := m.c.field(name, m.values[name]).text(name)
		if valueErr != nil {
			return nil, valueErr
		}
		out[name] = value
	}
	return out, nil
}

// plain turns a node into the ordinary Go value a fault parameter holds. It
// takes only what JSON can carry, so a parameter behaves the same whether it
// arrived in a file or over the API.
func (c cursor) plain(what string) (any, error) {
	switch c.node.Kind {
	case yaml.MappingNode:
		m, err := c.mapping(what)
		if err != nil {
			return nil, err
		}
		out := make(map[string]any, len(m.names))
		for _, name := range m.names {
			value, valueErr := m.c.field(name, m.values[name]).plain(name)
			if valueErr != nil {
				return nil, valueErr
			}
			out[name] = value
		}
		return out, nil
	case yaml.SequenceNode:
		items, err := c.sequence(what)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(items))
		for _, item := range items {
			value, valueErr := item.plain(what)
			if valueErr != nil {
				return nil, valueErr
			}
			out = append(out, value)
		}
		return out, nil
	case yaml.ScalarNode:
		return c.scalar(what)
	default:
		return nil, c.errf("%s is not a value Faultline understands", what)
	}
}

func (c cursor) scalar(what string) (any, error) {
	switch c.node.Tag {
	case "!!str":
		return c.node.Value, nil
	case "!!null":
		return nil, nil
	case "!!bool":
		return c.boolean(what)
	case "!!int":
		return c.number(what)
	case "!!float":
		var f float64
		if err := c.node.Decode(&f); err != nil {
			return nil, c.errf("%s is not a number Faultline understands", what)
		}
		return f, nil
	default:
		return nil, c.errf("%s must be text, a number, or true or false", what)
	}
}

func describe(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		return "a mapping"
	case yaml.SequenceNode:
		return "a list"
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return "empty"
		}
		return "a single value"
	default:
		return "something else"
	}
}
