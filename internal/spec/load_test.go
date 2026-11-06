package spec

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// parseFixture parses testdata/<name> as if it were a file called ois.yaml.
func parseFixture(t *testing.T, name string) (*Spec, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return Parse(data, "ois.yaml")
}

func TestParseMinimal(t *testing.T) {
	s, err := parseFixture(t, "minimal.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if s.APIVersion != APIVersion || s.Project != "demo" {
		t.Fatalf("got apiVersion %q project %q", s.APIVersion, s.Project)
	}
	for _, name := range ComponentNames {
		if got := s.Enabled(name); got != (name == "storage") {
			t.Errorf("Enabled(%s) = %v", name, got)
		}
	}
}

func TestParseNullComponentEnables(t *testing.T) {
	s, err := parseFixture(t, "null-component.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Enabled("storage") || !s.Enabled("kafka") {
		t.Fatalf("storage %v kafka %v, want both enabled", s.Enabled("storage"), s.Enabled("kafka"))
	}
	if s.Enabled("catalog") {
		t.Fatal("catalog enabled but not listed")
	}
}

func TestParseFull(t *testing.T) {
	s, err := parseFixture(t, "full.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if s.Cluster != (Cluster{Size: "custom", CPUs: 4, Memory: 8192, Disk: "40g"}) {
		t.Errorf("cluster = %+v", s.Cluster)
	}
	if len(s.Streams) != 2 || s.Streams[1].Name != "orders" {
		t.Errorf("streams = %+v", s.Streams)
	}
	if len(s.Maintenance) != 1 || s.Maintenance[0].Table != "lake.clicks" {
		t.Errorf("maintenance = %+v", s.Maintenance)
	}
	if !slices.Equal(s.Samples, []string{"clickstream"}) {
		t.Errorf("samples = %v", s.Samples)
	}
	lines := map[string]int{
		"apiVersion":         3,
		"cluster.cpus":       8,
		"components.storage": 13,
		"streams":            19,
		"streams[1]":         21,
		"streams[1].name":    21,
		"maintenance[0]":     24,
		"samples[0]":         26,
	}
	for path, want := range lines {
		if got := s.Line(path); got != want {
			t.Errorf("Line(%q) = %d, want %d", path, got, want)
		}
	}
	if s.Line("nope") != 0 {
		t.Error("Line of an unknown path is not 0")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		fixture string
		want    string
		without string
	}{
		{fixture: "typo-component.yaml", want: `ois.yaml:4: components.stroage: unknown field (did you mean "storage"?)`},
		{fixture: "typo-top.yaml", want: `ois.yaml:3: compnents: unknown field (did you mean "components"?)`},
		{fixture: "unknown-no-suggestion.yaml", want: `ois.yaml:3: zzzzzz: unknown field`, without: "did you mean"},
		{fixture: "bad-type.yaml", want: `ois.yaml:6: cluster.cpus: expected a number`},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			_, err := parseFixture(t, tt.fixture)
			if err == nil {
				t.Fatal("no error")
			}
			var de *DecodeError
			if !errors.As(err, &de) {
				t.Fatalf("err %T is not a *DecodeError: %v", err, err)
			}
			if !strings.HasPrefix(err.Error(), tt.want) {
				t.Fatalf("err = %q, want prefix %q", err, tt.want)
			}
			if tt.without != "" && strings.Contains(err.Error(), tt.without) {
				t.Fatalf("err = %q, must not contain %q", err, tt.without)
			}
		})
	}
	_, err := parseFixture(t, "typo-component.yaml")
	if want := `ois.yaml:4: components.stroage: unknown field (did you mean "storage"?)`; err.Error() != want {
		t.Fatalf("err = %q, want exactly %q", err, want)
	}
}

func TestParseInline(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", "ois.yaml is empty"},
		{"only comments", "# nothing\n", "ois.yaml is empty"},
		{"not a mapping", "- a\n", "ois.yaml:1: expected a mapping"},
		{"syntax", "project: demo\n  bad: [\n", "ois.yaml:2:"},
		{"two documents", "project: a\n---\nproject: b\n", "only one YAML document"},
		{"nested typo", "project: demo\ncluster:\n  szie: lean\n", `ois.yaml:3: cluster.szie: unknown field (did you mean "size"?)`},
		{"stream typo", "streams:\n  - name: a\n  - nmae: b\n", `ois.yaml:3: streams[1].nmae: unknown field (did you mean "name"?)`},
		{"components not a mapping", "components: [storage]\n", "ois.yaml:1: components: expected a mapping"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.in), "ois.yaml")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "minimal.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "ois.yaml")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if s.File != file || s.Dir != dir || !filepath.IsAbs(s.Dir) {
		t.Fatalf("File %q Dir %q", s.File, s.Dir)
	}
	if got := s.Line("components.storage"); got != 3 {
		t.Fatalf("Line(components.storage) = %d, want 3", got)
	}
	if got, want := s.Path("dbt"), filepath.Join(dir, "dbt"); got != want {
		t.Errorf("Path(dbt) = %q, want %q", got, want)
	}
	if got := s.Path("/abs/x"); got != "/abs/x" {
		t.Errorf("Path(/abs/x) = %q", got)
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("Load of a missing file did not fail")
	}
}

func TestLoadRelativeDirIsAbsolute(t *testing.T) {
	s, err := Load(filepath.Join("testdata", "minimal.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(s.Dir) || !filepath.IsAbs(s.File) {
		t.Fatalf("Dir %q File %q are not absolute", s.Dir, s.File)
	}
}

func TestEnabledUnknownPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Enabled(nope) did not panic")
		}
	}()
	(&Spec{}).Enabled("nope")
}

func TestSuggest(t *testing.T) {
	tests := []struct {
		got        string
		candidates []string
		want       string
	}{
		{"stroage", ComponentNames, "storage"},
		{"kafak", ComponentNames, "kafka"},
		{"trno", ComponentNames, "trino"},
		{"zzzzzz", ComponentNames, ""},
		{"compnents", yamlKeys("Spec"), "components"},
		{"szie", yamlKeys("Cluster"), "size"},
		{"x", nil, ""},
	}
	for _, tt := range tests {
		if got := suggest(tt.got, tt.candidates); got != tt.want {
			t.Errorf("suggest(%q) = %q, want %q", tt.got, got, tt.want)
		}
	}
}

func TestYAMLKeys(t *testing.T) {
	if got := yamlKeys("Components"); !slices.Equal(got, ComponentNames) {
		t.Errorf("Components keys = %v, want %v", got, ComponentNames)
	}
	if got := yamlKeys("Spec"); slices.Contains(got, "Dir") || slices.Contains(got, "pos") || !slices.Contains(got, "samples") {
		t.Errorf("Spec keys = %v", got)
	}
	if got := yamlKeys("Stream"); !slices.Equal(got, []string{"name"}) {
		t.Errorf("Stream keys = %v", got)
	}
}
