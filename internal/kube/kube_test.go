package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/christiannarbon/ois/internal/cluster"
	"github.com/christiannarbon/ois/internal/execx"
	"github.com/christiannarbon/ois/internal/spec"
)

var demo = cluster.Target{Project: "demo", Profile: "dev", Kind: spec.TargetMinikube, Context: "ois-demo"}

func newClient(f *execx.Fake) *Client { return New(f, demo) }

func notFound(kind, name string) error {
	return &execx.ExitError{Cmd: "kubectl", ExitCode: 1,
		Stderr: `Error from server (NotFound): ` + kind + ` "` + name + `" not found`}
}

func stdin(t *testing.T, c execx.Cmd) []byte {
	t.Helper()
	if c.Stdin == nil {
		t.Fatal("no stdin")
	}
	b, err := io.ReadAll(c.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertLines(t *testing.T, f *execx.Fake, want ...string) {
	t.Helper()
	if got := f.CommandLines(); !slices.Equal(got, want) {
		t.Fatalf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestNew(t *testing.T) {
	k := New(&execx.Fake{}, demo)
	if k.Context != "ois-demo" || k.Project != "demo" {
		t.Fatalf("got %+v", k)
	}
}

func TestApply(t *testing.T) {
	f := &execx.Fake{}
	manifest := []byte("kind: ConfigMap\n")
	if err := newClient(f).Apply(context.Background(), manifest, "s3cret"); err != nil {
		t.Fatal(err)
	}
	assertLines(t, f, "kubectl --context ois-demo apply --server-side --field-manager=ois --force-conflicts -f -")
	if got := stdin(t, f.Calls[0]); !bytes.Equal(got, manifest) {
		t.Fatalf("stdin %q", got)
	}
	if !slices.Equal(f.Calls[0].Secrets, []string{"s3cret"}) {
		t.Fatalf("secrets %q", f.Calls[0].Secrets)
	}
}

func TestEnsureNamespace(t *testing.T) {
	f := &execx.Fake{}
	if err := newClient(f).EnsureNamespace(context.Background(), "storage"); err != nil {
		t.Fatal(err)
	}
	assertLines(t, f, "kubectl --context ois-demo apply --server-side --field-manager=ois --force-conflicts -f -")
	var ns struct {
		Kind     string
		Metadata struct {
			Name   string
			Labels map[string]string
		}
	}
	if err := json.Unmarshal(stdin(t, f.Calls[0]), &ns); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"app.kubernetes.io/part-of": "demo", "app.kubernetes.io/managed-by": "ois"}
	if ns.Kind != "Namespace" || ns.Metadata.Name != "storage" || len(ns.Metadata.Labels) != 2 ||
		ns.Metadata.Labels["app.kubernetes.io/part-of"] != want["app.kubernetes.io/part-of"] ||
		ns.Metadata.Labels["app.kubernetes.io/managed-by"] != want["app.kubernetes.io/managed-by"] {
		t.Fatalf("got %+v", ns)
	}
}

func TestSimpleCommands(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		call func(k *Client) error
		want string
	}{
		{"wait", func(k *Client) error {
			return k.Wait(ctx, "storage", "job/ois-storage-buckets", "condition=complete", 2*time.Minute)
		}, "kubectl --context ois-demo -n storage wait job/ois-storage-buckets --for=condition=complete --timeout=120s"},
		{"wait cluster-scoped", func(k *Client) error {
			return k.Wait(ctx, "", "nodes", "condition=Ready", 1500*time.Millisecond)
		}, "kubectl --context ois-demo wait nodes --for=condition=Ready --timeout=2s"},
		{"rollout", func(k *Client) error {
			return k.RolloutStatus(ctx, "storage", "deployment/rustfs", 3*time.Minute)
		}, "kubectl --context ois-demo -n storage rollout status deployment/rustfs --timeout=180s"},
		{"delete", func(k *Client) error {
			return k.Delete(ctx, "storage", "job/x")
		}, "kubectl --context ois-demo -n storage delete job/x --ignore-not-found --wait=true"},
		{"exec", func(k *Client) error {
			return k.Exec(ctx, "storage", "rustfs-0", "rustfs", []string{"ls", "-l"}, nil, io.Discard, io.Discard, false)
		}, "kubectl --context ois-demo -n storage exec rustfs-0 -c rustfs -- ls -l"},
		{"exec interactive", func(k *Client) error {
			return k.Exec(ctx, "storage", "rustfs-0", "rustfs", []string{"sh"}, strings.NewReader(""), io.Discard, io.Discard, true)
		}, "kubectl --context ois-demo -n storage exec rustfs-0 -c rustfs -i -t -- sh"},
		{"port-forward", func(k *Client) error {
			return k.PortForward(ctx, "storage", "svc/rustfs", 9001, 9001, io.Discard, io.Discard)
		}, "kubectl --context ois-demo -n storage port-forward svc/rustfs 9001:9001"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &execx.Fake{}
			if err := c.call(newClient(f)); err != nil {
				t.Fatal(err)
			}
			assertLines(t, f, c.want)
		})
	}
}

func TestExecStreamsStdin(t *testing.T) {
	f := (&execx.Fake{}).On("kubectl --context ois-demo -n storage exec", execx.Result{Stdout: []byte("out"), Stderr: []byte("err")}, nil)
	in := strings.NewReader("select 1")
	var out, errOut bytes.Buffer
	if err := newClient(f).Exec(context.Background(), "storage", "p", "c", []string{"sh"}, in, &out, &errOut, false); err != nil {
		t.Fatal(err)
	}
	if f.Calls[0].Stdin != in || out.String() != "out" || errOut.String() != "err" {
		t.Fatalf("stdin %v, out %q, err %q", f.Calls[0].Stdin, out.String(), errOut.String())
	}
}

func TestPortForward(t *testing.T) {
	boom := errors.New("address already in use")
	f := (&execx.Fake{}).On("kubectl --context ois-demo -n storage port-forward", execx.Result{}, boom)
	if err := newClient(f).PortForward(context.Background(), "storage", "svc/x", 1, 2, nil, nil); !errors.Is(err, boom) {
		t.Fatalf("live ctx: err %v, want %v", err, boom)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := newClient(f).PortForward(ctx, "storage", "svc/x", 1, 2, nil, nil); err != nil {
		t.Fatalf("cancelled ctx: err %v, want nil", err)
	}
}

func TestGetJSON(t *testing.T) {
	const cmd = "kubectl --context ois-demo -n storage get secret creds -o json"
	boom := errors.New("connection refused")
	cases := []struct {
		name      string
		res       execx.Result
		err       error
		wantOut   string
		wantFound bool
		wantErr   error
	}{
		{"found", execx.Result{Stdout: []byte(" {\"kind\":\"Secret\"}\n")}, nil, `{"kind":"Secret"}`, true, nil},
		{"not found", execx.Result{}, notFound("secrets", "creds"), "", false, nil},
		{"not found in result stderr", execx.Result{Stderr: []byte("Error from server (NotFound): x")}, errors.New("exit 1"), "", false, nil},
		{"dry run", execx.Result{}, nil, "", false, nil},
		{"error", execx.Result{}, boom, "", false, boom},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := (&execx.Fake{}).On(cmd, c.res, c.err)
			out, found, err := newClient(f).GetJSON(context.Background(), "storage", "secret", "creds")
			if string(out) != c.wantOut || found != c.wantFound || !errors.Is(err, c.wantErr) {
				t.Fatalf("got (%q, %v, %v)", out, found, err)
			}
			assertLines(t, f, cmd)
		})
	}
}

