package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/christiannarbon/ois/internal/cluster"
	"github.com/christiannarbon/ois/internal/spec"
)

const readyTimeout = 300 * time.Second

func newUpCommand(g *GlobalOptions) *cobra.Command {
	var recreate bool
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Create or start the project's local minikube cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUp(cmd.Context(), g, recreate)
		},
	}
	cmd.Flags().BoolVar(&recreate, "recreate", false,
		"delete and recreate the cluster when it no longer matches ois.yaml (asks first; deletes all data in it)")
	return cmd
}

// minikubeManager resolves the spec and returns a Manager for it, refusing profiles that
// target an existing cluster. Used by up and down, which run before the context exists.
func minikubeManager(g *GlobalOptions, verb string) (*spec.Resolved, *cluster.Manager, error) {
	r, err := g.Resolve()
	if err != nil {
		return nil, nil, err
	}
	if r.Profile.Target != spec.TargetMinikube {
		return nil, nil, fmt.Errorf("ois %s manages local minikube clusters only; profile %q targets an existing cluster (context %q)",
			verb, r.Profile.Name, r.Profile.Context)
	}
	m, err := cluster.New(g.Runner(), r.Project)
	if err != nil {
		return nil, nil, err
	}
	if g.Verbose {
		m.Progress = g.Stderr
	}
	return r, m, nil
}

func runUp(ctx context.Context, g *GlobalOptions, recreate bool) error {
	r, m, err := minikubeManager(g, "up")
	if err != nil {
		return err
	}
	want := cluster.SizeFor(r.Cluster)
	st, err := m.Inspect(ctx)
	if err != nil {
		return err
	}
	if st.State == cluster.StateInvalid {
		return m.InvalidError()
	}
	if st.State != cluster.StateMissing {
		if problems := drift(m, st, want); len(problems) > 0 {
			if !recreate {
				return driftError(m.Profile, problems)
			}
			prompt := fmt.Sprintf("Delete cluster %s and ALL data in it, then recreate it?", m.Profile)
			if err := confirm(g, g.input(), prompt, "recreate"); err != nil {
				return err
			}
			fmt.Fprintf(g.Stdout, "deleting cluster %s to recreate it\n", m.Profile)
			if err := m.Purge(ctx); err != nil {
				return err
			}
		}
	}
	fmt.Fprintf(g.Stdout, "starting cluster %s (%s %s), this can take a few minutes\n", m.Profile, r.Cluster.Size, want)
	if err := m.Start(ctx, want); err != nil {
		return err
	}
	if err := m.EnableAddons(ctx); err != nil {
		return err
	}
	t, err := cluster.RequireContext(ctx, g.Runner(), r.Project, r.Profile)
	if err != nil {
		return err
	}
	if err := cluster.WaitReady(ctx, g.Runner(), t, readyTimeout); err != nil {
		return err
	}
	if err := cluster.RequireOneDefaultStorageClass(ctx, g.Runner(), t); err != nil {
		return err
	}
	fmt.Fprintf(g.Stdout, "✓ cluster %s ready (%s) — kube context %q\n", m.Profile, cpuMem(want), t.Context)
	fmt.Fprintf(g.Stdout, "  your current kube context was not changed; ois always uses %q\n", t.Context)
	return nil
}

// drift lists what minikube cannot change on the existing profile. Values minikube did
// not report are not compared.
func drift(m *cluster.Manager, st cluster.Status, want cluster.Size) []string {
	verb := "is running with"
	if st.State == cluster.StateStopped {
		verb = "exists with"
	}
	var out []string
	if st.Size.CPUs != want.CPUs || st.Size.MemoryMiB != want.MemoryMiB {
		out = append(out, fmt.Sprintf("cluster %q %s %d CPU / %d MiB but ois.yaml asks for %d CPU / %d MiB.",
			m.Profile, verb, st.Size.CPUs, st.Size.MemoryMiB, want.CPUs, want.MemoryMiB))
	}
	if st.K8sVersion != "" && st.K8sVersion != m.K8sVersion {
		out = append(out, fmt.Sprintf("cluster %q has Kubernetes %s but ois pins %s.", m.Profile, st.K8sVersion, m.K8sVersion))
	}
	if st.Driver != "" && st.Driver != m.Driver {
		out = append(out, fmt.Sprintf("cluster %q uses the %s driver but ois is set to use %s (%s).",
			m.Profile, st.Driver, m.Driver, cluster.DriverEnv))
	}
	return out
}

func driftError(profile string, problems []string) error {
	why := "minikube cannot change these on an existing cluster."
	if len(problems) == 1 && strings.Contains(problems[0], " CPU / ") {
		why = "minikube cannot resize a cluster."
	}
	return fmt.Errorf("%s\n%s Recreate it (this deletes all data in it):\n  ois up --recreate",
		strings.Join(problems, "\n"), why)
}

func cpuMem(s cluster.Size) string { return fmt.Sprintf("%d CPU, %d MiB", s.CPUs, s.MemoryMiB) }
