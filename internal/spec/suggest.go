package spec

import (
	"reflect"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var (
	keysOnce   sync.Once
	keysByType map[string][]string
)

// yamlKeys returns the yaml tag names of a spec struct type, by Go type name ("Components").
func yamlKeys(typeName string) []string {
	keysOnce.Do(func() {
		keysByType = map[string][]string{}
		for _, t := range []reflect.Type{reflect.TypeFor[Spec](), reflect.TypeFor[External]()} {
			collectKeys(t)
		}
	})
	return keysByType[typeName]
}

func collectKeys(t reflect.Type) {
	t = elemType(t)
	if t.Kind() != reflect.Struct || t == nodeType {
		return
	}
	if _, seen := keysByType[t.Name()]; seen {
		return
	}
	keysByType[t.Name()] = nil
	names, types := yamlFields(t)
	keysByType[t.Name()] = names
	for _, name := range names {
		collectKeys(types[name])
	}
}

var nodeType = reflect.TypeFor[yaml.Node]()

func elemType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	return t
}

// yamlFields returns the yaml keys of struct type t in field order, with each field's type.
// External has no yaml tags; its keys are the lowercased field names.
func yamlFields(t reflect.Type) ([]string, map[string]reflect.Type) {
	var names []string
	types := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		switch {
		case name == "-":
			continue
		case name == "":
			name = strings.ToLower(f.Name)
		}
		names = append(names, name)
		types[name] = f.Type
	}
	return names, types
}

// suggest returns the closest candidate within Levenshtein distance 2, or "".
func suggest(got string, candidates []string) string {
	best, bestDist := "", 3
	for _, c := range candidates {
		if d := levenshtein(got, c); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
