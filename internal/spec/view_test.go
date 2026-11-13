package spec

import (
	"reflect"
	"strings"
	"testing"
)

const everyBlock = header + `components: {storage: {}, kafka: {}, catalog: {}, flink: {}, trino: {}, dagster: {}, superset: {}}
streams:
  - name: clicks
transform: {}
maintenance:
  - table: lake.clicks
dashboards: {}
samples: [test-sample]
profiles:
  prod: {target: kubernetes, context: c}
`

func TestResolvedSharesNothing(t *testing.T) {
	s, err := Parse([]byte(everyBlock), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r := mustResolve(t, s, "dev")
	a, b := reflect.ValueOf(s).Elem(), reflect.ValueOf(r.Spec).Elem()
	for i := range a.NumField() {
		f := a.Type().Field(i)
		if !f.IsExported() {
			continue
		}
		if path := shared(a.Field(i), b.Field(i), f.Name); path != "" {
			t.Errorf("resolved view shares %s with the loaded spec", path)
		}
	}
}

// shared returns the path of the first slice, map or pointer that a and b both point at.
// Zero-size values are skipped: Go may give them all the same address.
func shared(a, b reflect.Value, path string) string {
	switch a.Kind() {
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return ""
		}
		if a.Pointer() == b.Pointer() && a.Elem().Type().Size() > 0 {
			return path
		}
		return shared(a.Elem(), b.Elem(), path)
	case reflect.Slice:
		if a.Len() > 0 && b.Len() > 0 && a.Pointer() == b.Pointer() {
			return path
		}
		for i := range min(a.Len(), b.Len()) {
			if p := shared(a.Index(i), b.Index(i), path+"[]"); p != "" {
				return p
			}
		}
	case reflect.Map:
		if !a.IsNil() && !b.IsNil() && a.Pointer() == b.Pointer() {
			return path
		}
	case reflect.Struct:
		for i := range a.NumField() {
			if !a.Type().Field(i).IsExported() {
				continue
			}
			if p := shared(a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name); p != "" {
				return p
			}
		}
	}
	return ""
}

func TestResolvedMutationLeavesSpecAlone(t *testing.T) {
	s, err := Parse([]byte(everyBlock), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r := mustResolve(t, s, "dev")
	r.Streams[0].Name = "changed"
	r.Maintenance[0].Table = "changed"
	r.Samples[0] = "changed"
	if s.Streams[0].Name != "clicks" || s.Maintenance[0].Table != "lake.clicks" || s.Samples[0] != "test-sample" {
		t.Fatalf("mutating the view changed the spec: %+v %+v %v", s.Streams, s.Maintenance, s.Samples)
	}
}

func TestResolvedViewHasNoProfiles(t *testing.T) {
	s, err := Parse([]byte(everyBlock), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r := mustResolve(t, s, "prod")
	if r.Profiles != nil {
		t.Fatalf("view Profiles = %v, want nil", r.Profiles)
	}
	if _, err := r.Resolve("dev"); err == nil || !strings.Contains(err.Error(), `already resolved for profile "prod"`) {
		t.Fatalf("Resolve on a view: err = %v", err)
	}
}
