// Package spec defines the ois.yaml project spec and loads it strictly.
package spec

import (
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const APIVersion = "ois/v1alpha1"

type Spec struct {
	APIVersion  string             `yaml:"apiVersion"`
	Project     string             `yaml:"project"`
	Cluster     Cluster            `yaml:"cluster"`
	Components  Components         `yaml:"components"`
	Streams     []Stream           `yaml:"streams,omitempty"`
	Transform   *Transform         `yaml:"transform,omitempty"`
	Maintenance []Maintenance      `yaml:"maintenance,omitempty"`
	Dashboards  *Dashboards        `yaml:"dashboards,omitempty"`
	Samples     []string           `yaml:"samples,omitempty"`
	Profiles    map[string]Profile `yaml:"profiles,omitempty"`

	// Dir is the absolute directory containing ois.yaml. Relative paths resolve against it.
	Dir string `yaml:"-"`
	// File is the absolute path of ois.yaml.
	File string `yaml:"-"`
	// pos maps a field path ("components.storage", "streams[0].name") to its line.
	pos map[string]int
	// componentsNode is the top-level components node (a mapping or an alias to one).
	componentsNode *yaml.Node
}

type Cluster struct {
	Size   string `yaml:"size,omitempty"`   // minimal | lean | custom
	CPUs   int    `yaml:"cpus,omitempty"`   // custom only
	Memory int    `yaml:"memory,omitempty"` // MiB, custom only
	Disk   string `yaml:"disk,omitempty"`   // e.g. "40g", custom only
}

// Components: a non-nil pointer means the component is enabled.
type Components struct {
	Storage  *Storage  `yaml:"storage,omitempty"`
	Kafka    *Kafka    `yaml:"kafka,omitempty"`
	Catalog  *Catalog  `yaml:"catalog,omitempty"`
	Flink    *Flink    `yaml:"flink,omitempty"`
	Trino    *Trino    `yaml:"trino,omitempty"`
	Dagster  *Dagster  `yaml:"dagster,omitempty"`
	Superset *Superset `yaml:"superset,omitempty"`
}

type Storage struct{}
type Kafka struct{}
type Catalog struct{}
type Flink struct{}
type Trino struct{}
type Dagster struct{}
type Superset struct{}

type Stream struct {
	Name string `yaml:"name"`
}
type Transform struct{}
type Maintenance struct {
	Table string `yaml:"table"`
}
type Dashboards struct{}

// Line returns the 1-based line of a field path, or 0 if unknown.
func (s *Spec) Line(path string) int { return s.pos[path] }

// Path resolves a path from the spec (relative to Dir) to an absolute path.
func (s *Spec) Path(rel string) string {
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel)
	}
	return filepath.Join(s.Dir, rel)
}

// ComponentNames is the canonical component order (also a valid install order).
var ComponentNames = []string{"storage", "kafka", "catalog", "flink", "trino", "dagster", "superset"}

// Enabled reports whether components.<name> is present.
func (s *Spec) Enabled(name string) bool {
	c := s.Components
	switch name {
	case "storage":
		return c.Storage != nil
	case "kafka":
		return c.Kafka != nil
	case "catalog":
		return c.Catalog != nil
	case "flink":
		return c.Flink != nil
	case "trino":
		return c.Trino != nil
	case "dagster":
		return c.Dagster != nil
	case "superset":
		return c.Superset != nil
	}
	panic(fmt.Sprintf("spec: unknown component %q", name))
}
