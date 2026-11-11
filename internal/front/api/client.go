package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kanggu12/wsl-spawner/internal/engine/history"
)

// Runner runs one command line of wsl-spawner: what it prints goes to out as
// it is printed, its errors to errOut, and its exit code comes back. err is
// only for a command line that could not run at all.
type Runner func(ctx context.Context, args []string, out, errOut io.Writer) (code int, err error)

// Client asks wsl-spawner's commands, through a Runner, and reads their
// JSON. A command that ends with a code of its own, as check's 3, still
// gives its JSON: only one that printed none is an error.
type Client struct {
	Run Runner
	// Lang is passed to every command, so a command never sets a language
	// of its own beside the screen's.
	Lang string
}

// call runs args and decodes the JSON they print into v.
func (c Client) call(ctx context.Context, v any, args ...string) error {
	var out, errOut bytes.Buffer
	code, err := c.Run(ctx, c.withLang(args), &out, &errOut)
	if err != nil {
		return err
	}
	if derr := json.Unmarshal(out.Bytes(), v); derr != nil {
		return failed(args, code, errOut.String(), derr)
	}
	return nil
}

// withLang is args with the screen's language.
func (c Client) withLang(args []string) []string {
	if c.Lang == "" {
		return args
	}
	return append(append([]string{}, args...), "--lang", c.Lang)
}

// failed is the error of a command that printed no JSON: its own last
// words, which say what went wrong and what to do.
func failed(args []string, code int, errOut string, cause error) error {
	said := strings.TrimSpace(errOut)
	if said == "" {
		said = cause.Error()
	}
	// The command's own mark and the line after it, as one line.
	said = strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimPrefix(said, "✗ "), "→", "(")), " ")
	if strings.Contains(said, "(") && !strings.HasSuffix(said, ")") {
		said += ")"
	}
	return &CommandError{Args: args, Code: code, Said: said}
}

// CommandError is a command that ended without its JSON.
type CommandError struct {
	Args []string
	Code int
	// Said is what it said on its error output, in one line.
	Said string
}

func (e *CommandError) Error() string {
	if e.Said != "" {
		return e.Said
	}
	return "wsl-spawner " + strings.Join(e.Args, " ") + " ended with " + strconv.Itoa(e.Code)
}

// Usage reports whether the command was refused as asked wrong (code 2):
// a name WSL does not have, or one wsl-spawner leaves be.
func (e *CommandError) Usage() bool { return e.Code == 2 }

// Attention reports whether the command did what it was asked, but not as
// it should be (code 3): a hook installed that Windows has not read yet,
// for one. Said says how.
func (e *CommandError) Attention() bool { return e.Code == 3 }

// Do runs a command that prints no JSON, and says what it said when it
// ended with a code other than 0.
func (c Client) Do(ctx context.Context, args ...string) error {
	var out, errOut bytes.Buffer
	code, err := c.Run(ctx, c.withLang(args), &out, &errOut)
	if err != nil {
		return err
	}
	if code != 0 {
		return failed(args, code, errOut.String(), errors.New("exit status "+strconv.Itoa(code)))
	}
	return nil
}

// Settings reads the settings into v, the shape the hidden settings
// command prints.
func (c Client) Settings(ctx context.Context, v any) error { return c.call(ctx, v, "settings") }

// Status lists the distributions.
func (c Client) Status(ctx context.Context) (Status, error) {
	var s Status
	return s, c.call(ctx, &s, "status", "--json")
}

// Check looks everything over.
func (c Client) Check(ctx context.Context) (Check, error) {
	var ch Check
	return ch, c.call(ctx, &ch, "check", "--json")
}

// Hook says where the sign-out hook stands.
func (c Client) Hook(ctx context.Context) (Hook, error) {
	var h Hook
	return h, c.call(ctx, &h, "hook", "status", "--json")
}

// History lists the stops and starts, oldest first.
func (c Client) History(ctx context.Context) ([]history.Entry, error) {
	var h struct {
		Entries []history.Entry `json:"entries"`
	}
	return h.Entries, c.call(ctx, &h, "history", "--json")
}

// Start starts the distributions named.
func (c Client) Start(ctx context.Context, names []string) (StartResult, error) {
	var r StartResult
	return r, c.call(ctx, &r, append([]string{"start", "--json"}, names...)...)
}

// Set changes a distribution's choices; nil leaves one as it is.
func (c Client) Set(ctx context.Context, name string, managed, keep, logon, skip *bool) (SetResult, error) {
	args := []string{"set", name, "--yes", "--json"}
	for flag, v := range map[string]*bool{"--managed": managed, "--keep": keep, "--logon": logon, "--skip": skip} {
		if v != nil {
			args = append(args, flag+"="+strconv.FormatBool(*v))
		}
	}
	var r SetResult
	return r, c.call(ctx, &r, args...)
}

// Stop stops the distributions named, or every running one of wsl-spawner's
// when none is named, and WSL after when shutdown; on gets each step as it
// happens, in the goroutine that runs it.
func (c Client) Stop(ctx context.Context, names []string, shutdown bool, on func(StopEvent)) (StopResult, error) {
	args := []string{"stop", "--yes", "--json", "--events"}
	if shutdown {
		args = append(args, "--shutdown")
	}
	// The command refuses a stop that names nothing, as a person typing it
	// may have forgotten the names: none named here means every one.
	if len(names) == 0 {
		args = append(args, "--all")
	}
	args = append(args, names...)
	var res StopResult
	got := false
	lw := &lineWriter{fn: func(line []byte) {
		// Each line is an event, but the last, which is the results.
		var probe struct {
			Event  string `json:"event"`
			Schema int    `json:"schema"`
		}
		if json.Unmarshal(line, &probe) != nil {
			return
		}
		if probe.Event != "" {
			var e StopEvent
			if json.Unmarshal(line, &e) == nil {
				on(e)
			}
			return
		}
		if probe.Schema != 0 && json.Unmarshal(line, &res) == nil {
			got = true
		}
	}}
	var errOut bytes.Buffer
	code, err := c.Run(ctx, c.withLang(args), lw, &errOut)
	lw.flush()
	if err != nil {
		return res, err
	}
	if !got {
		return res, failed(args, code, errOut.String(), errors.New("no results"))
	}
	return res, nil
}

// lineWriter hands each whole line written to it to fn, as it comes.
type lineWriter struct {
	buf []byte
	fn  func([]byte)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := bytes.TrimRight(w.buf[:i], "\r")
		if len(line) > 0 {
			w.fn(append([]byte(nil), line...))
		}
		w.buf = w.buf[i+1:]
	}
}

// flush hands over a last line that came without its line break.
func (w *lineWriter) flush() {
	if line := bytes.TrimSpace(w.buf); len(line) > 0 {
		w.fn(line)
	}
	w.buf = nil
}

// Exe is a Runner that starts wsl-spawner.exe at path for each command
// line, in env: the screen in a WSL terminal asks the Windows program this
// way, and the Windows program asks itself, so both go one way.
func Exe(path string, env []string) Runner {
	return func(ctx context.Context, args []string, out, errOut io.Writer) (int, error) {
		return startExe(ctx, path, env, args, out, errOut)
	}
}

// errNoProgramInTests is what Exe gives in a test: a test starts no
// program; it hands in a Runner of its own.
var errNoProgramInTests = errors.New("a test starts no program; give it a Runner")

// describe names a command line in an error.
func describe(args []string) string { return fmt.Sprintf("wsl-spawner %s", strings.Join(args, " ")) }
