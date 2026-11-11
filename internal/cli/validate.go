package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

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
			return writePlan(g.Stdout, s)
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

func writePlan(w io.Writer, s *spec.Spec) error {
	samples := "none"
	if len(s.Samples) > 0 {
		samples = strings.Join(s.Samples, ", ")
	}
	_, err := fmt.Fprintf(w, `spec:        %s
project:     %s  (cluster profile %s)
cluster:     %s
components:  %s
streams:     %d
samples:     %s
✓ %s is valid
`, s.File, s.Project, project.ProfileName(s.Project), s.Cluster.Size,
		strings.Join(installOrder(s), ", "), len(s.Streams), samples, filepath.Base(s.File))
	return err
}
