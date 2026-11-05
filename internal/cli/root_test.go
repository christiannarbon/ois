package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
