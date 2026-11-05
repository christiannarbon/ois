package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/christiannarbon/ois/internal/version"
)

func newVersionCommand(g *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ois version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(g.Stdout, "ois %s (commit %s, built %s)\n",
				version.Version, version.Commit, version.Date)
			return err
		},
	}
}
