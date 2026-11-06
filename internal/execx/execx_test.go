package execx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedact(t *testing.T) {
	cases := []struct {
		name, in string
		secrets  []string
		want     string
	}{
		{"longest first", "key=abcdef", []string{"abc", "abcdef"}, "key=***"},
		{"shorter alone", "key=abcxyz", []string{"abc", "abcdef"}, "key=***xyz"},
		{"every occurrence", "s3cret and s3cret", []string{"s3cret"}, "*** and ***"},
		{"empty ignored", "nothing here", []string{""}, "nothing here"},
		{"no secrets", "plain", nil, "plain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Redact(tc.in, tc.secrets); got != tc.want {
				t.Errorf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCmdString(t *testing.T) {
	cases := []struct {
		name string
		cmd  Cmd
		want string
	}{
		{"plain", Cmd{Name: "kubectl", Args: []string{"get", "ns", "-o", "name"}}, "kubectl get ns -o name"},
		{"spaces", Cmd{Name: "sh", Args: []string{"-c", "echo hi"}}, "sh -c 'echo hi'"},
		{"single quote", Cmd{Name: "echo", Args: []string{"it's"}}, `echo 'it'\''s'`},
		{"empty arg", Cmd{Name: "echo", Args: []string{""}}, "echo ''"},
		{"secret", Cmd{Name: "helm", Args: []string{"--set", "password=hunter22"}, Secrets: []string{"hunter22"}}, "helm --set 'password=***'"},
		{"secret with quote", Cmd{Name: "x", Args: []string{"p'w d"}, Secrets: []string{"p'w d"}}, "x '***'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cmd.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRealExitError(t *testing.T) {
	var log bytes.Buffer
	r := NewReal(&log, true)
	c := Cmd{
		Name:    "sh",
		Args:    []string{"-c", "echo out; echo err topsecret >&2; exit 3", "topsecret"},
		Secrets: []string{"topsecret"},
	}
	res, err := r.Run(context.Background(), c)
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want *ExitError", err)
	}
	if res.ExitCode != 3 || ee.ExitCode != 3 {
		t.Errorf("exit code = %d / %d, want 3", res.ExitCode, ee.ExitCode)
	}
	if string(res.Stdout) != "out\n" {
		t.Errorf("stdout = %q", res.Stdout)
	}
	if !strings.Contains(err.Error(), "err ***") || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("Error() = %q", err.Error())
	}
	for name, s := range map[string]string{"error": err.Error(), "verbose log": log.String()} {
		if strings.Contains(s, "topsecret") {
			t.Errorf("%s leaks the secret: %q", name, s)
		}
	}
	if !strings.HasPrefix(log.String(), "+ sh -c ") {
		t.Errorf("verbose log = %q", log.String())
	}
}

func TestRealSuccessDirEnvStdin(t *testing.T) {
	dir := t.TempDir()
	r := NewReal(&bytes.Buffer{}, false)
	res, err := r.Run(context.Background(), Cmd{
		Name:  "sh",
		Args:  []string{"-c", `pwd; echo "$OIS_TEST_VAR"; cat`},
		Dir:   dir,
		Env:   []string{"OIS_TEST_VAR=hello"},
		Stdin: strings.NewReader("from stdin"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	lines := strings.Split(string(res.Stdout), "\n")
	got, _ := filepath.EvalSymlinks(lines[0])
	if got != want || lines[1] != "hello" || lines[2] != "from stdin" {
		t.Errorf("stdout = %q", res.Stdout)
	}
}

func TestRealStderrTrimmed(t *testing.T) {
	r := NewReal(&bytes.Buffer{}, false)
	_, err := r.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "head -c 5000 /dev/zero | tr '\\0' x >&2; echo END >&2; exit 1"}})
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v", err)
	}
	if len(ee.Stderr) > maxStderr+len("…") || !strings.HasSuffix(ee.Stderr, "END") || !strings.HasPrefix(ee.Stderr, "…") {
		t.Errorf("stderr not trimmed to the tail: len %d", len(ee.Stderr))
	}
}

func TestRealMissingBinary(t *testing.T) {
	r := NewReal(&bytes.Buffer{}, false)
	for _, name := range []string{"ois-no-such-binary", "/no/such/ois-binary"} {
		_, err := r.Run(context.Background(), Cmd{Name: name})
		if err == nil || !strings.Contains(err.Error(), "not found on PATH") || !strings.Contains(err.Error(), "ois doctor") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestRealMissingDir(t *testing.T) {
	r := NewReal(&bytes.Buffer{}, false)
	_, err := r.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "true"}, Dir: "/no/such/dir"})
	if err == nil || !strings.Contains(err.Error(), "working directory") {
		t.Errorf("err = %v", err)
	}
}

func TestRealContextCancelled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := NewReal(&bytes.Buffer{}, false).Stream(ctx, Cmd{Name: "sleep", Args: []string{"5"}}, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want deadline exceeded", err)
	}
}

