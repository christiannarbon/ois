// Package cli builds the ois command tree.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
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

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	return run(NewRootCommand(os.Stdout, os.Stderr), os.Stderr)
}

func run(root *cobra.Command, stderr io.Writer) int {
	err := root.Execute()
	if err == nil {
		return ExitOK
	}
	var ec *ExitCodeError
	if errors.As(err, &ec) {
		if ec.Err != nil {
			fmt.Fprintln(stderr, "error:", ec.Err)
		}
		return ec.Code
	}
	fmt.Fprintln(stderr, "error:", err)
	return ExitError
}
