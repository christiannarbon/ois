package spec

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const testSample = "test-sample"

// probeProject makes the test-only rule fire; no real spec uses this name.
const probeProject = "rule-probe"

func init() {
	RegisterSample(testSample)
	RegisterRule(Rule{Name: "zz-test-probe", Check: func(s *Spec) []FieldError {
		if s.Project != probeProject {
			return nil
		}
		return []FieldError{{Path: "project", Msg: "probe rule ran"}}
	}})
}

const header = "apiVersion: ois/v1alpha1\nproject: demo\n"

func validate(t *testing.T, yml string) *ValidationError {
	t.Helper()
	s, err := Parse([]byte(yml), "ois.yaml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	err = s.Validate()
	if err == nil {
		return nil
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate returned %T, want *ValidationError: %v", err, err)
	}
	return ve
}

func TestValidateMinimal(t *testing.T) {
	if ve := validate(t, header+"components: {storage: {}}\n"); ve != nil {
		t.Fatalf("unexpected errors:\n%v", ve)
	}
}

func TestValidateDefaultsSize(t *testing.T) {
	s, err := Parse([]byte(header+"components: {storage: {}}\n"), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if s.Cluster.Size != "lean" {
		t.Fatalf("cluster.size = %q, want lean", s.Cluster.Size)
	}
}

func TestValidateFull(t *testing.T) {
	full := header + `cluster: {size: custom, cpus: 4, memory: 8192, disk: 40g}
components:
  storage:
  kafka:
  catalog:
  flink:
  trino:
  dagster:
  superset:
streams:
  - name: clicks
  - name: orders-2
transform: {}
maintenance:
  - table: lake.clicks
dashboards: {}
samples: [test-sample]
`
	if ve := validate(t, full); ve != nil {
		t.Fatalf("unexpected errors:\n%v", ve)
	}
}

func TestRuleFailures(t *testing.T) {
	storage := "components: {storage: {}}\n"
	tests := []struct {
		rule, yml, path, msg string
	}{
		{"api-version", "apiVersion: ois/v1\nproject: demo\n" + storage, "apiVersion", `must be "ois/v1alpha1"`},
		{"project-name", "apiVersion: ois/v1alpha1\nproject: Demo_1\n" + storage, "project", "must be 2-30 characters"},
		{"project-name too long", "apiVersion: ois/v1alpha1\nproject: " + strings.Repeat("a", 31) + "\n" + storage, "project", "must be 2-30"},
		{"project-name trailing dash", "apiVersion: ois/v1alpha1\nproject: demo-\n" + storage, "project", "must be 2-30"},
		{"cluster-size unknown", header + "cluster: {size: huge}\n" + storage, "cluster.size", "must be one of minimal, lean, custom"},
		{"cluster-size custom cpus", header + "cluster: {size: custom, cpus: 1, memory: 4096, disk: 20g}\n" + storage, "cluster.cpus", "at least 2"},
		{"cluster-size custom memory", header + "cluster: {size: custom, cpus: 2, memory: 2048, disk: 20g}\n" + storage, "cluster.memory", "at least 4096"},
		{"cluster-size custom disk", header + "cluster: {size: custom, cpus: 2, memory: 4096, disk: 20GB}\n" + storage, "cluster.disk", `like "40g"`},
		{"cluster-size lean with cpus", header + "cluster: {size: lean, cpus: 4}\n" + storage, "cluster.cpus", "only allowed with size: custom"},
		{"components-present", header + "components: {}\n", "components", "at least one component"},
		{"streams-require", header + "components: {storage: {}, kafka: {}, catalog: {}}\nstreams:\n  - name: a\n", "streams", "needs components.flink"},
		{"stream-names pattern", header + "components: {storage: {}, kafka: {}, catalog: {}, flink: {}}\nstreams:\n  - name: Bad_Name\n", "streams[0].name", "must match"},
		{"stream-names duplicate", header + "components: {storage: {}, kafka: {}, catalog: {}, flink: {}}\nstreams:\n  - name: a\n  - name: a\n", "streams[1].name", `duplicate stream name "a" (also streams[0])`},
		{"stream-names empty", header + "components: {storage: {}, kafka: {}, catalog: {}, flink: {}}\nstreams:\n  - name: \"\"\n", "streams[0].name", "required"},
		{"transform-requires", header + "components: {storage: {}, trino: {}}\ntransform: {}\n", "transform", "needs components.dagster"},
		{"maintenance-requires", header + "components: {storage: {}, dagster: {}}\nmaintenance:\n  - table: t\n", "maintenance", "needs components.trino"},
		{"maintenance table", header + "components: {storage: {}, dagster: {}, trino: {}}\nmaintenance:\n  - table: \"\"\n", "maintenance[0].table", "required"},
		{"dashboards-require", header + storage + "dashboards: {}\n", "dashboards", "needs components.superset"},
		{"samples-known unknown", header + storage + "samples: [nope]\n", "samples[0]", `unknown sample "nope" (known: test-sample)`},
		{"samples-known duplicate", header + storage + "samples: [test-sample, test-sample]\n", "samples[1]", "duplicate sample"},
		{"storage-required", header + "components: {kafka: {}}\n", "components", "storage must be enabled; kafka store data in it"},
	}
	for _, tt := range tests {
		t.Run(tt.rule, func(t *testing.T) {
			ve := validate(t, tt.yml)
			if ve == nil {
				t.Fatal("no errors")
			}
			for _, fe := range ve.Errors {
				if fe.Path == tt.path && strings.Contains(fe.Msg, tt.msg) {
					if fe.Line <= 0 {
						t.Fatalf("%s: Line = %d, want > 0", fe.Path, fe.Line)
					}
					return
				}
			}
			t.Fatalf("no error at %s containing %q in:\n%v", tt.path, tt.msg, ve)
		})
	}
}

func TestRuleStreamsRequireOnePerComponent(t *testing.T) {
	ve := validate(t, header+"components: {storage: {}}\nstreams:\n  - name: a\n")
	var msgs []string
	for _, fe := range ve.Errors {
		if fe.Path == "streams" {
			msgs = append(msgs, fe.Msg)
		}
	}
	want := []string{"needs components.catalog to be enabled", "needs components.flink to be enabled", "needs components.kafka to be enabled"}
	slices.Sort(msgs)
	if !slices.Equal(msgs, want) {
		t.Fatalf("streams errors = %q, want %q", msgs, want)
	}
}

func TestRuleCustomWithoutCPUsUsesClusterLine(t *testing.T) {
	ve := validate(t, header+"components: {storage: {}}\ncluster:\n  size: custom\n  memory: 4096\n  disk: 20g\n")
	if ve == nil || len(ve.Errors) != 1 {
		t.Fatalf("want exactly one error, got:\n%v", ve)
	}
	if fe := ve.Errors[0]; fe.Path != "cluster.cpus" || fe.Line != 4 {
		t.Fatalf("got %+v, want cluster.cpus on line 4 (the cluster key)", fe)
	}
}

func TestRuleAPIVersionMissing(t *testing.T) {
	ve := validate(t, "project: demo\ncomponents: {storage: {}}\n")
	if ve == nil || ve.Errors[0].Path != "apiVersion" || ve.Errors[0].Line != 0 {
		t.Fatalf("got:\n%v", ve)
	}
	if got := ve.Error(); got != `ois.yaml: apiVersion: required, must be "ois/v1alpha1"` {
		t.Fatalf("Error() = %q", got)
	}
}

func TestRuleSamplesNoneAvailable(t *testing.T) {
	if got := unknownSampleMsg("x", nil); !strings.Contains(got, "no samples are available in this build") {
		t.Fatalf("got %q", got)
	}
}

func TestValidateMultipleErrorsSorted(t *testing.T) {
	yml := "apiVersion: v0\nproject: X\ncluster: {size: huge}\ncomponents: {kafka: {}}\nsamples: [nope]\n"
	ve := validate(t, yml)
	if ve == nil {
		t.Fatal("no errors")
	}
	var paths []string
	for i, fe := range ve.Errors {
		paths = append(paths, fe.Path)
		if i > 0 && fe.Line < ve.Errors[i-1].Line {
			t.Fatalf("errors not sorted by line:\n%v", ve)
		}
	}
	want := []string{"apiVersion", "project", "cluster.size", "components", "samples[0]"}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	text := ve.Error()
	if lines := strings.Split(text, "\n"); len(lines) != len(want) || !strings.HasPrefix(lines[0], "ois.yaml:1: apiVersion: ") {
		t.Fatalf("Error() =\n%s", text)
	}
}

func TestValidateRunsRegisteredRule(t *testing.T) {
	ve := validate(t, "apiVersion: ois/v1alpha1\nproject: "+probeProject+"\ncomponents: {storage: {}}\n")
	if ve == nil || len(ve.Errors) != 1 || ve.Errors[0].Msg != "probe rule ran" || ve.Errors[0].Line != 2 {
		t.Fatalf("got:\n%v", ve)
	}
}

func TestRuleRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate RegisterRule did not panic")
		}
	}()
	RegisterRule(Rule{Name: "api-version", Check: checkAPIVersion})
}

