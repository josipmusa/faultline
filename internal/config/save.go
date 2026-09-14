package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/josipmusa/faultline/internal/rules"
)

// Save writes rs as the rules, bypass as the bypass list and scenarios as the
// named situations of the file at path, and leaves everything else it holds
// exactly as it was: the comments and the routes.
//
// The file is edited as a YAML tree rather than written out from a struct, so a
// rule nobody touched keeps the words it was written with, and a rule that did
// change keeps the comment above it. A rule the file does not know yet is
// appended to the top level rules; the order of the rules already there is left
// alone, because the file belongs to whoever wrote it.
//
// A scenario the file already declares is left exactly as it is, including the
// rules written inside it; only one the file has never seen is appended. A
// scenario's rules list is not rewritten, because it may hold a whole rule
// written in place and writing that back as an id would leave a file that no
// longer loads. Dropping the entries that named a deleted rule is a rule
// change, and is done above.
//
// Save refuses a file it cannot read in full, rather than overwriting what it
// does not understand.
func Save(path string, rs []rules.Rule, bypass []string, scenarios []rules.Scenario) error {
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
	if err := e.applyScenarios(scenarios); err != nil {
		return err
	}

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

	scenarios *yaml.Node      // the scenarios list, nil when the file has none
	declared  map[string]bool // the names the file already declares
}

func (e *edit) collect() {
	e.defs, e.named = map[string]place{}, map[string][]place{}

	if seq, ok := child(e.root, "rules"); ok && seq.Kind == yaml.SequenceNode {
		e.top = seq
		for _, item := range seq.Content {
			e.record(place{seq: seq, node: item, enabled: true})
		}
	}

	e.declared = map[string]bool{}

	scenarios, ok := child(e.root, "scenarios")
	if !ok || scenarios.Kind != yaml.SequenceNode {
		return
	}
	e.scenarios = scenarios
	for _, scenario := range scenarios.Content {
		if name, named := child(resolve(scenario), "name"); named && name.Kind == yaml.ScalarNode {
			e.declared[name.Value] = true
		}
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

// rewriteChanged writes every rule that no longer says what the file says
// into the mapping the file already holds, key by key. The order the author
// wrote the keys in, the comments on each of them and the line they sit on all
// survive an edit to one value, so a save that changed a number changes one
// line of the file.
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
		// Enabled is written only when leaving it out would mean the opposite:
		// a rule inside a scenario is off unless the file says so, one at the
		// top level on. Turning a scenario on writes enabled on its rules, so
		// the flags survive a reload while the rehearsal runs; turning it off
		// again takes them out, so the file is left exactly as it was
		// committed.
		if rule.Enabled == p.enabled {
			dropKey(node, "enabled")
		}
		mergeMapping(resolve(p.node), node)
	}
	return nil
}

// mergeMapping makes dst say what src says while keeping dst's own order and
// comments. A key both hold keeps its place, and its value is merged the same
// way when both are mappings or replaced when they are not; a key only dst
// holds is dropped; a key only src holds is inserted after the key that
// precedes it in src, so a new key lands where the canonical order puts it
// among the keys already there.
func mergeMapping(dst, src *yaml.Node) {
	content := make([]*yaml.Node, 0, len(src.Content))
	kept := make(map[string]bool, len(src.Content)/2)

	for i := 0; i+1 < len(dst.Content); i += 2 {
		key, value := dst.Content[i], dst.Content[i+1]
		name := resolve(key).Value
		next, ok := child(src, name)
		if !ok {
			continue
		}
		kept[name] = true
		if old := resolve(value); old.Kind == yaml.MappingNode && next.Kind == yaml.MappingNode {
			mergeMapping(old, next)
			content = append(content, key, value)
			continue
		}
		// The comment on a value's line is about the key, so it moves to the
		// new value the way the key itself stays.
		next.HeadComment, next.LineComment, next.FootComment = value.HeadComment, value.LineComment, value.FootComment
		content = append(content, key, next)
	}

	for i := 0; i+1 < len(src.Content); i += 2 {
		name := src.Content[i].Value
		if kept[name] {
			continue
		}
		at := 0
		if i > 0 {
			at = indexOfKey(content, src.Content[i-2].Value) + 2
		}
		content = slices.Insert(content, at, src.Content[i], src.Content[i+1])
		kept[name] = true
	}
	dst.Content = content
}

// indexOfKey is the position of key in a mapping's content, or -2 so that the
// slot after a key nothing holds is the start.
func indexOfKey(content []*yaml.Node, key string) int {
	for i := 0; i+1 < len(content); i += 2 {
		if resolve(content[i]).Value == key {
			return i
		}
	}
	return -2
}

// dropKey removes key and its value from a mapping.
func dropKey(m *yaml.Node, key string) {
	if i := indexOfKey(m.Content, key); i >= 0 {
		m.Content = slices.Delete(m.Content, i, i+2)
	}
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

// applyScenarios appends every scenario the file does not declare yet, in the
// order it is given, naming its rules by id.
//
// Appending is all it does. A scenario already written keeps the rules list it
// was written with, comments, inline rules and all, because that list is the
// file's way of saying where a rule lives and this function has no way to say
// it back. Being written last is also what keeps a new scenario loadable: the
// loader reads scenarios in order, so an entry appended at the end can name any
// rule the file holds, including one written inside an earlier scenario.
func (e *edit) applyScenarios(scenarios []rules.Scenario) error {
	for _, scenario := range scenarios {
		if e.declared[scenario.Name] {
			continue
		}
		for _, id := range scenario.Rules {
			if _, written := e.defs[id]; !written {
				return fmt.Errorf("config: the scenario %q names the rule %q, which is not in the file; "+
					"nothing was written, because a file naming a rule that is not there would not load",
					scenario.Name, id)
			}
		}
		e.scenarios = appendTo(e.root, e.scenarios, "scenarios", scenarioNode(scenario))
		e.declared[scenario.Name] = true
	}
	return nil
}

// scenarioNode writes a scenario as the file spells one: a name and the ids of
// the rules it turns on.
func scenarioNode(s rules.Scenario) *yaml.Node {
	list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, id := range s.Rules {
		list.Content = append(list.Content, scalarNode(id))
	}

	m := newMapping()
	m.add("name", scalarNode(s.Name))
	m.add("rules", list)
	return m.node
}

// appendTo adds node to the sequence under key, creating that key at the end of
// the mapping when the file has none.
//
// A key the file did not have is a section of its own, so it is written with a
// blank line above it, the way a file somebody wrote spaces its sections out. A
// leading newline on the head comment is how the encoder is asked for that.
func appendTo(root, seq *yaml.Node, key string, node *yaml.Node) *yaml.Node {
	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		name := scalarNode(key)
		if len(root.Content) > 0 {
			name.HeadComment = "\n"
		}
		root.Content = append(root.Content, name, seq)
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
