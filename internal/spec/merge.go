package spec

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// mergeNodes deep-merges override onto base (both MappingNodes) and returns a new node.
// Mappings merge key by key; any other kind in override replaces the base value.
// Line numbers of override nodes are kept so decode errors point into the profile.
func mergeNodes(base, override *yaml.Node) *yaml.Node {
	switch {
	case base == nil:
		return cloneNode(override)
	case override == nil:
		return cloneNode(base)
	}
	out := cloneNode(base)
	for i := 0; i+1 < len(override.Content); i += 2 {
		key, val := override.Content[i], override.Content[i+1]
		j := keyIndex(out, key.Value)
		switch {
		case j < 0:
			out.Content = append(out.Content, cloneNode(key), cloneNode(val))
		case out.Content[j+1].Kind == yaml.MappingNode && val.Kind == yaml.MappingNode:
			out.Content[j+1] = mergeNodes(out.Content[j+1], val)
		default:
			out.Content[j+1] = cloneNode(val)
		}
	}
	return out
}

// cloneNode deep-copies n, so callers may change the result without touching the spec.
func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	if n.Content != nil {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, child := range n.Content {
			c.Content[i] = cloneNode(child)
		}
	}
	return &c
}

func keyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// liftExternal removes `external` keys from the merged components mapping and returns
// them by component name, with the line of each `external` key.
func liftExternal(components *yaml.Node) (map[string]*yaml.Node, map[string]int) {
	nodes, lines := map[string]*yaml.Node{}, map[string]int{}
	if components == nil {
		return nodes, lines
	}
	for i := 0; i+1 < len(components.Content); i += 2 {
		comp, body := components.Content[i].Value, components.Content[i+1]
		if body.Kind != yaml.MappingNode {
			continue
		}
		if j := keyIndex(body, "external"); j >= 0 {
			nodes[comp], lines[comp] = body.Content[j+1], body.Content[j].Line
			body.Content = append(body.Content[:j], body.Content[j+2:]...)
		}
	}
	return nodes, lines
}

// unknownKeys reports every mapping key in n that type t does not declare, at the given
// path. It is the strict check for nodes decoded with node.Decode, which ignores
// KnownFields, and it keeps each key's own line.
func unknownKeys(n *yaml.Node, t reflect.Type, path string) []FieldError {
	t = deref(t)
	if n == nil || t == nodeType {
		return nil
	}
	join := func(key string) string {
		if path == "" {
			return key
		}
		return path + "." + key
	}
	var errs []FieldError
	switch {
	case t.Kind() == reflect.Struct && n.Kind == yaml.MappingNode:
		names, types := yamlFields(t)
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			ft, ok := types[key.Value]
			if !ok {
				msg := "unknown field"
				if s := suggest(key.Value, names); s != "" {
					msg += fmt.Sprintf(" (did you mean %q?)", s)
				}
				errs = append(errs, FieldError{Path: join(key.Value), Msg: msg, Line: key.Line})
				continue
			}
			errs = append(errs, unknownKeys(n.Content[i+1], ft, join(key.Value))...)
		}
	case t.Kind() == reflect.Map && n.Kind == yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			errs = append(errs, unknownKeys(n.Content[i+1], t.Elem(), join(n.Content[i].Value))...)
		}
	case t.Kind() == reflect.Slice && n.Kind == yaml.SequenceNode:
		for i, item := range n.Content {
			errs = append(errs, unknownKeys(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	return errs
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// decodeStrict checks n for unknown keys and then decodes it into out. Type errors keep
// the node's line; their path is the deepest key recorded on that line.
func (s *Spec) decodeStrict(n *yaml.Node, out any, path string) []FieldError {
	if errs := unknownKeys(n, reflect.TypeOf(out), path); len(errs) > 0 {
		return errs
	}
	if err := n.Decode(out); err != nil {
		de, ok := convertError(err, s.File, s.pos, s.vals).(*DecodeError)
		if !ok {
			return []FieldError{{Path: path, Msg: err.Error()}}
		}
		p := de.Path
		if p == "" || !strings.HasPrefix(p, path) {
			p = path
		}
		return []FieldError{{Path: p, Msg: de.Msg, Line: de.Line}}
	}
	return nil
}

// maxExpandedNodes caps alias expansion, so a few nested anchors cannot grow into
// millions of nodes.
const maxExpandedNodes = 100_000

// expandError is an alias or merge-key problem at a line.
type expandError struct {
	Line int
	Msg  string
}

func (e *expandError) Error() string { return e.Msg }

// expandFieldError turns an expandAliases error into a FieldError at path.
func expandFieldError(path string, err error) FieldError {
	fe := FieldError{Path: path, Msg: err.Error()}
	var ee *expandError
	if errors.As(err, &ee) {
		fe.Line = ee.Line
	}
	return fe
}

// expandAliases returns a copy of n as yaml.v3's decoder sees it: aliases are replaced by
// the node they point to and "<<" merge keys are folded into their mapping, explicit keys
// winning. Expanded nodes keep the lines of their anchors.
func expandAliases(n *yaml.Node) (*yaml.Node, error) {
	budget := maxExpandedNodes
	return expand(n, &budget)
}

func expand(n *yaml.Node, budget *int) (*yaml.Node, error) {
	if n == nil {
		return nil, nil
	}
	if *budget--; *budget < 0 {
		return nil, &expandError{Line: n.Line, Msg: fmt.Sprintf("too many aliases (anchors expand to more than %d nodes)", maxExpandedNodes)}
	}
	if n.Kind == yaml.AliasNode {
		return expand(n.Alias, budget)
	}
	c := *n
	c.Content = nil
	var inherited []*yaml.Node
	for i := 0; i < len(n.Content); i++ {
		if n.Kind == yaml.MappingNode && i+1 < len(n.Content) && n.Content[i].Tag == "!!merge" {
			key := n.Content[i]
			i++
			src, err := expand(n.Content[i], budget)
			if err != nil {
				return nil, err
			}
			sources := []*yaml.Node{src}
			if src.Kind == yaml.SequenceNode {
				sources = src.Content
			}
			for _, m := range sources {
				if m.Kind != yaml.MappingNode {
					return nil, &expandError{Line: key.Line, Msg: "<< needs a mapping or a list of mappings"}
				}
				inherited = append(inherited, m.Content...)
			}
			continue
		}
		child, err := expand(n.Content[i], budget)
		if err != nil {
			return nil, err
		}
		c.Content = append(c.Content, child)
	}
	for i := 0; i+1 < len(inherited); i += 2 {
		if keyIndex(&c, inherited[i].Value) < 0 {
			c.Content = append(c.Content, inherited[i], inherited[i+1])
		}
	}
	return &c, nil
}
