package helm

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/christiannarbon/ois/internal/cluster"
	"github.com/christiannarbon/ois/internal/execx"
	"github.com/christiannarbon/ois/internal/spec"
)

var demo = cluster.Target{Project: "demo", Profile: "dev", Kind: spec.TargetMinikube, Context: "ois-demo"}

func TestUpgradeInstall(t *testing.T) {
	cases := []struct {
		name string
		rel  Release
		want string
	}{
		{"repo chart", Release{
			Name: "rustfs", Namespace: "storage", Chart: "rustfs", Repo: "https://charts.rustfs.com",
			Version: "0.0.80", ValuesFiles: []string{"/tmp/r/values.yaml", "/tmp/r/values-dev.yaml"},
			Wait: true, Timeout: 5 * time.Minute,
		}, "helm upgrade --install rustfs rustfs --repo https://charts.rustfs.com --version 0.0.80" +
			" --namespace storage --create-namespace --kube-context ois-demo" +
			" -f /tmp/r/values.yaml -f /tmp/r/values-dev.yaml --wait --timeout 5m0s"},
		{"oci chart, default timeout", Release{
			Name: "polaris", Namespace: "catalog", Chart: "oci://ghcr.io/example/charts/polaris",
			Version: "1.2.0", Wait: true,
		}, "helm upgrade --install polaris oci://ghcr.io/example/charts/polaris --version 1.2.0" +
			" --namespace catalog --create-namespace --kube-context ois-demo --wait --timeout 10m0s"},
		{"no wait", Release{
			Name: "x", Namespace: "ns", Chart: "x", Repo: "https://example.org/charts", Version: "1.0.0",
			Timeout: time.Minute,
		}, "helm upgrade --install x x --repo https://example.org/charts --version 1.0.0" +
			" --namespace ns --create-namespace --kube-context ois-demo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &execx.Fake{}
			if err := New(f, demo).UpgradeInstall(context.Background(), c.rel); err != nil {
				t.Fatal(err)
			}
			if got := f.CommandLines(); !slices.Equal(got, []string{c.want}) {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestUpgradeInstallRefusesBeforeRunning(t *testing.T) {
	base := Release{Name: "x", Namespace: "ns", Chart: "x", Repo: "https://example.org/charts", Version: "1.0.0"}
	cases := []struct {
		name    string
		edit    func(r *Release)
		wantErr string
	}{
		{"empty version", func(r *Release) { r.Version = "" }, "needs an exact version"},
		{"latest", func(r *Release) { r.Version = "latest" }, `got "latest"`},
		{"no repo, not oci", func(r *Release) { r.Repo = "" }, "needs a repo URL or an oci:// reference"},
		{"oci with repo", func(r *Release) { r.Chart = "oci://ghcr.io/x/x" }, "takes no repo URL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := base
			c.edit(&r)
			f := &execx.Fake{}
			err := New(f, demo).UpgradeInstall(context.Background(), r)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err %v, want %q", err, c.wantErr)
			}
			if len(f.Calls) != 0 {
				t.Fatalf("ran %q", f.CommandLines())
			}
		})
	}
}

func TestStatus(t *testing.T) {
	const cmd = "helm status rustfs -n storage --kube-context ois-demo -o json"
	boom := errors.New("Kubernetes cluster unreachable")
	cases := []struct {
		name       string
		res        execx.Result
		err        error
		wantStatus string
		wantFound  bool
		wantErr    string
	}{
		{"deployed", execx.Result{Stdout: []byte(`{"name":"rustfs","info":{"status":"deployed"}}`)}, nil, "deployed", true, ""},
		{"failed", execx.Result{Stdout: []byte(`{"info":{"status":"failed"}}`)}, nil, "failed", true, ""},
		{"not found", execx.Result{}, &execx.ExitError{Cmd: "helm", ExitCode: 1, Stderr: "Error: release: not found"}, "", false, ""},
		{"not found in result stderr", execx.Result{Stderr: []byte("Error: release: not found")}, errors.New("exit 1"), "", false, ""},
		{"dry run", execx.Result{}, nil, "", false, ""},
		{"error", execx.Result{}, boom, "", false, "unreachable"},
		{"bad json", execx.Result{Stdout: []byte("{")}, nil, "", false, "reading helm status of rustfs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := (&execx.Fake{}).On(cmd, c.res, c.err)
			status, found, err := New(f, demo).Status(context.Background(), "rustfs", "storage")
			if status != c.wantStatus || found != c.wantFound {
				t.Fatalf("got (%q, %v)", status, found)
			}
			if c.wantErr == "" && err != nil || c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err %v, want %q", err, c.wantErr)
			}
			if got := f.CommandLines(); !slices.Equal(got, []string{cmd}) {
				t.Fatalf("got %q", got)
			}
		})
	}
}

func TestEveryCallCarriesContext(t *testing.T) {
	f := &execx.Fake{}
	h := New(f, demo)
	_ = h.UpgradeInstall(context.Background(), Release{Name: "x", Namespace: "n", Chart: "oci://r/x", Version: "1"})
	_, _, _ = h.Status(context.Background(), "x", "n")
	lines := f.CommandLines()
	if len(lines) != 2 {
		t.Fatalf("calls %q", lines)
	}
	for _, l := range lines {
		if !strings.Contains(l, " --kube-context ois-demo") {
			t.Errorf("no context: %s", l)
		}
	}
}