func TestValidateNearestLine(t *testing.T) {
	s, err := Parse([]byte(header+"components: {storage: {}}\nstreams:\n  - name: a\n  - name: b\n"), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{
		"streams[1].source.topic": 6,
		"streams[1].name":         6,
		"streams[7]":              4,
		"nothing.here":            0,
	} {
		if got := s.nearestLine(path); got != want {
			t.Errorf("nearestLine(%q) = %d, want %d", path, got, want)
		}
	}
}

func TestValidateErrorUsesBaseName(t *testing.T) {
	ve := &ValidationError{File: "/some/dir/ois.yaml", Errors: []FieldError{{Path: "project", Msg: "required", Line: 2}}}
	if got := ve.Error(); got != "ois.yaml:2: project: required" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestValidateLoadAndValidate(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(good, []byte(header+"components: {storage: {}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte(header+"components: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadAndValidate(good); err != nil || s == nil {
		t.Fatalf("good: %v", err)
	}
	var ve *ValidationError
	if _, err := LoadAndValidate(bad); !errors.As(err, &ve) {
		t.Fatalf("bad: err = %v, want *ValidationError", err)
	}
}

func TestValidateKnownSamplesSorted(t *testing.T) {
	if got := KnownSamples(); !slices.IsSorted(got) || !slices.Contains(got, testSample) {
		t.Fatalf("KnownSamples() = %v", got)
	}
}
