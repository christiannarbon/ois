package spec

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// FieldError is one validation problem.
type FieldError struct {
	Path string // e.g. "cluster.cpus", "streams[1].name"
	Msg  string
	Line int // filled by Validate from Spec.Line(Path) when 0
}

// ValidationError holds every problem, sorted by Line then Path.
type ValidationError struct {
	File   string
	Errors []FieldError
}

// Error renders one problem per line: "ois.yaml:12: streams[0].name: must match ^[a-z][a-z0-9-]*$"
// (File is shown as its base name). Line 0 prints as "ois.yaml: <path>: <msg>".
func (e *ValidationError) Error() string {
	name := filepath.Base(e.File)
	lines := make([]string, len(e.Errors))
	for i, fe := range e.Errors {
		loc := name
		if fe.Line > 0 {
			loc = fmt.Sprintf("%s:%d", name, fe.Line)
		}
		lines[i] = fmt.Sprintf("%s: %s: %s", loc, fe.Path, fe.Msg)
	}
	return strings.Join(lines, "\n")
}

// Rule is one named check.
type Rule struct {
	Name  string
	Check func(s *Spec) []FieldError
}

var (
	rulesMu sync.Mutex
	rules   = map[string]Rule{}
)

// RegisterRule adds a rule. Call it from init() in rules_<topic>.go files.
// Duplicate names panic.
func RegisterRule(r Rule) {
	if r.Name == "" || r.Check == nil {
		panic("spec: RegisterRule needs a name and a check")
	}
	rulesMu.Lock()
	defer rulesMu.Unlock()
	if _, dup := rules[r.Name]; dup {
		panic(fmt.Sprintf("spec: rule %q registered twice", r.Name))
	}
	rules[r.Name] = r
}

func sortedRules() []Rule {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Rule) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// Validate runs every rule in registration-name order and returns *ValidationError or nil.
func (s *Spec) Validate() error {
	var errs []FieldError
	for _, r := range sortedRules() {
		for _, fe := range r.Check(s) {
			if fe.Line == 0 {
				fe.Line = s.nearestLine(fe.Path)
			}
			errs = append(errs, fe)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	slices.SortStableFunc(errs, func(a, b FieldError) int {
		return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Path, b.Path))
	})
	return &ValidationError{File: s.File, Errors: errs}
}

// nearestLine returns the line of path or, failing that, of its closest recorded parent:
// streams[1].source.topic -> streams[1].source -> streams[1] -> streams.
func (s *Spec) nearestLine(path string) int {
	for path != "" {
		if l := s.Line(path); l > 0 {
			return l
		}
		cut := max(strings.LastIndexByte(path, '.'), strings.LastIndexByte(path, '['))
		if cut < 0 {
			return 0
		}
		path = path[:cut]
	}
	return 0
}

// LoadAndValidate is Load followed by Validate.
func LoadAndValidate(path string) (*Spec, error) {
	s, err := Load(path)
	if err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}
