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
	"kubectl config get-contexts -o name",
	"kubectl --context ois-demo wait --for=condition=Ready nodes --all --timeout=300s",
	"kubectl --context ois-demo wait --for=condition=Ready pods --all -n kube-system --field-selector=status.phase!=Succeeded --timeout=300s",
	"kubectl --context ois-demo get storageclass -o json",
}

func profiles(status string, cpus, mem int) execx.Result {
	return profilesWith(status, cpus, mem, "docker", "v1.30.0")
}

func profilesWith(status string, cpus, mem int, driver, version string) execx.Result {
	return execx.Result{Stdout: []byte(`{"invalid": [], "valid": [{"Name": "ois-demo", "Status": "` + status +
		`", "Config": {"CPUs": ` + strconv.Itoa(cpus) + `, "Memory": ` + strconv.Itoa(mem) +
		`, "Driver": "` + driver + `", "KubernetesConfig": {"KubernetesVersion": "` + version + `"}}}]}`)}
}

var invalidProfile = execx.Result{Stdout: []byte(`{"invalid": [{"Name": "ois-demo"}], "valid": []}`)}

// runCLI runs ois against upSpec with a fake runner and the given stdin.
func runCLI(t *testing.T, f *execx.Fake, stdin io.Reader, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return runCLIWith(t, f, &GlobalOptions{stdin: stdin}, args...)
}

