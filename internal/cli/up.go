package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/christiannarbon/ois/internal/cluster"
	"github.com/christiannarbon/ois/internal/execx"
	"github.com/christiannarbon/ois/internal/spec"
)

const defaultClassAnnotation = "storageclass.kubernetes.io/is-default-class"

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
	cmd.Flags().BoolVar(&recreate, "recreate", false, "delete and recreate the cluster when its size differs from ois.yaml (deletes all data in it)")
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
	return r, m, nil
}

func runUp(ctx context.Context, g *GlobalOptions, recreate bool) error {
	r, m, err := minikubeManager(g, "up")
	if err != nil {
		return err
	}
	want := cluster.SizeFor(r.Cluster)
	state, have, err := m.Inspect(ctx)
	if err != nil {
		return err
	}
	if state != cluster.StateMissing && (have.CPUs != want.CPUs || have.MemoryMiB != want.MemoryMiB) {
		if !recreate {
			return sizeMismatch(m.Profile, state, have, want)
		}
		fmt.Fprintf(g.Stdout, "deleting cluster %s (%s) to recreate it at %s\n", m.Profile, cpuMem(have), cpuMem(want))
		if err := m.Delete(ctx); err != nil {
			return err
		}
	}
	fmt.Fprintf(g.Stdout, "starting cluster %s (%s %s), this can take a few minutes\n", m.Profile, r.Cluster.Size, want)
	if err := m.Start(ctx, want); err != nil {
		return err
	}
	if err := m.EnableAddons(ctx); err != nil {
		return err
	}
	kctx := r.Profile.Context
	if _, err := g.Runner().Run(ctx, kubectl(kctx, "wait", "--for=condition=Ready", "nodes", "--all", "--timeout=300s")); err != nil {
		return err
	}
	if err := requireOneDefaultStorageClass(ctx, g.Runner(), kctx); err != nil {
		return err
	}
	fmt.Fprintf(g.Stdout, "✓ cluster %s ready (%s) — kube context %q\n", m.Profile, cpuMem(want), kctx)
	fmt.Fprintf(g.Stdout, "  your current kube context was not changed; ois always uses %q\n", kctx)
	return nil
}

func sizeMismatch(profile string, state cluster.State, have, want cluster.Size) error {
	verb := "is running"
	if state == cluster.StateStopped {
		verb = "exists"
	}
	return fmt.Errorf("cluster %q %s with %d CPU / %d MiB but ois.yaml asks for %d CPU / %d MiB.\n"+
		"minikube cannot resize a cluster. Recreate it (this deletes all data in it):\n"+
		"  ois up --recreate", profile, verb, have.CPUs, have.MemoryMiB, want.CPUs, want.MemoryMiB)
}

func cpuMem(s cluster.Size) string { return fmt.Sprintf("%d CPU, %d MiB", s.CPUs, s.MemoryMiB) }

func kubectl(kctx string, args ...string) execx.Cmd {
	return execx.Cmd{Name: "kubectl", Args: append([]string{"--context", kctx}, args...)}
}

// requireOneDefaultStorageClass fails unless exactly one StorageClass is the default.
// Empty output (dry run) cannot be checked and passes.
func requireOneDefaultStorageClass(ctx context.Context, r execx.Runner, kctx string) error {
	res, err := r.Run(ctx, kubectl(kctx, "get", "storageclass", "-o", "json"))
	if err != nil {
		return err
	}
	out := bytes.TrimSpace(res.Stdout)
	if len(out) == 0 {
		return nil
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return fmt.Errorf("reading storage classes: %w", err)
	}
	var defaults []string
	for _, sc := range list.Items {
		if sc.Metadata.Annotations[defaultClassAnnotation] == "true" {
			defaults = append(defaults, sc.Metadata.Name)
		}
	}
	if len(defaults) != 1 {
		return fmt.Errorf("cluster %q must have exactly one default StorageClass, found %d: [%s]",
			kctx, len(defaults), strings.Join(defaults, ", "))
	}
	return nil
}
