package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/christiannarbon/ois/internal/execx"
)

const (
	listCmd      = "minikube profile list -o json"
	upSpec       = "apiVersion: ois/v1alpha1\nproject: demo\ncomponents: {storage: {}}\nprofiles:\n  prod: {target: kubernetes, context: prod-admin}\n"
	oneDefaultSC = `{"items": [
	  {"metadata": {"name": "standard", "annotations": {"storageclass.kubernetes.io/is-default-class": "true"}}},
	  {"metadata": {"name": "slow"}}]}`
)

var upCommands = []string{
	listCmd,
	"minikube start -p ois-demo --driver=docker --kubernetes-version=v1.30.0 --cpus=4 --memory=7168 --disk-size=30g --keep-context --interactive=false",
	"minikube addons enable default-storageclass -p ois-demo",
	"minikube addons enable storage-provisioner -p ois-demo",
	"kubectl --context ois-demo wait --for=condition=Ready nodes --all --timeout=300s",
	"kubectl --context ois-demo get storageclass -o json",
}

func profiles(status string, cpus, mem int) execx.Result {
	return execx.Result{Stdout: []byte(`{"invalid": [], "valid": [{"Name": "ois-demo", "Status": "` + status +
		`", "Config": {"CPUs": ` + strconv.Itoa(cpus) + `, "Memory": ` + strconv.Itoa(mem) + `}}]}`)}
}