// Every method's command line names the project's context right after kubectl.
func TestEveryCallCarriesContext(t *testing.T) {
	ctx := context.Background()
	f := &execx.Fake{}
	k := newClient(f)
	_ = k.Apply(ctx, nil)
	_ = k.EnsureNamespace(ctx, "ns")
	_ = k.Wait(ctx, "ns", "pod/x", "condition=Ready", time.Second)
	_ = k.RolloutStatus(ctx, "ns", "deployment/x", time.Second)
	_ = k.Delete(ctx, "ns", "job/x")
	_, _, _ = k.GetJSON(ctx, "ns", "pods")
	_ = k.Exec(ctx, "ns", "p", "c", []string{"true"}, nil, nil, nil, false)
	_ = k.PortForward(ctx, "ns", "svc/x", 1, 2, nil, nil)
	_, _, _ = k.GetSecret(ctx, "ns", "s")
	_ = k.ApplySecret(ctx, "ns", "s", map[string]string{"k": "v"}, nil)
	_ = k.RunSetupJob(ctx, "ns", "job", []byte("{}"), time.Second)
	_ = k.ForgetSetupJob(ctx, "ns", "job")
	lines := f.CommandLines()
	if len(lines) < 14 {
		t.Fatalf("only %d calls: %q", len(lines), lines)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "kubectl --context ois-demo ") {
			t.Errorf("no context: %s", l)
		}
	}
}
