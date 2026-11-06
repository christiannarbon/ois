package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/spf13/cobra"

	assets "github.com/christiannarbon/ois/components"
	"github.com/christiannarbon/ois/internal/version"
)

func newVersionCommand(g *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ois version and the pinned component versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pins, err := assets.Versions()
			if err != nil {
				return err
			}
			return writeVersion(g.Stdout, pins)
		},
	}
}

// writeVersion skips empty values: those components are not pinned yet.
func writeVersion(w io.Writer, pins map[string]string) error {
	if _, err := fmt.Fprintf(w, "ois %s (commit %s, built %s)\n\npinned versions:\n",
		version.Version, version.Commit, version.Date); err != nil {
		return err
	}
	keys := slices.Sorted(maps.Keys(pins))
	keys = slices.DeleteFunc(keys, func(k string) bool { return pins[k] == "" })
	width := 0
	for _, k := range keys {
		width = max(width, len(k))
	}
	for _, k := range keys {
		if _, err := fmt.Fprintf(w, "  %-*s  %s\n", width, k, pins[k]); err != nil {
			return err
		}
	}
	return nil
}
