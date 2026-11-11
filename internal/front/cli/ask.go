package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// ask reads a yes or no; anything but y or yes is no. Ctrl+C while it waits
// ends the wait with the context's error: the signal is caught for the run,
// so it would not end the read on its own.
func ask(ctx context.Context, in io.Reader, out io.Writer, q string) (bool, error) {
	fmt.Fprint(out, q)
	// The line read, or the bug that stopped the read.
	type answer struct {
		line  string
		bug   any
		stack []byte
	}
	line := make(chan answer, 1)
	// The read cannot be stopped; when Ctrl+C comes first it is left behind,
	// and the program ends soon after.
	go func() {
		// A bug here says so as the command's error, as one anywhere else
		// does, rather than as a bare Go panic.
		defer func() {
			if r := recover(); r != nil {
				line <- answer{bug: r, stack: debug.Stack()}
			}
		}()
		s, _ := bufio.NewReader(in).ReadString('\n')
		line <- answer{line: s}
	}()
	select {
	case <-ctx.Done():
		// The prompt's line ends, so the error starts on a line of its own.
		fmt.Fprintln(out)
		return false, ctx.Err()
	case got := <-line:
		if got.bug != nil {
			// Where it was, above what to do about it.
			fmt.Fprintln(out)
			out.Write(got.stack)
			return false, hint(fmt.Errorf("stopped on a bug: %v", got.bug), "please report it with the lines above: "+IssuesURL)
		}
		a := strings.ToLower(strings.TrimSpace(got.line))
		return a == "y" || a == "yes", nil
	}
}

// needAnswer stops a command that would ask a question no one can answer:
// no terminal, none the launcher relays, and no --yes. what is what --yes
// does without the question.
func needAnswer(yes bool, what string) error {
	if yes || stdinIsTerminal() || relayed(0) {
		return nil
	}
	return hint(fmt.Errorf("%w: no terminal to ask in", errUsage), "add --yes to "+what+" without the question")
}

// counted is n of a thing, named for one or for several.
func counted(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}

// confirm asks q on stderr and returns nil for a yes. A no ends the command
// with the words not; Ctrl+C at the question ends it like a no.
func confirm(cmd *cobra.Command, q, not string) error {
	ok, err := ask(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), q)
	switch {
	case errors.Is(err, context.Canceled):
		return errors.New(not + ": cancelled")
	case err != nil:
		return err
	case !ok:
		return errors.New(not)
	}
	return nil
}

// sayDone prints what a command did. On a terminal its first line follows a
// check mark and the notes under it are muted; plain, it is the text as it
// is, for a script.
func sayDone(out io.Writer, text string) {
	l := lookFor(out)
	first, notes, _ := strings.Cut(text, "\n")
	fmt.Fprintln(out, l.done(first))
	if notes != "" {
		fmt.Fprintln(out, l.muted(notes))
	}
}
