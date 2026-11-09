package spec

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	projectNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,28}[a-z0-9]$`)
	streamNameRe  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	diskRe        = regexp.MustCompile(`^[0-9]+g$`)
)

var clusterSizes = []string{"minimal", "lean", "custom"}

const (
	minCustomCPUs   = 2
	minCustomMemory = 4096
)

func init() {
	for _, r := range []Rule{
		{"api-version", checkAPIVersion},
		{"project-name", checkProjectName},
		{"cluster-size", checkClusterSize},
		{"components-present", checkComponentsPresent},
		{"streams-require", checkStreamsRequire},
		{"stream-names", checkStreamNames},
		{"transform-requires", checkTransformRequires},
		{"maintenance-requires", checkMaintenanceRequires},
		{"dashboards-require", checkDashboardsRequire},
		{"samples-known", checkSamplesKnown},
		{"storage-required", checkStorageRequired},
	} {
		RegisterRule(r)
	}
}

func checkAPIVersion(s *Spec) []FieldError {
	switch s.APIVersion {
	case APIVersion:
		return nil
	case "":
		return []FieldError{{Path: "apiVersion", Msg: fmt.Sprintf("required, must be %q", APIVersion)}}
	}
	return []FieldError{{Path: "apiVersion", Msg: fmt.Sprintf("must be %q, got %q", APIVersion, s.APIVersion)}}
}

func checkProjectName(s *Spec) []FieldError {
	switch {
	case s.Project == "":
		return []FieldError{{Path: "project", Msg: "required"}}
	case !projectNameRe.MatchString(s.Project):
		return []FieldError{{Path: "project", Msg: "must be 2-30 characters matching " + projectNameRe.String()}}
	}
	return nil
}

func checkClusterSize(s *Spec) []FieldError {
	c := s.Cluster
	if !slices.Contains(clusterSizes, c.Size) {
		return []FieldError{{Path: "cluster.size", Msg: fmt.Sprintf("must be one of %s, got %q", strings.Join(clusterSizes, ", "), c.Size)}}
	}
	var errs []FieldError
	if c.Size != "custom" {
		for path, set := range map[string]bool{"cluster.cpus": c.CPUs != 0, "cluster.memory": c.Memory != 0, "cluster.disk": c.Disk != ""} {
			if set {
				errs = append(errs, FieldError{Path: path, Msg: fmt.Sprintf("only allowed with size: custom (size is %s)", c.Size)})
			}
		}
		return errs
	}
	if c.CPUs < minCustomCPUs {
		errs = append(errs, FieldError{Path: "cluster.cpus", Msg: fmt.Sprintf("size: custom needs cpus of at least %d", minCustomCPUs)})
	}
	if c.Memory < minCustomMemory {
		errs = append(errs, FieldError{Path: "cluster.memory", Msg: fmt.Sprintf("size: custom needs memory of at least %d (MiB)", minCustomMemory)})
	}
	if !diskRe.MatchString(c.Disk) {
		errs = append(errs, FieldError{Path: "cluster.disk", Msg: `size: custom needs disk like "40g"`})
	}
	return errs
}

func checkComponentsPresent(s *Spec) []FieldError {
	if len(s.enabledComponents()) == 0 {
		return []FieldError{{Path: "components", Msg: "enable at least one component, e.g. storage: {}"}}
	}
	return nil
}

func checkStreamsRequire(s *Spec) []FieldError {
	if len(s.Streams) == 0 {
		return nil
	}
	return s.require("streams", "flink", "kafka", "catalog")
}

func checkStreamNames(s *Spec) []FieldError {
	var errs []FieldError
	seen := map[string]int{}
	for i, st := range s.Streams {
		path := fmt.Sprintf("streams[%d].name", i)
		switch first, dup := seen[st.Name]; {
		case st.Name == "":
			errs = append(errs, FieldError{Path: path, Msg: "required"})
		case !streamNameRe.MatchString(st.Name):
			errs = append(errs, FieldError{Path: path, Msg: "must match " + streamNameRe.String()})
		case dup:
			errs = append(errs, FieldError{Path: path, Msg: fmt.Sprintf("duplicate stream name %q (also streams[%d])", st.Name, first)})
		default:
			seen[st.Name] = i
		}
	}
	return errs
}

func checkTransformRequires(s *Spec) []FieldError {
	if s.Transform == nil {
		return nil
	}
	return s.require("transform", "dagster", "trino")
}

func checkMaintenanceRequires(s *Spec) []FieldError {
	if len(s.Maintenance) == 0 {
		return nil
	}
	errs := s.require("maintenance", "dagster", "trino")
	for i, m := range s.Maintenance {
		if m.Table == "" {
			errs = append(errs, FieldError{Path: fmt.Sprintf("maintenance[%d].table", i), Msg: "required"})
		}
	}
	return errs
}

func checkDashboardsRequire(s *Spec) []FieldError {
	if s.Dashboards == nil {
		return nil
	}
	return s.require("dashboards", "superset")
}

func checkSamplesKnown(s *Spec) []FieldError {
	known := KnownSamples()
	var errs []FieldError
	seen := map[string]int{}
	for i, name := range s.Samples {
		path := fmt.Sprintf("samples[%d]", i)
		first, dup := seen[name]
		switch {
		case !slices.Contains(known, name):
			errs = append(errs, FieldError{Path: path, Msg: unknownSampleMsg(name, known)})
		case dup:
			errs = append(errs, FieldError{Path: path, Msg: fmt.Sprintf("duplicate sample %q (also samples[%d])", name, first)})
		default:
			seen[name] = i
		}
	}
	return errs
}

func unknownSampleMsg(name string, known []string) string {
	if len(known) == 0 {
		return fmt.Sprintf("unknown sample %q: no samples are available in this build", name)
	}
	return fmt.Sprintf("unknown sample %q (known: %s)", name, strings.Join(known, ", "))
}

func checkStorageRequired(s *Spec) []FieldError {
	enabled := s.enabledComponents()
	if len(enabled) == 0 || s.Enabled("storage") {
		return nil
	}
	return []FieldError{{Path: "components", Msg: fmt.Sprintf("storage must be enabled; %s store data in it", strings.Join(enabled, ", "))}}
}

// require returns one error at path for each named component that is not enabled.
func (s *Spec) require(path string, names ...string) []FieldError {
	var errs []FieldError
	for _, name := range names {
		if !s.Enabled(name) {
			errs = append(errs, FieldError{Path: path, Msg: fmt.Sprintf("needs components.%s to be enabled", name)})
		}
	}
	return errs
}

func (s *Spec) enabledComponents() []string {
	var out []string
	for _, name := range ComponentNames {
		if s.Enabled(name) {
			out = append(out, name)
		}
	}
	return out
}
