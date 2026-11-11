package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christiannarbon/ois/internal/project"
	"github.com/christiannarbon/ois/internal/spec"
)

func init() { spec.RegisterSample("test-sample") }

// runValidate writes yml (if non-empty) to <tmp>/ois.yaml and runs ois with args.
func runValidate(t *testing.T, yml string, args ...string) (code int, stdout, stderr string, dir string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if yml != "" {
		if err := os.WriteFile(filepath.Join(dir, "ois.yaml"), []byte(yml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errBuf bytes.Buffer
	root := NewRootCommand(&out, &errBuf)
	full := make([]string, 0, len(args))
	for _, a := range args {
		full = append(full, strings.ReplaceAll(a, "$DIR", dir))
	}
	root.SetArgs(full)
	code = run(root, &errBuf)
	return code, out.String(), errBuf.String(), dir
}

func TestValidateValid(t *testing.T) {
	yml := "apiVersion: ois/v1alpha1\nproject: demo\ncomponents:\n  storage: {}\nsamples: []\n"
	code, out, errOut, dir := runValidate(t, yml, "-f", "$DIR/ois.yaml", "validate")
	if code != ExitOK || errOut != "" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	want := "spec:        " + filepath.Join(dir, "ois.yaml") + "\n" +
		"project:     demo  (cluster profile ois-demo)\n" +
		"cluster:     lean\n" +
		"profile:     dev  (target minikube, context ois-demo, tier dev)\n" +
		"profiles:    dev\n" +
		"components:  storage\n" +
		"streams:     0\n" +
		"samples:     none\n" +
		"✓ ois.yaml is valid\n"
	if out != want {
		t.Fatalf("stdout\n%s\nwant\n%s", out, want)
	}
}

func TestValidateComponentOrder(t *testing.T) {
	yml := "apiVersion: ois/v1alpha1\nproject: demo\ncomponents:\n  trino:\n  catalog:\n  storage:\n"
	code, out, errOut, _ := runValidate(t, yml, "-f", "$DIR", "validate")
	if code != ExitOK {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	if !strings.Contains(out, "components:  storage, catalog, trino\n") {
		t.Fatalf("stdout %q", out)
	}
}

func TestValidateErrorsInLineOrder(t *testing.T) {
	yml := "apiVersion: ois/v1\nproject: demo\ncluster: {size: huge}\ncomponents:\n  storage: {}\n"
	code, out, errOut, _ := runValidate(t, yml, "-f", "$DIR/ois.yaml", "validate")
	if code != ExitError || out != "" {
		t.Fatalf("code %d stdout %q", code, out)
	}
	want := "ois.yaml:1: apiVersion: must be \"ois/v1alpha1\", got \"ois/v1\"\n" +
		"ois.yaml:3: cluster.size: must be one of minimal, lean, custom, got \"huge\"\n"
	if errOut != want {
		t.Fatalf("stderr\n%s\nwant\n%s", errOut, want)
	}
}

func TestValidateDecodeErrorPrintedOnce(t *testing.T) {
	yml := "apiVersion: ois/v1alpha1\nproject: demo\ncomponents:\n  stroage: {}\n"
	code, _, errOut, _ := runValidate(t, yml, "-f", "$DIR/ois.yaml", "validate")
	want := "ois.yaml:4: components.stroage: unknown field (did you mean \"storage\"?)\n"
	if code != ExitError || errOut != want {
		t.Fatalf("code %d stderr %q, want %q", code, errOut, want)
	}
}

func TestValidateNoSpec(t *testing.T) {
	_, _, _, dir := runValidate(t, "")
	t.Chdir(dir)
	var out, errBuf bytes.Buffer
	root := NewRootCommand(&out, &errBuf)
	root.SetArgs([]string{"validate"})
	code := run(root, &errBuf)
	if code != ExitError || !strings.Contains(errBuf.String(), `run "ois init"`) {
		t.Fatalf("code %d stderr %q", code, errBuf.String())
	}
	if strings.Count(errBuf.String(), "\n") != 1 {
		t.Fatalf("error printed more than once: %q", errBuf.String())
	}
}

func TestValidateRejectsArgs(t *testing.T) {
	code, _, errOut, _ := runValidate(t, "", "validate", "extra")
	if code != ExitError || !strings.Contains(errOut, "unknown command") && !strings.Contains(errOut, "accepts 0 arg") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{&spec.DecodeError{File: "/x/ois.yaml", Line: 4, Path: "project", Msg: "bad"}, "ois.yaml:4: project: bad\n"},
		{&spec.ValidationError{File: "/x/ois.yaml", Errors: []spec.FieldError{{Path: "a", Msg: "m", Line: 1}, {Path: "b", Msg: "n", Line: 2}}},
			"ois.yaml:1: a: m\nois.yaml:2: b: n\n"},
		{fmt.Errorf("loading: %w", &spec.DecodeError{File: "ois.yaml", Msg: "wrapped"}), "loading: ois.yaml: wrapped\n"},
		{errors.New("boom"), "error: boom\n"},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		writeError(&buf, tt.err)
		if buf.String() != tt.want {
			t.Errorf("writeError(%v) = %q, want %q", tt.err, buf.String(), tt.want)
		}
	}
}

func TestValidateProfileView(t *testing.T) {
	yml := `apiVersion: ois/v1alpha1
project: demo
components:
  storage: {}
  catalog: {}
samples: [test-sample]
profiles:
  prod:
    target: kubernetes
    context: prod-admin
    components:
      trino:
      storage:
        external:
          endpoint: https://s3.example.com
          credentials_secret: s3-creds
          buckets: {warehouse: wh, flink: fl}
      catalog:
        external:
          uri: https://polaris.example.com
          warehouse: lake
          credentials_secrets: {trino: trino-cat}
`
	code, out, errOut, _ := runValidate(t, yml, "-f", "$DIR", "--profile", "prod", "validate")
	if code != ExitOK {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	for _, want := range []string{
		"profile:     prod  (target kubernetes, context prod-admin, tier production)\n",
		"profiles:    dev, prod\n",
		"components:  storage (external), catalog (external), trino\n",
		"samples:     none\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestValidateUnknownProfile(t *testing.T) {
	yml := "apiVersion: ois/v1alpha1\nproject: demo\ncomponents: {storage: {}}\nprofiles:\n  prod: {target: kubernetes, context: c}\n"
	code, _, errOut, _ := runValidate(t, yml, "-f", "$DIR", "--profile", "prd", "validate")
	if code != ExitError || errOut != "error: unknown profile \"prd\" (available: dev, prod)\n" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

func TestValidateChecksOtherProfiles(t *testing.T) {
	yml := "apiVersion: ois/v1alpha1\nproject: demo\ncomponents: {storage: {}}\nprofiles:\n  prod:\n    target: kubernetes\n"
	code, _, errOut, _ := runValidate(t, yml, "-f", "$DIR", "validate")
	want := "ois.yaml:5: profiles.prod.context: context is required for target: kubernetes (ois never uses your current context)\n"
	if code != ExitError || errOut != want {
		t.Fatalf("code %d stderr %q, want %q", code, errOut, want)
	}
}

func TestGlobalResolve(t *testing.T) {
	_, _, _, dir := runValidate(t, "apiVersion: ois/v1alpha1\nproject: demo\ncomponents: {storage: {}}\n")
	r, err := (&GlobalOptions{File: dir, Profile: "dev"}).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Profile.Name != "dev" || !r.Enabled("storage") {
		t.Fatalf("got %+v", r.Profile)
	}
}

func TestMinikubeContextMatchesProjectContextName(t *testing.T) {
	for _, name := range []string{"demo", "my-lake", "a1"} {
		s, err := spec.Parse([]byte("apiVersion: ois/v1alpha1\nproject: "+name+"\ncomponents: {storage: {}}\n"), "ois.yaml")
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.Resolve(spec.DefaultProfile)
		if err != nil {
			t.Fatal(err)
		}
		if want := project.ContextName(name); r.Profile.Context != want {
			t.Errorf("context %q, want project.ContextName %q", r.Profile.Context, want)
		}
	}
}
