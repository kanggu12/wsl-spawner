package cli

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"
)

// What the commands ask of the terminal: tests replace them.
var (
	// stdinIsTerminal reports whether a person types into stdin.
	stdinIsTerminal = func() bool { return term.IsTerminal(os.Stdin.Fd()) }
	// stdoutIsTerminal reports whether stdout is a terminal.
	stdoutIsTerminal = func() bool { return term.IsTerminal(os.Stdout.Fd()) }
	// relayed reports whether fd (0 stdin, 1 stdout, 2 stderr) reaches a
	// terminal through WSL's launcher, which says so in WSL_SPAWNER_TTY:
	// started from WSL, this program's ends are pipes, a person at the
	// other end of them all the same.
	relayed = func(fd int) bool { return strings.Contains(os.Getenv("WSL_SPAWNER_TTY"), strconv.Itoa(fd)) }
)

// widthOf is the width of the terminal w writes to, 100 when it is none. A
// terminal WSL's launcher relays says its width itself.
func widthOf(w io.Writer) int {
	if f, ok := w.(*os.File); ok {
		if tw, _, err := term.GetSize(f.Fd()); err == nil && tw > 0 {
			return tw
		}
		if n, err := strconv.Atoi(os.Getenv("WSL_SPAWNER_COLUMNS")); relayedFile(f) && err == nil && n > 0 {
			return n
		}
	}
	return 100
}

// exists reports whether a file is there.
func exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// orList joins values as English lists them: "tui or cli", "auto, en or ko".
func orList(vs []string) string { return listWith(vs, " or ") }

// andList joins values as English lists them: "A and B", "A, B and C".
func andList(vs []string) string { return listWith(vs, " and ") }

// listWith joins values with commas, and last before the last one.
func listWith(vs []string, last string) string {
	if len(vs) < 2 {
		return strings.Join(vs, "")
	}
	return strings.Join(vs[:len(vs)-1], ", ") + last + vs[len(vs)-1]
}
