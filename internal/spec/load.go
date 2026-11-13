package spec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load reads and strictly decodes ois.yaml, records field positions, sets Dir/File and
// applies defaults. It does not run Validate.
func Load(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data, path)
}

// DecodeError is a decode problem tied to a line.
type DecodeError struct {
	File string // shown as its base name
	Line int
	Path string // best-effort field path, e.g. "components.stroage"
	Msg  string
}

func (e *DecodeError) Error() string {
	var b strings.Builder
	b.WriteString(filepath.Base(e.File))
	if e.Line > 0 {
		fmt.Fprintf(&b, ":%d", e.Line)
	}
	if e.Path != "" {
		b.WriteString(": " + e.Path)
	}
	b.WriteString(": " + e.Msg)
	return b.String()
}

// Parse is Load for bytes; file is used for Dir/File and error messages.
func Parse(data []byte, file string) (*Spec, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, convertError(err, file, nil)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, fmt.Errorf("%s is empty", filepath.Base(file))
	}
	pos := map[string]int{}
	recordPositions(doc.Content[0], "", pos)

	// Decode the original bytes strictly so error lines match the file. The node is
	// decoded afterwards only for its values, once nulls under components are fixed.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&Spec{}); err != nil {
		return nil, convertError(err, file, pos)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, &DecodeError{File: file, Line: extra.Line, Msg: "only one YAML document is allowed"}
	}

	enableNullComponents(doc.Content[0])
	s := &Spec{}
	if err := doc.Decode(s); err != nil {
		return nil, convertError(err, file, pos)
	}
	s.File = abs
	s.Dir = filepath.Dir(abs)
	s.pos = pos
	if comps := mappingValue(doc.Content[0], "components"); comps != nil && (comps.Kind == yaml.MappingNode || comps.Kind == yaml.AliasNode) {
		s.componentsNode = comps
	}
	applyDefaults(s)
	return s, nil
}

func recordPositions(n *yaml.Node, path string, pos map[string]int) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, val := n.Content[i], n.Content[i+1]
			p := key.Value
			if path != "" {
				p = path + "." + key.Value
			}
			pos[p] = key.Line
			recordPositions(val, p, pos)
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			p := fmt.Sprintf("%s[%d]", path, i)
			pos[p] = item.Line
			recordPositions(item, p, pos)
		}
	}
}

// enableNullComponents turns "components:\n  storage:" into storage: {} so a bare key
// enables the component instead of decoding to a nil pointer. It does this for the
// top-level components and for every profiles.<name>.components.
func enableNullComponents(root *yaml.Node) {
	nullsToMappings(mappingValue(root, "components"))
	if profiles := mappingValue(root, "profiles"); profiles != nil && profiles.Kind == yaml.MappingNode {
		for i := 1; i < len(profiles.Content); i += 2 {
			nullsToMappings(mappingValue(profiles.Content[i], "components"))
		}
	}
}

func nullsToMappings(comps *yaml.Node) {
	if comps == nil || comps.Kind != yaml.MappingNode {
		return
	}
	for j := 1; j < len(comps.Content); j += 2 {
		if v := comps.Content[j]; v.Tag == "!!null" {
			comps.Content[j] = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: v.Line, Column: v.Column}
		}
	}
}

// mappingValue returns the value node of key in mapping n, or nil.
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

var (
	unknownFieldRe = regexp.MustCompile(`^line (\d+): field (\S+) not found in type spec\.(\w+)$`)
	typeErrRe      = regexp.MustCompile("^line (\\d+): cannot unmarshal !!(\\w+)(?: `(.*)`)? into (\\S+)$")
	syntaxErrRe    = regexp.MustCompile(`^yaml: line (\d+): (.+)$`)
)

func convertError(err error, file string, pos map[string]int) error {
	var te *yaml.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		msg := te.Errors[0]
		if m := unknownFieldRe.FindStringSubmatch(msg); m != nil {
			line, _ := strconv.Atoi(m[1])
			de := &DecodeError{File: file, Line: line, Path: keyPath(pos, line, m[2]), Msg: "unknown field"}
			if s := suggest(m[2], yamlKeys(m[3])); s != "" {
				de.Msg += fmt.Sprintf(" (did you mean %q?)", s)
			}
			return de
		}
		if m := typeErrRe.FindStringSubmatch(msg); m != nil {
			line, _ := strconv.Atoi(m[1])
			return &DecodeError{File: file, Line: line, Path: deepestPath(pos, line), Msg: expected(m[4], m[3])}
		}
		return &DecodeError{File: file, Msg: msg}
	}
	if m := syntaxErrRe.FindStringSubmatch(err.Error()); m != nil {
		line, _ := strconv.Atoi(m[1])
		return &DecodeError{File: file, Line: line, Msg: m[2]}
	}
	return &DecodeError{File: file, Msg: strings.TrimPrefix(err.Error(), "yaml: ")}
}

func expected(goType, got string) string {
	var want string
	switch {
	case strings.HasPrefix(goType, "int"), strings.HasPrefix(goType, "uint"), strings.HasPrefix(goType, "float"):
		want = "a number"
	case goType == "string":
		want = "a string"
	case goType == "bool":
		want = "true or false"
	case strings.HasPrefix(goType, "[]"):
		want = "a list"
	default:
		want = "a mapping"
	}
	if got != "" {
		return fmt.Sprintf("expected %s, got %q", want, got)
	}
	return "expected " + want
}

// keyPath finds the recorded path of the key named field on line.
func keyPath(pos map[string]int, line int, field string) string {
	best := ""
	for p, l := range pos {
		if l == line && (p == field || strings.HasSuffix(p, "."+field)) &&
			(len(p) > len(best) || len(p) == len(best) && p < best) {
			best = p
		}
	}
	if best == "" {
		return field
	}
	return best
}

// deepestPath returns the most nested key path recorded on line, or "". Sequence items
// are skipped: a type error on a line is about the value of the key that starts it.
func deepestPath(pos map[string]int, line int) string {
	best := ""
	for p, l := range pos {
		if l == line && !strings.HasSuffix(p, "]") && (len(p) > len(best) || len(p) == len(best) && p < best) {
			best = p
		}
	}
	return best
}
