package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
)

type real struct {
	log     io.Writer
	verbose bool
}

// NewReal returns a Runner that executes commands. With verbose, each command is written
// to log before it runs.
func NewReal(log io.Writer, verbose bool) Runner {
	return &real{log: log, verbose: verbose}
}

func (r *real) Run(ctx context.Context, c Cmd) (Result, error) {
	var stdout, stderr bytes.Buffer
	err := r.exec(ctx, c, &stdout, &stderr)
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var ee *ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode
		return res, newExitError(c, ee.ExitCode, res.Stderr)
	}
	return res, err
}

func (r *real) Stream(ctx context.Context, c Cmd, stdout, stderr io.Writer) error {
	return r.exec(ctx, c, stdout, stderr)
}

func (r *real) exec(ctx context.Context, c Cmd, stdout, stderr io.Writer) error {
	if r.verbose {
		fmt.Fprintln(r.log, "+", c.String())
	}
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	cmd.Stdin = c.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	var pe *fs.PathError
	if errors.As(err, &pe) && pe.Op == "chdir" {
		return fmt.Errorf("%s: working directory %s does not exist", c.String(), c.Dir)
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s not found on PATH — run \"ois doctor\"", c.Name)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", c.String(), ctxErr)
	}
	var xe *exec.ExitError
	if errors.As(err, &xe) {
		return newExitError(c, xe.ExitCode(), nil)
	}
	return fmt.Errorf("%s: %v", c.String(), err)
}
