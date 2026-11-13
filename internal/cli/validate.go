package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/christiannarbon/ois/internal/cluster"
	"github.com/christiannarbon/ois/internal/project"
	"github.com/christiannarbon/ois/internal/spec"
)

func newValidateCommand(g *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check ois.yaml and print what it resolves to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := g.LoadSpec()
			if err != nil {
				writeError(g.Stderr, err)
				return errSilent
			}
			r, err := s.Resolve(g.Profile)
			if err != nil {
				writeError(g.Stderr, err)
				return errSilent
			}
			return writePlan(g.Stdout, r, s.ProfileNames())
		},
	}
}

// installOrder lists the enabled components in the order they are installed.
func installOrder(s *spec.Spec) []string {
	var out []string
	for _, name := range spec.ComponentNames {
		if s.Enabled(name) {
			out = append(out, name)
		}
	}
	return out
}

func writePlan(w io.Writer, r *spec.Resolved, profiles []string) error {
	samples := "none"
	if len(r.Samples) > 0 {
		samples = strings.Join(r.Samples, ", ")
	}
	components := installOrder(r.Spec)
	for i, name := range components {
		if r.Profile.IsExternal(name) {
			components[i] = name + " (external)"
		}
	}
	p := r.Profile
	projectLine := fmt.Sprintf("%s  (cluster profile %s)", r.Project, project.ProfileName(r.Project))
	clusterLine := fmt.Sprintf("%s (%s)", r.Cluster.Size, cluster.SizeFor(r.Cluster))
	if p.Target == spec.TargetKubernetes {
		projectLine = r.Project
		clusterLine = fmt.Sprintf("existing (context %s)", p.Context)
	}
	_, err := fmt.Fprintf(w, `spec:        %s
project:     %s
cluster:     %s
profile:     %s  (target %s, context %s, tier %s)
profiles:    %s
components:  %s
streams:     %d
samples:     %s
✓ %s is valid
`, r.File, projectLine, clusterLine,
		p.Name, p.Target, p.Context, p.Tier, strings.Join(profiles, ", "),
		strings.Join(components, ", "), len(r.Streams), samples, filepath.Base(r.File))
	return err
}
