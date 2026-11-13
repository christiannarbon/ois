// Package cli builds the ois command tree.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/christiannarbon/ois/internal/cluster"
	"github.com/christiannarbon/ois/internal/execx"
	"github.com/christiannarbon/ois/internal/project"
	"github.com/christiannarbon/ois/internal/spec"
)

// GlobalOptions are the flags shared by every command.
type GlobalOptions struct {
	File    string // path to ois.yaml; empty means search upwards from the working directory
	DryRun  bool   // print external commands instead of running them
	Verbose bool   // log every external command before running it
	Profile string // deployment profile from ois.yaml profiles:
	Yes     bool   // skip confirmation prompts
	Stdout  io.Writer
	Stderr  io.Writer
}

// Runner returns the execx.Runner selected by the global flags.
func (g *GlobalOptions) Runner() execx.Runner {
	if g.DryRun {
		return execx.NewDryRun(g.Stderr)
	}
	return execx.NewReal(g.Stderr, g.Verbose)
}

// SpecPath resolves the ois.yaml this invocation refers to.
func (g *GlobalOptions) SpecPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return project.Resolve(g.File, cwd)
}

// LoadSpec resolves, loads and validates the project spec.
func (g *GlobalOptions) LoadSpec() (*spec.Spec, error) {
	path, err := g.SpecPath()
	if err != nil {
		return nil, err
	}
	return spec.LoadAndValidate(path)
}

// Resolve loads, validates and resolves the spec for --profile.
func (g *GlobalOptions) Resolve() (*spec.Resolved, error) {
	s, err := g.LoadSpec()
	if err != nil {
		return nil, err
	}
	return s.Resolve(g.Profile)
}

// Target resolves the spec for --profile and guards that profile's cluster context.
func (g *GlobalOptions) Target(ctx context.Context) (*spec.Resolved, cluster.Target, error) {
	r, err := g.Resolve()
	if err != nil {
		return nil, cluster.Target{}, err
	}
	t, err := cluster.RequireContext(ctx, g.Runner(), r.Project, r.Profile)
	if err != nil {
		return nil, cluster.Target{}, err
	}
	return r, t, nil
}

// NewRootCommand builds the full command tree. Tests call it with buffers.
func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	g := &GlobalOptions{Stdout: stdout, Stderr: stderr}
	root := &cobra.Command{
		Use:           "ois",
		Short:         "A config-driven lakehouse playground on local Kubernetes",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	pf := root.PersistentFlags()
	pf.StringVarP(&g.File, "file", "f", "", "path to ois.yaml (default: search upwards from the current directory)")
	pf.BoolVar(&g.DryRun, "dry-run", false, "print external commands instead of running them")
	pf.BoolVarP(&g.Verbose, "verbose", "v", false, "log every external command")
	pf.StringVar(&g.Profile, "profile", "dev", "deployment profile from ois.yaml (profiles:)")
	pf.BoolVar(&g.Yes, "yes", false, "answer yes to confirmation prompts (for CI)")
	root.AddCommand(
		newVersionCommand(g),
		newValidateCommand(g),
	)
	return root
}

// Exit codes are stable; CI scripts depend on them.
const (
	ExitOK        = 0
	ExitError     = 1
	ExitChanges   = 2 // ois plan found changes
	ExitPreflight = 3 // a preflight check failed
)

// ExitCodeError lets a command choose its exit code. A nil Err exits silently.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit code %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitCodeError) Unwrap() error { return e.Err }

// errSilent exits 1 without printing; the command already reported the problem.
var errSilent = errors.New("error already reported")

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	return run(NewRootCommand(os.Stdout, os.Stderr), os.Stderr)
}

func run(root *cobra.Command, stderr io.Writer) int {
	err := root.Execute()
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, errSilent) {
		return ExitError
	}
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		if ec.Err != nil {
			writeError(stderr, ec.Err)
		}
		return ec.Code
	}
	writeError(stderr, err)
	return ExitError
}

// writeError prints spec problems as-is, one "ois.yaml:<line>: ..." per line, so every
// command reports them the same way; anything else gets an "error:" prefix.
func writeError(w io.Writer, err error) {
	var de *spec.DecodeError
	var ve *spec.ValidationError
	if errors.As(err, &de) || errors.As(err, &ve) {
		fmt.Fprintln(w, err)
		return
	}
	fmt.Fprintln(w, "error:", err)
}
