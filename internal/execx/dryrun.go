package execx

import (
	"context"
	"fmt"
	"io"
)

type dryRun struct{ out io.Writer }

// NewDryRun returns a Runner that prints each command to out instead of running it.
func NewDryRun(out io.Writer) Runner { return &dryRun{out: out} }

func (d *dryRun) Run(_ context.Context, c Cmd) (Result, error) {
	d.print(c)
	return Result{}, nil
}

func (d *dryRun) Stream(_ context.Context, c Cmd, _, _ io.Writer) error {
	d.print(c)
	return nil
}

func (d *dryRun) print(c Cmd) {
	fmt.Fprintln(d.out, "+", c.String())
	if c.Stdin != nil {
		n, _ := io.Copy(io.Discard, c.Stdin)
		fmt.Fprintf(d.out, "  (stdin: %d bytes)\n", n)
	}
}
