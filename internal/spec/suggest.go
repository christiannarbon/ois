package spec

import (
	"reflect"
	"strings"
	"sync"
)

var (
	keysOnce   sync.Once
	keysByType map[string][]string
)

// yamlKeys returns the yaml tag names of a spec struct type, by Go type name ("Components").
func yamlKeys(typeName string) []string {
	keysOnce.Do(func() {
		keysByType = map[string][]string{}
		collectKeys(reflect.TypeFor[Spec]())
	})
	return keysByType[typeName]
}

func collectKeys(t reflect.Type) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	if _, seen := keysByType[t.Name()]; seen {
		return
	}
	keys := []string{}
	keysByType[t.Name()] = keys
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if !f.IsExported() || name == "-" || name == "" {
			continue
		}
		keys = append(keys, name)
		collectKeys(f.Type)
	}
	keysByType[t.Name()] = keys
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
