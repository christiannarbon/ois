package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/christiannarbon/ois/internal/execx"
)

func TestVersionCommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	root := NewRootCommand(&out, &errBuf)
	root.SetArgs([]string{"version"})
	if code := run(root, &errBuf); code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %q", code, errBuf.String())
	}
	if !strings.HasPrefix(out.String(), "ois dev (commit none, built unknown)") {
		t.Fatalf("unexpected output %q", out.String())
	}
	if !strings.Contains(out.String(), "KUBERNETES_VERSION") {
		t.Fatalf("pinned versions missing from %q", out.String())
	}
}

func TestGlobalFlags(t *testing.T) {
	pf := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{}).PersistentFlags()
	for _, name := range []string{"file", "dry-run", "verbose", "profile", "yes"} {
		if pf.Lookup(name) == nil {
			t.Errorf("persistent flag --%s is not registered", name)
		}
	}
	for short, long := range map[string]string{"f": "file", "v": "verbose"} {
		if f := pf.ShorthandLookup(short); f == nil || f.Name != long {
			t.Errorf("-%s is not the shorthand for --%s", short, long)
		}
	}
	if got := pf.Lookup("profile").DefValue; got != "dev" {
		t.Errorf("--profile default = %q, want dev", got)
	}
}

func TestExitCodes(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		code    int
		wantErr string
	}{
		{"silent exit code", &ExitCodeError{Code: ExitChanges}, ExitChanges, ""},
		{"exit code with message", &ExitCodeError{Code: ExitPreflight, Err: errors.New("docker is not running")}, ExitPreflight, "error: docker is not running\n"},
		{"plain error", errors.New("boom"), ExitError, "error: boom\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			root := NewRootCommand(&out, &errBuf)
			root.AddCommand(&cobra.Command{
				Use:  "fail",
				RunE: func(*cobra.Command, []string) error { return tc.err },
			})
			root.SetArgs([]string{"fail"})
			if code := run(root, &errBuf); code != tc.code {
				t.Errorf("exit code = %d, want %d", code, tc.code)
			}
			if errBuf.String() != tc.wantErr {
				t.Errorf("stderr = %q, want %q", errBuf.String(), tc.wantErr)
			}
		})
	}
}

func TestUnknownCommandExitsWithError(t *testing.T) {
	var errBuf bytes.Buffer
	root := NewRootCommand(&bytes.Buffer{}, &errBuf)
	root.SetArgs([]string{"nope"})
	if code := run(root, &errBuf); code != ExitError {
		t.Fatalf("exit code = %d, want %d", code, ExitError)
	}
	if !strings.Contains(errBuf.String(), "unknown command") {
		t.Fatalf("stderr = %q", errBuf.String())
	}
}

func TestRunnerFollowsDryRun(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	cmd := execx.Cmd{Name: "touch", Args: []string{marker}}

	var dry bytes.Buffer
	g := &GlobalOptions{DryRun: true, Stderr: &dry}
	if _, err := g.Runner().Run(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("--dry-run executed the command")
	}
	if !strings.HasPrefix(dry.String(), "+ touch ") {
		t.Errorf("dry-run output = %q", dry.String())
	}

	var log bytes.Buffer
	g = &GlobalOptions{Verbose: true, Stderr: &log}
	if _, err := g.Runner().Run(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("real runner did not run the command: %v", err)
	}
	if !strings.HasPrefix(log.String(), "+ touch ") {
		t.Errorf("verbose log = %q", log.String())
	}
}

func TestSpecPath(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(dir, "ois.yaml")
	if err := os.WriteFile(spec, []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	for _, file := range []string{"", "../ois.yaml", ".."} {
		g := &GlobalOptions{File: file}
		got, err := g.SpecPath()
		if err != nil {
			t.Fatalf("File=%q: %v", file, err)
		}
		if got != spec {
			t.Errorf("File=%q: SpecPath = %q, want %q", file, got, spec)
		}
	}
}

func TestVersionListsPins(t *testing.T) {
	var out bytes.Buffer
	pins := map[string]string{"B_VERSION": "2.0", "A_LONGER_VERSION": "1.0", "UNPINNED": ""}
	if err := writeVersion(&out, pins); err != nil {
		t.Fatal(err)
	}
	want := "ois dev (commit none, built unknown)\n\npinned versions:\n" +
		"  A_LONGER_VERSION  1.0\n" +
		"  B_VERSION         2.0\n"
	if out.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestGlobalTarget(t *testing.T) {
	yml := "apiVersion: ois/v1alpha1\nproject: demo\ncomponents: {storage: {}}\n" +
		"profiles:\n  prod: {target: kubernetes, context: prod-admin}\n"
	_, _, _, dir := runValidate(t, yml)
	for profile, want := range map[string]string{"dev": "ois-demo", "prod": "prod-admin"} {
		var errBuf bytes.Buffer
		g := &GlobalOptions{File: dir, Profile: profile, DryRun: true, Stderr: &errBuf}
		r, target, err := g.Target(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if r.Profile.Name != profile || target.Context != want || target.Project != "demo" {
			t.Errorf("%s: got %+v", profile, target)
		}
		if !strings.Contains(errBuf.String(), "kubectl config get-contexts -o name") {
			t.Errorf("%s: dry-run output %q", profile, errBuf.String())
		}
	}
}
