package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// errNotConfirmed is returned when the user answers anything but yes.
var errNotConfirmed = errors.New("aborted")

// isTerminal reports whether in is an interactive terminal. Tests replace it.
var isTerminal = defaultIsTerminal

func defaultIsTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	// /dev/null is a character device too, and CI often redirects stdin from it.
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(fi, null)
}

// confirm asks a yes/no question. With g.Yes it returns nil without asking. When in is
// not a terminal and g.Yes is false it refuses. On a terminal it prints prompt + " [y/N] "
// and accepts only "y" or "yes".
func confirm(g *GlobalOptions, in io.Reader, prompt, action string) error {
	if g.Yes {
		return nil
	}
	if !isTerminal(in) {
		return fmt.Errorf("refusing to %s without --yes when not interactive", action)
	}
	fmt.Fprintf(g.Stderr, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return errNotConfirmed
}
