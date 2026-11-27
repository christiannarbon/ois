// Package helm runs helm against exactly one kube context. Charts are always installed by
// repo URL (or oci:// reference) and an exact version; ois never runs `helm repo add`.
package helm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/christiannarbon/ois/internal/cluster"
	"github.com/christiannarbon/ois/internal/execx"
)

type Client struct {
	Run     execx.Runner
	Context string
}

func New(r execx.Runner, t cluster.Target) *Client {
	return &Client{Run: r, Context: t.Context}
}

// Release describes one chart installation.
type Release struct {
	Name        string // release name, e.g. "rustfs"
	Namespace   string
	Chart       string   // chart name with Repo, or a full "oci://..." reference
	Repo        string   // https chart repo URL; empty for oci:// charts
	Version     string   // exact chart version; required
	ValuesFiles []string // absolute paths, applied in order
	Wait        bool
	Timeout     time.Duration // default 10m when zero
}

const defaultTimeout = 10 * time.Minute

func (r Release) validate() error {
	oci := strings.HasPrefix(r.Chart, "oci://")
	switch {
	case r.Version == "" || r.Version == "latest":
		return fmt.Errorf("helm release %s: chart %s needs an exact version, got %q", r.Name, r.Chart, r.Version)
	case r.Repo == "" && !oci:
		return fmt.Errorf("helm release %s: chart %s needs a repo URL or an oci:// reference", r.Name, r.Chart)
	case r.Repo != "" && oci:
		return fmt.Errorf("helm release %s: an oci:// chart takes no repo URL", r.Name)
	}
	return nil
}

// UpgradeInstall runs:
//
//	helm upgrade --install <name> <chart> [--repo <repo>] --version <v>
//	  --namespace <ns> --create-namespace --kube-context <ctx>
//	  [-f <values>]... [--wait --timeout <t>]
func (c *Client) UpgradeInstall(ctx context.Context, r Release) error {
	if err := r.validate(); err != nil {
		return err
	}
	args := []string{"upgrade", "--install", r.Name, r.Chart}
	if r.Repo != "" {
		args = append(args, "--repo", r.Repo)
	}
	args = append(args, "--version", r.Version, "--namespace", r.Namespace, "--create-namespace",
		"--kube-context", c.Context)
	for _, f := range r.ValuesFiles {
		args = append(args, "-f", f)
	}
	if r.Wait {
		t := r.Timeout
		if t == 0 {
			t = defaultTimeout
		}
		args = append(args, "--wait", "--timeout", t.String())
	}
	_, err := c.Run.Run(ctx, execx.Cmd{Name: "helm", Args: args})
	return err
}

// Status returns the release status ("deployed", "failed", ...) or found=false.
// Uses `helm status <name> -n <ns> --kube-context <ctx> -o json` and reads .info.status.
// Empty output (dry run) also returns found=false.
func (c *Client) Status(ctx context.Context, name, namespace string) (status string, found bool, err error) {
	res, err := c.Run.Run(ctx, execx.Cmd{Name: "helm",
		Args: []string{"status", name, "-n", namespace, "--kube-context", c.Context, "-o", "json"}})
	if releaseNotFound(res, err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	out := bytes.TrimSpace(res.Stdout)
	if len(out) == 0 {
		return "", false, nil
	}
	var rel struct {
		Info struct {
			Status string `json:"status"`
		} `json:"info"`
	}
	if err := json.Unmarshal(out, &rel); err != nil {
		return "", false, fmt.Errorf("reading helm status of %s: %w", name, err)
	}
	return rel.Info.Status, true, nil
}

func releaseNotFound(res execx.Result, err error) bool {
	const msg = "release: not found"
	if bytes.Contains(res.Stderr, []byte(msg)) {
		return true
	}
	var ee *execx.ExitError
	return errors.As(err, &ee) && strings.Contains(ee.Stderr, msg)
}