// runCLIWith is runCLI with test-only GlobalOptions fields (stdin, terminal) preset.
func runCLIWith(t *testing.T, f *execx.Fake, g *GlobalOptions, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Setenv("OIS_MINIKUBE_DRIVER", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ois.yaml"), []byte(upSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	g.Stdout, g.Stderr, g.runnerOverride = &out, &errBuf, f
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
	if code, _, errOut := runCLI(t, f, nil, "up", "--recreate", "--yes"); code != ExitOK {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	want := []string{
		listCmd,
		"minikube delete -p ois-demo",
		"kubectl config delete-context ois-demo",
		"kubectl config delete-cluster ois-demo",
		"kubectl config unset users.ois-demo",
	}
	assertCommands(t, f, append(want, upCommands[1:]...))
}

func TestUpRecreateAsksFirst(t *testing.T) {
	f := (&execx.Fake{}).On(listCmd, profiles("OK", 2, 4096), nil)
	code, _, errOut := runCLI(t, f, strings.NewReader("y\n"), "up", "--recreate")
	if code != ExitError || errOut != "error: refusing to recreate without --yes when not interactive\n" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	assertCommands(t, f, []string{listCmd})

	f = (&execx.Fake{}).On(listCmd, profiles("OK", 2, 4096), nil)
	g := &GlobalOptions{stdin: strings.NewReader("n\n"), terminal: func(io.Reader) bool { return true }}
	code, _, errOut = runCLIWith(t, f, g, "up", "--recreate")
	if code != ExitError || !strings.HasPrefix(errOut, "Delete cluster ois-demo and ALL data in it, then recreate it? [y/N] ") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	assertCommands(t, f, []string{listCmd})
}

func TestUpRecreateOnlyOnDrift(t *testing.T) {
	f := (&execx.Fake{}).
		On(listCmd, profiles("OK", 4, 7168), nil).
		On("kubectl --context ois-demo get storageclass", execx.Result{Stdout: []byte(oneDefaultSC)}, nil)
	if code, _, errOut := runCLI(t, f, nil, "up", "--recreate"); code != ExitOK {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	assertCommands(t, f, upCommands)
}

func TestUpDrift(t *testing.T) {
	t.Setenv("OIS_MINIKUBE_DRIVER", "")
	cases := []struct {
		name string
		res  execx.Result
		want string
	}{
		{"kubernetes version", profilesWith("OK", 4, 7168, "docker", "v1.29.4"),
			"error: cluster \"ois-demo\" has Kubernetes v1.29.4 but ois pins v1.30.0.\n" +
				"minikube cannot change these on an existing cluster. Recreate it (this deletes all data in it):\n  ois up --recreate\n"},
		{"driver", profilesWith("OK", 4, 7168, "hyperkit", "v1.30.0"),
			"error: cluster \"ois-demo\" uses the hyperkit driver but ois is set to use docker (OIS_MINIKUBE_DRIVER).\n" +
				"minikube cannot change these on an existing cluster. Recreate it (this deletes all data in it):\n  ois up --recreate\n"},
		{"size and version", profilesWith("Stopped", 2, 4096, "docker", "v1.31.0"),
			"error: cluster \"ois-demo\" exists with 2 CPU / 4096 MiB but ois.yaml asks for 4 CPU / 7168 MiB.\n" +
				"cluster \"ois-demo\" has Kubernetes v1.31.0 but ois pins v1.30.0.\n" +
				"minikube cannot change these on an existing cluster. Recreate it (this deletes all data in it):\n  ois up --recreate\n"},
		{"unreported fields are not compared", profilesWith("OK", 4, 7168, "", ""), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := (&execx.Fake{}).On(listCmd, c.res, nil)
			code, _, errOut := runCLI(t, f, nil, "up")
			if c.want == "" {
				if code != ExitOK {
					t.Fatalf("code %d stderr %q", code, errOut)
				}
				return
			}
			if code != ExitError || errOut != c.want {
				t.Fatalf("code %d stderr\n%s\nwant\n%s", code, errOut, c.want)
			}
			assertCommands(t, f, []string{listCmd})
		})
	}
}

func TestUpInvalidProfile(t *testing.T) {
	f := (&execx.Fake{}).On(listCmd, invalidProfile, nil)
	code, _, errOut := runCLI(t, f, nil, "up")
	if code != ExitError || errOut != "error: minikube reports cluster ois-demo as invalid; delete it with: ois down --purge\n" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	assertCommands(t, f, []string{listCmd})
}

func TestUpFailures(t *testing.T) {
	boom := &execx.ExitError{Cmd: "x", ExitCode: 1, Stderr: "boom"}
	cases := []struct {
		name, prefix, want string
		res                execx.Result
	}{
		{"start", "minikube start", "boom", execx.Result{}},
		{"addons", "minikube addons enable storage-provisioner", "boom", execx.Result{}},
		{"context not created", "kubectl config get-contexts", `kube context "ois-demo" not found`, execx.Result{Stdout: []byte("minikube\n")}},
		{"nodes", "kubectl --context ois-demo wait --for=condition=Ready nodes", "boom", execx.Result{}},
		{"system pods", "kubectl --context ois-demo wait --for=condition=Ready pods", "boom", execx.Result{}},
		{"storage classes", "kubectl --context ois-demo get storageclass", "boom", execx.Result{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var err error
			if c.res.Stdout == nil {
				err = boom
			}
			f := (&execx.Fake{}).On(c.prefix, c.res, err)
			code, out, errOut := runCLI(t, f, nil, "up")
			if code != ExitError || !strings.Contains(errOut, c.want) || strings.Contains(out, "✓") {
				t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
			}
			last := f.CommandLines()[len(f.Calls)-1]
			if !strings.HasPrefix(last, c.prefix) {
				t.Fatalf("kept going after the failure: last command %q", last)
			}
		})
	}
}

func TestUpRecreateDeleteFails(t *testing.T) {
	f := (&execx.Fake{}).
		On(listCmd, profiles("OK", 2, 4096), nil).
		On("minikube delete", execx.Result{}, &execx.ExitError{Cmd: "minikube delete", ExitCode: 1, Stderr: "busy"})
	code, _, errOut := runCLI(t, f, nil, "up", "--recreate", "--yes")
	if code != ExitError || !strings.Contains(errOut, "busy") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	assertCommands(t, f, []string{listCmd, "minikube delete -p ois-demo"})
}

func TestUpVerboseStreamsMinikube(t *testing.T) {
	f := (&execx.Fake{}).On("minikube start", execx.Result{Stdout: []byte("😄 minikube progress\n")}, nil)
	code, _, errOut := runCLI(t, f, nil, "-v", "up")
	if code != ExitOK || !strings.Contains(errOut, "😄 minikube progress") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
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
	assertCommands(t, f, []string{listCmd, "minikube stop -p ois-demo"})
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
	for answer, deleted := range map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false, "": false, "yep\n": false} {
		f := (&execx.Fake{}).On(listCmd, profiles("OK", 4, 7168), nil)
		g := &GlobalOptions{stdin: strings.NewReader(answer), terminal: func(io.Reader) bool { return true }}
		code, _, errOut := runCLIWith(t, f, g, "down", "--purge")
		if !strings.HasPrefix(errOut, "Delete cluster ois-demo and ALL data in it? [y/N] ") {
			t.Errorf("%q: prompt %q", answer, errOut)
		}
		got := slices.Contains(f.CommandLines(), "minikube delete -p ois-demo")
		if got != deleted || (code == ExitOK) != deleted {
			t.Errorf("%q: code %d deleted %v, want %v", answer, code, got, deleted)
		}
	}
}

func TestDownPurgeInvalidProfile(t *testing.T) {
	f := (&execx.Fake{}).On(listCmd, invalidProfile, nil)
	code, out, errOut := runCLI(t, f, nil, "down", "--purge", "--yes")
	if code != ExitOK || out != "✓ cluster ois-demo deleted\n" {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
	}
	assertCommands(t, f, []string{listCmd, "minikube delete -p ois-demo",
		"kubectl config delete-context ois-demo", "kubectl config delete-cluster ois-demo", "kubectl config unset users.ois-demo"})
}

func TestDownInvalidProfile(t *testing.T) {
	f := (&execx.Fake{}).On(listCmd, invalidProfile, nil)
	code, _, errOut := runCLI(t, f, nil, "down")
	if code != ExitError || !strings.Contains(errOut, "delete it with: ois down --purge") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestConfirmSharesOneReader(t *testing.T) {
	in := strings.NewReader("y\nyes\n")
	var errBuf bytes.Buffer
	g := &GlobalOptions{Stderr: &errBuf, terminal: func(io.Reader) bool { return true }}
	for i := range 2 {
		if err := confirm(g, in, "ok?", "go"); err != nil {
			t.Fatalf("prompt %d: %v", i+1, err)
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
