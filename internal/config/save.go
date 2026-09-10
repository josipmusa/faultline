package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	yaml "go.yaml.in/yaml/v3"

	"github.com/josipmusa/faultline/internal/rules"
)

// Save writes rs as the rules and bypass as the bypass list of the file at
// path, and leaves everything else it holds exactly as it was: the comments,
// the routes, and the scenarios with the rules written inside them.
//
// The file is edited as a YAML tree rather than written out from a struct, so a
// rule nobody touched keeps the words it was written with, and a rule that did
// change keeps the comment above it. A rule the file does not know yet is
// appended to the top level rules; the order of the rules already there is left
// alone, because the file belongs to whoever wrote it.
//
// Save refuses a file it cannot read in full, rather than overwriting what it
// does not understand.
func Save(path string, rs []rules.Rule, bypass []string) error {
	data, err := os.ReadFile(path) // #nosec G304 -- the path is the operator's own config file
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("config: reading %s: %w", path, err)
	}
	if _, err := Parse(path, data); err != nil {
		return err
	}

	doc, root, err := document(path, data)
	if err != nil {
		return err
	}

	e := &edit{file: path, root: root}
	e.collect()
	if err := e.apply(rs); err != nil {
		return err
	}
	e.applyBypass(bypass)

	out, err := render(doc, data)
	if err != nil {
		return err
	}
	return replaceFile(path, out)
}

// document parses data into a tree to edit, making an empty one when the file
// is missing or holds nothing but comments.
func document(file string, data []byte) (doc, root *yaml.Node, err error) {
	doc = &yaml.Node{}
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, nil, syntaxError(file, err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Kind, doc.Content = yaml.DocumentNode, []*yaml.Node{root}
		return doc, root, nil
	}

	root = resolve(doc.Content[0])
	if root.Kind != yaml.MappingNode {
		return nil, nil, (cursor{file: file, node: root}).errf(
			"a configuration file must be a mapping of names to values, this is %s", describe(root))
	}
	return doc, root, nil
}

// place is where one rule is written: the list holding it, the node that says
// what it is, and what leaving enabled out means there.
type place struct {
	seq     *yaml.Node
	node    *yaml.Node
	enabled bool
}

func (p place) read(file string) (rules.Rule, error) {
	rule, _, err := decodeRule(cursor{file: file, node: resolve(p.node)}, p.enabled)
	return rule, err
}

// edit is one pass over the tree: where every rule is written, and the changes
// to make to it.
type edit struct {
	file string
	root *yaml.Node

	top   *yaml.Node         // the top level rules list, nil when the file has none
	defs  map[string]place   // id to the node that defines that rule
	named map[string][]place // id to the scenario entries that only name it
}

func (e *edit) collect() {
	e.defs, e.named = map[string]place{}, map[string][]place{}

	if seq, ok := child(e.root, "rules"); ok && seq.Kind == yaml.SequenceNode {
		e.top = seq
		for _, item := range seq.Content {
			e.record(place{seq: seq, node: item, enabled: true})
		}
	}

	scenarios, ok := child(e.root, "scenarios")
	if !ok || scenarios.Kind != yaml.SequenceNode {
		return
	}
	for _, scenario := range scenarios.Content {
		list, listed := child(resolve(scenario), "rules")
		if !listed || list.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range list.Content {
			if id := resolve(item); id.Kind == yaml.ScalarNode {
				e.named[id.Value] = append(e.named[id.Value], place{seq: list, node: item})
				continue
			}
			// A rule written inside a scenario waits for the scenario, so
			// leaving enabled out there means off.
			e.record(place{seq: list, node: item, enabled: false})
		}
	}
}

func (e *edit) record(p place) {
	id, ok := child(resolve(p.node), "id")
	if !ok || id.Kind != yaml.ScalarNode {
		return // Parse has already accepted the file, so this cannot be a rule
	}
	e.defs[id.Value] = p
}

