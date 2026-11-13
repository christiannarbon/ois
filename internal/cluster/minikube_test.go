package cluster

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	assets "github.com/christiannarbon/ois/components"
	"github.com/christiannarbon/ois/internal/execx"
)

const listCmd = "minikube profile list -o json"

func profileJSON(status string) string {
	return `{"invalid": [], "valid": [
	  {"Name": "other", "Status": "Running", "Config": {"CPUs": 2, "Memory": 4096}},
	  {"Name": "ois-demo", "Status": "` + status + `",
	   "Config": {"CPUs": 4, "Memory": 7168, "KubernetesConfig": {"KubernetesVersion": "v1.30.0"}}}]}`
}

func newTestManager(f *execx.Fake) *Manager {
	return &Manager{Run: f, Profile: "ois-demo", K8sVersion: "v1.30.0", Driver: "docker"}
}

func TestNew(t *testing.T) {
	t.Setenv(DriverEnv, "")
	m, err := New(&execx.Fake{}, "demo")
	if err != nil {
		t.Fatal(err)
	}
	ver, _ := assets.Version("KUBERNETES_VERSION")
	if m.Profile != "ois-demo" || m.Driver != "docker" || m.K8sVersion != ver {
		t.Fatalf("got %+v", m)
	}

	t.Setenv(DriverEnv, "hyperkit")
	m, _ = New(&execx.Fake{}, "demo")
	if m.Driver != "hyperkit" {
		t.Fatalf("driver %q, want hyperkit", m.Driver)
	}
}

func TestInspect(t *testing.T) {
	cases := []struct {
		name  string
		res   execx.Result
		err   error
		state State
		size  Size
	}{
		{"running", execx.Result{Stdout: []byte(profileJSON("Running"))}, nil, StateRunning, Size{CPUs: 4, MemoryMiB: 7168}},
		{"ok", execx.Result{Stdout: []byte(profileJSON("OK"))}, nil, StateRunning, Size{CPUs: 4, MemoryMiB: 7168}},
		{"degraded", execx.Result{Stdout: []byte(profileJSON("Degraded"))}, nil, StateRunning, Size{CPUs: 4, MemoryMiB: 7168}},
		{"stopped", execx.Result{Stdout: []byte(profileJSON("Stopped"))}, nil, StateStopped, Size{CPUs: 4, MemoryMiB: 7168}},
		{"paused", execx.Result{Stdout: []byte(profileJSON("Paused"))}, nil, StateStopped, Size{CPUs: 4, MemoryMiB: 7168}},
		{"other profiles only", execx.Result{Stdout: []byte(`{"invalid": [], "valid": [{"Name": "minikube", "Status": "Running"}]}`)}, nil, StateMissing, Size{}},
		{"no profiles at all", execx.Result{ExitCode: 85}, &execx.ExitError{Cmd: listCmd, ExitCode: 85, Stderr: "* No minikube profile was found."}, StateMissing, Size{}},
		{"dry run", execx.Result{}, nil, StateMissing, Size{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := (&execx.Fake{}).On(listCmd, c.res, c.err)
			state, size, err := newTestManager(f).Inspect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if state != c.state || size != c.size {
				t.Fatalf("got %s %+v, want %s %+v", state, size, c.state, c.size)
			}
		})
	}
}

func TestInspectErrors(t *testing.T) {
	cases := []struct {
		name string
		res  execx.Result
		err  error
		want string
	}{
		{"other exit error", execx.Result{}, &execx.ExitError{Cmd: listCmd, ExitCode: 1, Stderr: "docker not running"}, "docker not running"},
		{"bad json", execx.Result{Stdout: []byte("{")}, nil, "reading minikube profile list"},
		{"invalid profile", execx.Result{Stdout: []byte(`{"invalid": [{"Name": "ois-demo"}], "valid": []}`)}, nil, "minikube delete -p ois-demo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := (&execx.Fake{}).On(listCmd, c.res, c.err)
			_, _, err := newTestManager(f).Inspect(context.Background())
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestStart(t *testing.T) {
	f := &execx.Fake{}
	if err := newTestManager(f).Start(context.Background(), Size{CPUs: 4, MemoryMiB: 7168, Disk: "30g"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"minikube start -p ois-demo --driver=docker --kubernetes-version=v1.30.0" +
		" --cpus=4 --memory=7168 --disk-size=30g --keep-context --interactive=false"}
	if got := f.CommandLines(); !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestEnableAddons(t *testing.T) {
	f := &execx.Fake{}
	if err := newTestManager(f).EnableAddons(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"minikube addons enable default-storageclass -p ois-demo",
		"minikube addons enable storage-provisioner -p ois-demo",
	}
	if got := f.CommandLines(); !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestEnableAddonsStopsOnError(t *testing.T) {
	boom := errors.New("boom")
	f := (&execx.Fake{}).On("minikube addons enable default-storageclass", execx.Result{}, boom)
	if err := newTestManager(f).EnableAddons(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if n := len(f.Calls); n != 1 {
		t.Fatalf("%d calls, want 1", n)
	}
}

func TestStop(t *testing.T) {
	cases := []struct {
		name string
		list string
		want []string
	}{
		{"running", profileJSON("Running"), []string{listCmd, "minikube stop -p ois-demo"}},
		{"stopped", profileJSON("Stopped"), []string{listCmd}},
		{"missing", `{"invalid": [], "valid": []}`, []string{listCmd}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := (&execx.Fake{}).On(listCmd, execx.Result{Stdout: []byte(c.list)}, nil)
			if err := newTestManager(f).Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := f.CommandLines(); !slices.Equal(got, c.want) {
				t.Fatalf("got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	f := (&execx.Fake{}).
		On(listCmd, execx.Result{Stdout: []byte(profileJSON("Stopped"))}, nil).
		On("kubectl config delete-cluster", execx.Result{ExitCode: 1}, &execx.ExitError{ExitCode: 1, Stderr: "not found"})
	if err := newTestManager(f).Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		listCmd,
		"minikube delete -p ois-demo",
		"kubectl config delete-context ois-demo",
		"kubectl config delete-cluster ois-demo",
		"kubectl config unset users.ois-demo",
	}
	if got := f.CommandLines(); !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestDeleteMissing(t *testing.T) {
	f := (&execx.Fake{}).On(listCmd, execx.Result{}, &execx.ExitError{ExitCode: 85, Stderr: "No minikube profile was found"})
	if err := newTestManager(f).Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.CommandLines(); !slices.Equal(got, []string{listCmd}) {
		t.Fatalf("got %q", got)
	}
}

func TestDeleteFailureStopsBeforeCleanup(t *testing.T) {
	boom := errors.New("boom")
	f := (&execx.Fake{}).
		On(listCmd, execx.Result{Stdout: []byte(profileJSON("Running"))}, nil).
		On("minikube delete", execx.Result{}, boom)
	if err := newTestManager(f).Delete(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if n := len(f.Calls); n != 2 {
		t.Fatalf("%d calls, want 2: %q", n, f.CommandLines())
	}
}

func TestDryRunInspectIsMissing(t *testing.T) {
	var out bytes.Buffer
	m := &Manager{Run: execx.NewDryRun(&out), Profile: "ois-demo"}
	state, _, err := m.Inspect(context.Background())
	if err != nil || state != StateMissing {
		t.Fatalf("got %s, %v", state, err)
	}
	if !strings.Contains(out.String(), "+ "+listCmd) {
		t.Fatalf("dry-run output %q", out.String())
	}
}