// runCLI runs ois against upSpec with a fake runner and the given stdin.
func runCLI(t *testing.T, f *execx.Fake, stdin io.Reader, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Setenv("OIS_MINIKUBE_DRIVER", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ois.yaml"), []byte(upSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	g := &GlobalOptions{Stdout: &out, Stderr: &errBuf, stdin: stdin, runnerOverride: f}
	root := newRootCommand(g)
	root.SetArgs(append([]string{"-f", dir}, args...))
	code = run(root, &errBuf)
	return code, out.String(), errBuf.String()
}

func assertCommands(t *testing.T, f *execx.Fake, want []string) {
	t.Helper()
	if got := f.CommandLines(); !slices.Equal(got, want) {
		t.Fatalf("commands\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestUpCreates(t *testing.T) {
	f := (&execx.Fake{}).
		On(listCmd, execx.Result{Stdout: []byte(`{"invalid": [], "valid": []}`)}, nil).
		On("kubectl --context ois-demo get storageclass", execx.Result{Stdout: []byte(oneDefaultSC)}, nil)
	code, out, errOut := runCLI(t, f, nil, "up")
	if code != ExitOK {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	assertCommands(t, f, upCommands)
	want := "✓ cluster ois-demo ready (4 CPU, 7168 MiB) — kube context \"ois-demo\"\n" +
		"  your current kube context was not changed; ois always uses \"ois-demo\"\n"
	if !strings.HasSuffix(out, want) {
		t.Fatalf("stdout\n%s\nwant suffix\n%s", out, want)
	}
}

func TestUpIdempotentWhenRunning(t *testing.T) {
	f := (&execx.Fake{}).
		On(listCmd, profiles("OK", 4, 7168), nil).
		On("kubectl --context ois-demo get storageclass", execx.Result{Stdout: []byte(oneDefaultSC)}, nil)
	if code, _, errOut := runCLI(t, f, nil, "up"); code != ExitOK {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	assertCommands(t, f, upCommands)
}

func TestUpSizeMismatch(t *testing.T) {
	f := (&execx.Fake{}).On(listCmd, profiles("OK", 2, 4096), nil)
	code, _, errOut := runCLI(t, f, nil, "up")
	want := "error: cluster \"ois-demo\" is running with 2 CPU / 4096 MiB but ois.yaml asks for 4 CPU / 7168 MiB.\n" +
		"minikube cannot resize a cluster. Recreate it (this deletes all data in it):\n" +
		"  ois up --recreate\n"
	if code != ExitError || errOut != want {
		t.Fatalf("code %d stderr\n%s\nwant\n%s", code, errOut, want)
	}
	assertCommands(t, f, []string{listCmd})
}

func TestUpSizeMismatchStopped(t *testing.T) {
	f := (&execx.Fake{}).On(listCmd, profiles("Stopped", 2, 4096), nil)
	code, _, errOut := runCLI(t, f, nil, "up")
	if code != ExitError || !strings.Contains(errOut, `cluster "ois-demo" exists with 2 CPU / 4096 MiB`) {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestUpRecreate(t *testing.T) {
	f := (&execx.Fake{}).
		On(listCmd, profiles("OK", 2, 4096), nil).
		On("kubectl --context ois-demo get storageclass", execx.Result{Stdout: []byte(oneDefaultSC)}, nil)
	if code, _, errOut := runCLI(t, f, nil, "up", "--recreate"); code != ExitOK {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	want := []string{
		listCmd,
		listCmd,
		"minikube delete -p ois-demo",
		"kubectl config delete-context ois-demo",
		"kubectl config delete-cluster ois-demo",
		"kubectl config unset users.ois-demo",
	}
	assertCommands(t, f, append(want, upCommands[1:]...))
}

func TestUpTwoDefaultStorageClasses(t *testing.T) {
	two := `{"items": [
	  {"metadata": {"name": "standard", "annotations": {"storageclass.kubernetes.io/is-default-class": "true"}}},
	  {"metadata": {"name": "fast", "annotations": {"storageclass.kubernetes.io/is-default-class": "true"}}}]}`
	f := (&execx.Fake{}).On("kubectl --context ois-demo get storageclass", execx.Result{Stdout: []byte(two)}, nil)
	code, _, errOut := runCLI(t, f, nil, "up")
	want := "error: cluster \"ois-demo\" must have exactly one default StorageClass, found 2: [standard, fast]\n"
	if code != ExitError || errOut != want {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestUpNoDefaultStorageClass(t *testing.T) {
	f := (&execx.Fake{}).On("kubectl --context ois-demo get storageclass", execx.Result{Stdout: []byte(`{"items": []}`)}, nil)
	code, _, errOut := runCLI(t, f, nil, "up")
	if code != ExitError || !strings.Contains(errOut, "found 0: []") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestUpDownRefuseKubernetesProfile(t *testing.T) {
	for _, args := range [][]string{{"up"}, {"down"}, {"down", "--purge", "--yes"}} {
		f := &execx.Fake{}
		code, _, errOut := runCLI(t, f, nil, append([]string{"--profile", "prod"}, args...)...)
		want := "error: ois " + args[0] + " manages local minikube clusters only; profile \"prod\" targets an existing cluster (context \"prod-admin\")\n"
		if code != ExitError || errOut != want {
			t.Errorf("%v: code %d stderr %q", args, code, errOut)
		}
		if n := len(f.Calls); n != 0 {
			t.Errorf("%v: %d external calls, want 0: %q", args, n, f.CommandLines())
		}
	}
}

func TestDownStops(t *testing.T) {
	f := (&execx.Fake{}).On(listCmd, profiles("OK", 4, 7168), nil)
	code, out, errOut := runCLI(t, f, nil, "down")
	if code != ExitOK || out != "✓ cluster ois-demo stopped (data kept)\n" {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
	}
	assertCommands(t, f, []string{listCmd, listCmd, "minikube stop -p ois-demo"})
}

func TestDownMissing(t *testing.T) {
	for _, args := range [][]string{{"down"}, {"down", "--purge"}} {
		f := (&execx.Fake{}).On(listCmd, execx.Result{}, &execx.ExitError{ExitCode: 85, Stderr: "No minikube profile was found"})
		code, out, _ := runCLI(t, f, nil, args...)
		if code != ExitOK || out != "cluster ois-demo does not exist\n" {
			t.Errorf("%v: code %d stdout %q", args, code, out)
		}
		assertCommands(t, f, []string{listCmd})
	}
}

func TestDownPurgeNonInteractiveRefused(t *testing.T) {
	f := (&execx.Fake{}).On(listCmd, profiles("OK", 4, 7168), nil)
	code, _, errOut := runCLI(t, f, strings.NewReader("yes\n"), "down", "--purge")
	if code != ExitError || errOut != "error: refusing to delete without --yes when not interactive\n" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	assertCommands(t, f, []string{listCmd})
}

func TestDownPurgeYes(t *testing.T) {
	f := (&execx.Fake{}).On(listCmd, profiles("Stopped", 4, 7168), nil)
	code, out, errOut := runCLI(t, f, nil, "down", "--purge", "--yes")
	if code != ExitOK || out != "✓ cluster ois-demo deleted\n" {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
	}
	if !slices.Contains(f.CommandLines(), "minikube delete -p ois-demo") {
		t.Fatalf("no delete: %q", f.CommandLines())
	}
}

func TestDownPurgeAnswers(t *testing.T) {
	isTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { isTerminal = defaultIsTerminal })
	for answer, deleted := range map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false, "": false, "yep\n": false} {
		f := (&execx.Fake{}).On(listCmd, profiles("OK", 4, 7168), nil)
		code, _, errOut := runCLI(t, f, strings.NewReader(answer), "down", "--purge")
		if !strings.HasPrefix(errOut, "Delete cluster ois-demo and ALL data in it? [y/N] ") {
			t.Errorf("%q: prompt %q", answer, errOut)
		}
		got := slices.Contains(f.CommandLines(), "minikube delete -p ois-demo")
		if got != deleted || (code == ExitOK) != deleted {
			t.Errorf("%q: code %d deleted %v, want %v", answer, code, got, deleted)
		}
	}
}

func TestIsTerminalRejectsPlainReaders(t *testing.T) {
	if isTerminal(strings.NewReader("y\n")) {
		t.Fatal("a strings.Reader is not a terminal")
	}
}

func TestIsTerminalRejectsDevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Fatal("/dev/null is not a terminal")
	}
}