func TestRealStream(t *testing.T) {
	var out, errOut bytes.Buffer
	err := NewReal(&bytes.Buffer{}, false).Stream(context.Background(),
		Cmd{Name: "sh", Args: []string{"-c", "echo a; echo b >&2; exit 4"}}, &out, &errOut)
	var ee *ExitError
	if !errors.As(err, &ee) || ee.ExitCode != 4 {
		t.Fatalf("err = %v", err)
	}
	if out.String() != "a\n" || errOut.String() != "b\n" {
		t.Errorf("stdout %q stderr %q", out.String(), errOut.String())
	}
}

func TestDryRunNeverExecutes(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	var out bytes.Buffer
	d := NewDryRun(&out)
	c := Cmd{
		Name:    "sh",
		Args:    []string{"-c", "touch " + marker},
		Stdin:   strings.NewReader("password=hunter22"),
		Secrets: []string{"hunter22"},
	}
	res, err := d.Run(context.Background(), c)
	if err != nil || len(res.Stdout) != 0 || res.ExitCode != 0 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if err := d.Stream(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "touch " + marker}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("dry-run executed the command (stat err = %v)", err)
	}
	if !strings.Contains(out.String(), "+ sh -c") || !strings.Contains(out.String(), "(stdin: 17 bytes)") {
		t.Errorf("output = %q", out.String())
	}
	if strings.Contains(out.String(), "hunter22") || strings.Contains(out.String(), "password") {
		t.Errorf("dry-run printed stdin: %q", out.String())
	}
}

func TestFake(t *testing.T) {
	boom := errors.New("boom")
	f := (&Fake{}).
		On("kubectl get", Result{Stdout: []byte("generic")}, nil).
		On("kubectl get ns", Result{Stdout: []byte("specific")}, nil).
		On("helm", Result{}, boom)

	ctx := context.Background()
	res, _ := f.Run(ctx, Cmd{Name: "kubectl", Args: []string{"get", "ns", "storage"}})
	if string(res.Stdout) != "specific" {
		t.Errorf("later rule did not win: %q", res.Stdout)
	}
	res, _ = f.Run(ctx, Cmd{Name: "kubectl", Args: []string{"get", "pods"}})
	if string(res.Stdout) != "generic" {
		t.Errorf("prefix match failed: %q", res.Stdout)
	}
	if _, err := f.Run(ctx, Cmd{Name: "helm", Args: []string{"list"}}); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	res, err := f.Run(ctx, Cmd{Name: "minikube", Args: []string{"status"}})
	if err != nil || len(res.Stdout) != 0 {
		t.Errorf("unmatched call: %+v, %v", res, err)
	}
	var out bytes.Buffer
	if err := f.Stream(ctx, Cmd{Name: "kubectl", Args: []string{"get", "ns"}}, &out, nil); err != nil || out.String() != "specific" {
		t.Errorf("stream: %q, %v", out.String(), err)
	}
	f.Run(ctx, Cmd{Name: "kubectl", Args: []string{"create", "secret", "--from-literal=k=v1"}, Secrets: []string{"v1"}})

	want := []string{
		"kubectl get ns storage",
		"kubectl get pods",
		"helm list",
		"minikube status",
		"kubectl get ns",
		"kubectl create secret --from-literal=k=v1",
	}
	got := f.CommandLines()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("CommandLines() = %q", got)
	}
}
