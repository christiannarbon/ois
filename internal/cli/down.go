package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/christiannarbon/ois/internal/cluster"
)

func newDownCommand(g *GlobalOptions) *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the project's local minikube cluster (--purge deletes it)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDown(cmd.Context(), g, purge)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "delete the cluster and all data in it (asks first; --yes skips)")
	return cmd
}

func runDown(ctx context.Context, g *GlobalOptions, purge bool) error {
	_, m, err := minikubeManager(g, "down")
	if err != nil {
		return err
	}
	state, _, err := m.Inspect(ctx)
	if err != nil {
		return err
	}
	if state == cluster.StateMissing {
		fmt.Fprintf(g.Stdout, "cluster %s does not exist\n", m.Profile)
		return nil
	}
	if !purge {
		if err := m.Stop(ctx); err != nil {
			return err
		}
		fmt.Fprintf(g.Stdout, "✓ cluster %s stopped (data kept)\n", m.Profile)
		return nil
	}
	if err := confirm(g, g.input(), fmt.Sprintf("Delete cluster %s and ALL data in it?", m.Profile), "delete"); err != nil {
		return err
	}
	if err := m.Delete(ctx); err != nil {
		return err
	}
	fmt.Fprintf(g.Stdout, "✓ cluster %s deleted\n", m.Profile)
	return nil
}