func (e *edit) apply(rs []rules.Rule) error {
	want := make(map[string]rules.Rule, len(rs))
	for _, r := range rs {
		want[r.ID] = r
	}

	e.dropGone(want)
	if err := e.rewriteChanged(want); err != nil {
		return err
	}
	return e.appendNew(rs)
}

// dropGone removes every rule the store no longer holds, and with it the
// scenario entries that named it, so the file it leaves behind still loads.
func (e *edit) dropGone(want map[string]rules.Rule) {
	gone := map[*yaml.Node]bool{}
	for id, p := range e.defs {
		if _, keep := want[id]; !keep {
			gone[p.node] = true
			delete(e.defs, id)
		}
	}
	for id, places := range e.named {
		if _, keep := want[id]; keep {
			continue
		}
		for _, p := range places {
			gone[p.node] = true
		}
		delete(e.named, id)
	}
	if len(gone) > 0 {
		prune(e.root, gone)
	}
}

func (e *edit) rewriteChanged(want map[string]rules.Rule) error {
	for id, p := range e.defs {
		rule := want[id]
		if written, err := p.read(e.file); err == nil && rules.Same(written, rule) {
			continue
		}
		node, err := ruleNode(rule)
		if err != nil {
			return err
		}
		// The comment above a rule is about the rule, not about the version of
		// it that is being replaced, and the line it was on is what says
		// whether a blank line separated it from what came before.
		node.HeadComment, node.LineComment, node.FootComment = p.node.HeadComment, p.node.LineComment, p.node.FootComment
		node.Line = p.node.Line
		*p.node = *node
	}
	return nil
}

func (e *edit) appendNew(rs []rules.Rule) error {
	for _, r := range rs {
		if _, written := e.defs[r.ID]; written {
			continue
		}
		node, err := ruleNode(r)
		if err != nil {
			return err
		}
		e.top = appendTo(e.root, e.top, "rules", node)
		e.defs[r.ID] = place{seq: e.top, node: node, enabled: true}
	}
	return nil
}

// applyBypass writes hosts as the bypass list. An entry already written keeps
// the node it was written with, so its spelling and the comment above it
// survive; a new one is appended and one no longer on the list is dropped.
//
// A file with no bypass list and nothing to write is left alone, so Faultline's
// own defaults never appear in somebody's configuration. An emptied list keeps
// its key, because dropping the key would make the next load read the hosts
// that were just taken off it.
func (e *edit) applyBypass(hosts []string) {
	seq, written := child(e.root, "bypass")
	if !written {
		if len(hosts) == 0 {
			return
		}
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		e.root.Content = append(e.root.Content, scalarNode("bypass"), seq)
	}

	kept := make(map[string]*yaml.Node, len(seq.Content))
	for _, item := range seq.Content {
		if node := resolve(item); node.Kind == yaml.ScalarNode {
			kept[node.Value] = item
		}
	}

	next := make([]*yaml.Node, 0, len(hosts))
	for _, host := range hosts {
		if node, ok := kept[host]; ok {
			next = append(next, node)
			continue
		}
		next = append(next, scalarNode(host))
	}
	seq.Content = next
}

// appendTo adds node to the sequence under key, creating that key at the end of
// the mapping when the file has none.
func appendTo(root, seq *yaml.Node, key string, node *yaml.Node) *yaml.Node {
	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, scalarNode(key), seq)
	}
	seq.Content = append(seq.Content, node)
	return seq
}

// prune removes the given nodes from every sequence in the tree.
func prune(n *yaml.Node, gone map[*yaml.Node]bool) {
	if n.Kind == yaml.SequenceNode {
		kept := n.Content[:0]
		for _, item := range n.Content {
			if !gone[item] {
				kept = append(kept, item)
			}
		}
		n.Content = kept
	}
	for _, c := range n.Content {
		prune(c, gone)
	}
}

func child(m *yaml.Node, key string) (*yaml.Node, bool) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := resolve(m.Content[i]); k.Kind == yaml.ScalarNode && k.Value == key {
			return resolve(m.Content[i+1]), true
		}
	}
	return nil, false
}
