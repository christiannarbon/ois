package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResolveAnchorsAndMergeKeys(t *testing.T) {
	s := mustParseFixture(t, "profiles-anchors.yaml")
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	prod := mustResolve(t, s, "prod")
	if got := installed(prod.Spec); got != "storage,kafka,trino" {
		t.Fatalf("prod components = %s", got)
	}
	if got := installed(mustResolve(t, s, "stage").Spec); got != "storage,kafka" {
		t.Fatalf("stage components = %s", got)
	}
}

func installed(s *Spec) string {
	var out []string
	for _, name := range ComponentNames {
		if s.Enabled(name) {
			out = append(out, name)
		}
	}
	return strings.Join(out, ",")
}

func TestResolveMergeKeyUnknownFieldPointsAtAnchor(t *testing.T) {
	yml := header + "components:\n  storage: {}\nprofiles:\n  prod:\n    context: c\n    components:\n      kafka: &k {replicas: 3}\n      trino:\n        <<: *k\n"
	s, err := Parse([]byte(yml), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	errs := validationErrors(t, s.Validate())
	fe := findError(t, errs, "profiles.prod.components.trino.replicas", "unknown field")
	if fe.Line != 9 {
		t.Fatalf("line = %d, want 9 (the anchored mapping)", fe.Line)
	}
}

func TestResolveMergeKeyNeedsMapping(t *testing.T) {
	yml := header + "components:\n  storage: {}\nprofiles:\n  prod:\n    context: c\n    components:\n      kafka:\n        <<: 5\n"
	s, err := Parse([]byte(yml), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fe := findError(t, validationErrors(t, s.Validate()), "profiles.prod.components", "<< needs a mapping")
	if fe.Line != 10 {
		t.Fatalf("line = %d, want 10 (the << key)", fe.Line)
	}
}

func TestResolveTooManyAliases(t *testing.T) {
	var b strings.Builder
	b.WriteString(header + "components: {storage: {}}\nprofiles:\n  prod:\n    context: c\n    components:\n      x0: &x0 [a, b, c, d, e, f, g, h, i, j]\n")
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&b, "      x%d: &x%d [", i, i)
		for j := range 10 {
			if j > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "*x%d", i-1)
		}
		b.WriteString("]\n")
	}
	s, err := Parse([]byte(b.String()), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Resolve("prod")
	findError(t, validationErrors(t, err), "profiles.prod.components", "too many aliases")
}

// TestStrictChecksAgree runs every fixture through both strict checks: yaml.v3's
// KnownFields (used by Parse) and unknownKeys (used for merged profile nodes). They must
// agree on the first unknown key, or profile overrides drift from the top level.
func TestStrictChecksAgree(t *testing.T) {
	files, err := filepath.Glob("testdata/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("fixtures: %v", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var doc yaml.Node
			if err := yaml.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			root, err := expandAliases(doc.Content[0])
			if err != nil {
				t.Fatal(err)
			}
			var viaNodes string
			if errs := unknownKeys(root, reflect.TypeFor[Spec](), ""); len(errs) > 0 {
				viaNodes = errs[0].Path
			}
			var viaDecoder string
			var de *DecodeError
			if _, err := Parse(data, "ois.yaml"); errors.As(err, &de) && strings.HasPrefix(de.Msg, "unknown field") {
				viaDecoder = de.Path
			}
			if viaNodes != viaDecoder {
				t.Fatalf("unknownKeys says %q, Parse says %q", viaNodes, viaDecoder)
			}
		})
	}
}
