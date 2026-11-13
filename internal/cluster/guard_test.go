package cluster

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/christiannarbon/ois/internal/execx"
	"github.com/christiannarbon/ois/internal/spec"
)

const contextsCmd = "kubectl config get-contexts -o name"

var (
	devProfile  = spec.ResolvedProfile{Name: "dev", Target: spec.TargetMinikube, Context: "ois-demo"}
	prodProfile = spec.ResolvedProfile{Name: "prod", Target: spec.TargetKubernetes, Context: "prod-admin"}
)

func contexts(names string) *execx.Fake {
	return (&execx.Fake{}).On(contextsCmd, execx.Result{Stdout: []byte(names)}, nil)
}

func TestRequireContext(t *testing.T) {
	cases := []struct {
		name string
		p    spec.ResolvedProfile
		list string
		want Target
	}{
		{"minikube present", devProfile, "minikube\nois-demo\n", Target{"demo", "dev", spec.TargetMinikube, "ois-demo"}},
		{"kubernetes present", prodProfile, "ois-demo\nprod-admin\n", Target{"demo", "prod", spec.TargetKubernetes, "prod-admin"}},
		{"empty output", devProfile, "", Target{"demo", "dev", spec.TargetMinikube, "ois-demo"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := contexts(c.list)
			got, err := RequireContext(context.Background(), f, "demo", c.p)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
			if lines := f.CommandLines(); !slices.Equal(lines, []string{contextsCmd}) {
				t.Fatalf("commands %q", lines)
			}
		})
	}
}

func TestRequireContextMissing(t *testing.T) {
	cases := []struct {
		name string
		p    spec.ResolvedProfile
		list string
		want string
	}{
		{"minikube", devProfile, "minikube\nois-other\n",
			`kube context "ois-demo" not found — run "ois up" first: cluster context not found`},
		// The current context (a real cluster) is listed but never used as a fallback.
		{"kubernetes with another current context", prodProfile, "work-cluster\nois-demo\n",
			`kube context "prod-admin" (profile prod) not found in your kubeconfig — ois never falls back to the current context: cluster context not found`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := contexts(c.list).On("kubectl config current-context", execx.Result{Stdout: []byte("work-cluster\n")}, nil)
			got, err := RequireContext(context.Background(), f, "demo", c.p)
			if !errors.Is(err, ErrNoContext) {
				t.Fatalf("err %v, want ErrNoContext", err)
			}
			if err.Error() != c.want {
				t.Fatalf("err\n%s\nwant\n%s", err, c.want)
			}
			if got != (Target{}) {
				t.Fatalf("got %+v, want zero Target", got)
			}
			if lines := f.CommandLines(); !slices.Equal(lines, []string{contextsCmd}) {
				t.Fatalf("commands %q", lines)
			}
		})
	}
}

func TestRequireContextKubectlError(t *testing.T) {
	boom := errors.New("boom")
	f := (&execx.Fake{}).On(contextsCmd, execx.Result{}, boom)
	if _, err := RequireContext(context.Background(), f, "demo", devProfile); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
}

func TestRequireContextDryRun(t *testing.T) {
	var out bytes.Buffer
	got, err := RequireContext(context.Background(), execx.NewDryRun(&out), "demo", devProfile)
	if err != nil || got.Context != "ois-demo" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTargetIsMinikube(t *testing.T) {
	if !(Target{Kind: spec.TargetMinikube}).IsMinikube() || (Target{Kind: spec.TargetKubernetes}).IsMinikube() {
		t.Fatal("IsMinikube")
	}
}
