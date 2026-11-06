package execx

import (
	"context"
	"io"
	"strings"
	"sync"
)

// Fake records calls and replies from rules matched by command-line prefix.
type Fake struct {
	Calls []Cmd
	rules []fakeRule
	mu    sync.Mutex
}

type fakeRule struct {
	prefix string
	res    Result
	err    error
}

var _ Runner = (*Fake)(nil)

// On registers a reply for any command whose unredacted "name arg1 arg2 ..." starts with
// prefix. Later rules win over earlier ones.
func (f *Fake) On(prefix string, res Result, err error) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules = append(f.rules, fakeRule{prefix: prefix, res: res, err: err})
	return f
}

func (f *Fake) Run(_ context.Context, c Cmd) (Result, error) {
	return f.reply(c)
}

// Stream writes the matched reply's stdout and stderr to the given writers.
func (f *Fake) Stream(_ context.Context, c Cmd, stdout, stderr io.Writer) error {
	res, err := f.reply(c)
	if stdout != nil {
		_, _ = stdout.Write(res.Stdout)
	}
	if stderr != nil {
		_, _ = stderr.Write(res.Stderr)
	}
	return err
}

// CommandLines returns each recorded call as "name arg1 arg2 ..." for assertions.
func (f *Fake) CommandLines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	lines := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		lines[i] = c.line()
	}
	return lines
}

func (f *Fake) reply(c Cmd) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, c)
	line := c.line()
	for i := len(f.rules) - 1; i >= 0; i-- {
		if strings.HasPrefix(line, f.rules[i].prefix) {
			return f.rules[i].res, f.rules[i].err
		}
	}
	return Result{}, nil
}
