package cluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/christiannarbon/ois/internal/execx"
	"github.com/christiannarbon/ois/internal/spec"
)

// Target identifies the one cluster a command may touch.
type Target struct {
	Project string
	Profile string // "dev", "prod", ...
	Kind    string // spec.TargetMinikube | spec.TargetKubernetes
	Context string // the only context any kubectl/helm call may use
}

// IsMinikube reports whether ois manages this cluster's lifecycle (up/down).
func (t Target) IsMinikube() bool { return t.Kind == spec.TargetMinikube }

// ErrNoContext is wrapped when the project's context is missing.
var ErrNoContext = errors.New("cluster context not found")

// RequireContext checks that kubeconfig lists the profile's context and returns the
// Target. The context always comes from the profile; the current context is never read.
// Empty output (dry run) means "cannot tell" and returns the Target without error.
func RequireContext(ctx context.Context, r execx.Runner, project string, p spec.ResolvedProfile) (Target, error) {
	t := Target{Project: project, Profile: p.Name, Kind: p.Target, Context: p.Context}
	res, err := r.Run(ctx, execx.Cmd{Name: "kubectl", Args: []string{"config", "get-contexts", "-o", "name"}})
	if err != nil {
		return Target{}, err
	}
	out := bytes.TrimSpace(res.Stdout)
	if len(out) == 0 {
		return t, nil
	}
	for _, name := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(name) == t.Context {
			return t, nil
		}
	}
	if t.IsMinikube() {
		return Target{}, fmt.Errorf("kube context %q not found — run \"ois up\" first: %w", t.Context, ErrNoContext)
	}
	return Target{}, fmt.Errorf("kube context %q (profile %s) not found in your kubeconfig — ois never falls back to the current context: %w",
		t.Context, t.Profile, ErrNoContext)
}
