// Package execx is the only way ois runs external commands.
//
// In dry-run mode nothing is executed and every read returns empty output, so callers
// must treat empty output as "not found / not ready" and keep going.
package execx

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Cmd describes one external command.
type Cmd struct {
	Name    string
	Args    []string
	Dir     string    // working directory; empty means the current one
	Env     []string  // KEY=VALUE pairs appended to os.Environ()
	Stdin   io.Reader // optional
	Secrets []string  // values replaced by *** wherever the command or its output is shown
}

// Result is the captured outcome of Run.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner runs external commands.
type Runner interface {
	// Run captures output. It returns a non-nil *ExitError when the command exits non-zero.
	Run(ctx context.Context, c Cmd) (Result, error)
	// Stream connects the command to the given writers and c.Stdin, for long-running or
	// interactive commands such as port-forward or logs -f.
	Stream(ctx context.Context, c Cmd, stdout, stderr io.Writer) error
}

// ExitError is returned when a command exits non-zero. All of its fields are redacted.
type ExitError struct {
	Cmd      string
	ExitCode int
	Stderr   string // trimmed to the last 2 KiB
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("%s: exit status %d", e.Cmd, e.ExitCode)
	if e.Stderr != "" {
		msg += ": " + e.Stderr
	}
	return msg
}

const maxStderr = 2 << 10

func newExitError(c Cmd, code int, stderr []byte) *ExitError {
	s := strings.TrimSpace(Redact(string(stderr), c.Secrets))
	if len(s) > maxStderr {
		s = s[len(s)-maxStderr:]
		for len(s) > 0 && !utf8.RuneStart(s[0]) {
			s = s[1:]
		}
		s = "…" + s
	}
	return &ExitError{Cmd: c.String(), ExitCode: code, Stderr: s}
}

var safeArg = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// String renders the command shell-quoted with secrets replaced by "***".
func (c Cmd) String() string {
	parts := make([]string, 0, len(c.Args)+1)
	for _, a := range append([]string{c.Name}, c.Args...) {
		parts = append(parts, quote(Redact(a, c.Secrets)))
	}
	return strings.Join(parts, " ")
}

func quote(s string) string {
	if safeArg.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (c Cmd) line() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}
