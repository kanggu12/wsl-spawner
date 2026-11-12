// Package cli is wsl-spawner's command line: the screen or the overview when
// started bare, and commands for scripts and for a look without the screen.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/kanggu12/wsl-spawner/internal/front/api"

	"github.com/spf13/cobra"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/machine/winapi"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
	"github.com/kanggu12/wsl-spawner/internal/version"
)

// Exit codes: 0 done, 1 failed, 2 wrong usage, 3 done but a distribution
// did not get its services' time (cut off, or a step failed): a script must
// not go on as after a clean stop.
const (
	exitOK        = 0
	exitFail      = 1
	exitUsage     = 2
	exitAttention = 3
)

// attention is an end a script must tell from a clean one: a stop that
// was cut off or had a step fail.
type attention struct{ error }

// Unwrap gives the failure itself.
func (a attention) Unwrap() error { return a.error }

// apps makes the program's App once per command line, on first use: the
// settings are read once, and tests hand in an App of their own. make may
// return an App and an error together: the App with the defaults, beside a
// settings file that does not load (app.New).
type apps struct {
	make func() (*app.App, error)
	once sync.Once
	a    *app.App
	err  error
}

// load makes the App, once: the error of a settings file that does not load
// gets what to do about it.
func (p *apps) load() {
	p.a, p.err = p.make()
	if p.err != nil {
		p.err = hint(p.err, settingsHint(p.err))
	}
}

// get returns the App, made on the first call. Making it fails only on a
// settings file that does not load, which says what to do about it: a
// command that reads the settings does not go on with defaults the person
// did not choose.
func (p *apps) get() (*app.App, error) {
	p.once.Do(p.load)
	if p.err != nil {
		// No App beside the error: nothing goes on with the defaults.
		return nil, p.err
	}
	return p.a, nil
}

// anyway returns the App whatever the settings file holds, for a command
// that reads none of it: the sign-out hook's, whose copy started as an
// administrator must always write how it went, and uninstall, which may be
// removing the file that does not load. The defaults stand in for such a
// file. It fails only when no App could be made at all.
func (p *apps) anyway() (*app.App, error) {
	p.once.Do(p.load)
	if p.a == nil {
		// Nothing to go on with: the error says why.
		return nil, p.err
	}
	return p.a, nil
}

// settingsErr is why the settings file did not load, nil when it did, for a
// command that went on with the defaults through anyway.
func (p *apps) settingsErr() error {
	p.once.Do(p.load)
	return p.err
}

// settingsHint is what to do about a settings file that does not load: the
// setup command that puts the value right, where one does, or else to fix
// the file by hand.
func settingsHint(err error) string {
	var ke *config.KeyError
	if errors.As(err, &ke) {
		switch ke.Key {
		case "language":
			return "wsl-spawner setup --language auto puts it right"
		case "theme":
			return "wsl-spawner setup --theme auto puts it right"
		case "mode":
			return "wsl-spawner setup --mode tui puts it right"
		case "resume":
			return "wsl-spawner setup --resume running puts it right"
		case "stop_limit":
			return "wsl-spawner setup --stop-limit 300 puts it right"
		}
	}
	return "fix the file, or move it away to start from the defaults"
}

// errUsage marks an error in how the command was called.
var errUsage = errors.New("usage")

// Execute runs the command line and returns the exit code. The console's
// modes, as colors changed them, are put back by the caller, after its
// last words (winapi.RestoreConsole).
func Execute() int { return run(os.Args[1:], os.Stdout, os.Stderr) }

// run runs one command line on this PC, writing to out and errOut.
func run(args []string, out, errOut io.Writer) int { return runWith(args, out, errOut, app.New) }

// runWith runs one command line with the App newApp makes; Ctrl+C stops it.
func runWith(args []string, out, errOut io.Writer, newApp func() (*app.App, error)) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return runIn(ctx, args, out, errOut, newApp)
}

// InProcess is a Runner that runs each command line in this process, with
// the App newApp makes: for the demo, on a made-up PC, and for tests.
func InProcess(newApp func() (*app.App, error)) api.Runner {
	return func(ctx context.Context, args []string, out, errOut io.Writer) (int, error) {
		return runIn(ctx, args, out, errOut, newApp), nil
	}
}

// runIn runs one command line in ctx.
func runIn(ctx context.Context, args []string, out, errOut io.Writer, newApp func() (*app.App, error)) int {
	root := newRoot(&apps{make: newApp})
	root.SetContext(ctx)
	root.SetArgs(args)
	root.SetOut(out)
	root.SetErr(errOut)
	err := root.Execute()
	if err != nil {
		printErr(errOut, withHint(usageHint(root, args, err)))
	}
	return exitFor(err)
}

// withHint adds what to do to an error that comes without, from below the
// command line.
func withHint(err error) error {
	var h hinted
	if errors.As(err, &h) {
		return err
	}
	if errors.Is(err, wsl.ErrNotWindows) {
		return hint(err, "run it on Windows, or inside WSL with wsl-spawner.exe installed on Windows")
	}
	if errors.Is(err, wsl.ErrNoAnswer) {
		return hint(err, noAnswerFix)
	}
	if errors.Is(err, wsl.ErrNoDistro) {
		return hint(err, "wsl-spawner status lists the distributions")
	}
	return err
}

// noAnswerFix is what to do when WSL did not answer in time.
const noAnswerFix = "WSL may still be starting or updating: try again in a minute"

// usageHint adds what to do to cobra's own errors about how a command was
// called: the command meant, for a word that is none, or the command's help.
func usageHint(root *cobra.Command, args []string, err error) error {
	var h hinted
	if errors.As(err, &h) || !(cobraUsage(err) || errors.Is(err, errUsage)) {
		return err
	}
	first, _, _ := strings.Cut(err.Error(), "\n")
	c, rest, ferr := root.Find(args)
	if ferr != nil || c == nil {
		c = root
	}
	if word, ok := strings.CutPrefix(first, "unknown command "); ok {
		// The word cobra did not know, as it quoted it.
		w, _, _ := strings.Cut(strings.Trim(word, `"`), `"`)
		if len(rest) > 0 && w == "" {
			w = rest[0]
		}
		// Cobra sets the distance only on its own path; two letters off
		// is its default too.
		c.SuggestionsMinimumDistance = 2
		if sug := c.SuggestionsFor(w); len(sug) > 0 {
			return hint(fmt.Errorf("%w: %s", errUsage, first), "did you mean: "+c.CommandPath()+" "+sug[0])
		}
		return hint(fmt.Errorf("%w: %s", errUsage, first), c.CommandPath()+" --help lists the commands")
	}
	return hint(fmt.Errorf("%w: %s", errUsage, trimUsage(first)), c.CommandPath()+" --help")
}

// trimUsage drops the "usage: " that marks a mistake in how a command was
// called: the exit code says it, the words need not.
func trimUsage(s string) string { return strings.TrimPrefix(s, "usage: ") }

// exitFor is the exit code for how a command ended: 2 for a mistake in how
// it was called, a distribution name WSL does not have included, 3 for an
// end a script must tell apart, 1 for a failure, 0 for none.
func exitFor(err error) int {
	var at attention
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &at):
		return exitAttention
	case errors.Is(err, errUsage) || cobraUsage(err) || errors.Is(err, wsl.ErrNoDistro):
		return exitUsage
	}
	return exitFail
}

// newRoot builds every command; p makes their App.
func newRoot(p *apps) *cobra.Command {
	var lang string
	var sf screenFlags
	root := &cobra.Command{
		Use:   "wsl-spawner",
		Short: "Give WSL's services and containers their time to stop when Windows shuts down, and bring them back",
		Long: "wsl-spawner stops your WSL distributions the slow way: each service and\n" +
			"container gets the time it asks for, and what was running comes back when the\n" +
			"distribution starts again. A sign-out hook does the same when Windows shuts down\n" +
			"or restarts; distributions you choose start at sign-in and keep running.\n\n" +
			"Started without a command in a terminal it opens the screen, or in CLI mode\n" +
			"prints the distributions and the commands to use. Every command works in both.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// The language: --lang, then the settings, then Windows.
			setting := lang
			if setting == "" {
				if a, err := p.get(); err == nil {
					setting = a.Config.Language
				}
			}
			if setting != "" && setting != "auto" && setting != "en" && setting != "ko" {
				return fmt.Errorf("%w: --lang takes auto, en or ko", errUsage)
			}
			i18n.Set(i18n.Choose(setting, winapi.Korean()))
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return bare(cmd, p, sf) },
	}
	addScreenFlags(root, &sf)
	root.PersistentFlags().StringVar(&lang, "lang", "", "the `language` of the screen, setup and the overview: auto, en or ko; the other commands print English (default: the settings, then WSL_SPAWNER_LANG, then Windows)")
	root.AddCommand(statusCmd(p), stopCmd(p), startCmd(p), setCmd(p), checkCmd(p), historyCmd(p), hookCmd(p), setupCmd(p), tuiCmd(p), uninstallCmd(p),
		logonCmd(p), logoffCmd(p), settingsCmd(p), overviewCmd(p), versionCmd())
	// Shell completion is cobra's own: it still works, but the help lists
	// only what this program does.
	root.CompletionOptions.HiddenDefaultCmd = true
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return fmt.Errorf("%w: %w", errUsage, err) })
	root.SetHelpCommand(helpCmd())
	// Every help fits an 80-column console: the commands' lines and the
	// flags wrap under their words, as cobra does not do by itself.
	cobra.AddTemplateFunc("wrapShort", wrapShort)
	t := root.UsageTemplate()
	t = strings.ReplaceAll(t, "{{rpad .Name .NamePadding }} {{.Short}}", "{{rpad .Name .NamePadding }} {{wrapShort .Short .NamePadding}}")
	t = strings.ReplaceAll(t, ".LocalFlags.FlagUsages", "(.LocalFlags.FlagUsagesWrapped 80)")
	t = strings.ReplaceAll(t, ".InheritedFlags.FlagUsages", "(.InheritedFlags.FlagUsagesWrapped 80)")
	root.SetUsageTemplate(t)
	// --lang changes nothing version prints: its help leaves it out.
	for _, c := range root.Commands() {
		if c.Name() == "version" {
			withoutGlobalFlags(c)
		}
	}
	return root
}

// helpCmd is help, and help NAME, NAME's help. A name that is no command is
// a mistake in the command line, as anywhere else in it, said with the
// command meant, not answered with the program's help and exit 0.
func helpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		RunE: func(c *cobra.Command, args []string) error {
			cmd, rest, err := c.Root().Find(args)
			if err == nil && len(rest) == 0 {
				return cmd.Help()
			}
			mistake := fmt.Errorf("%w: no command %q", errUsage, strings.Join(args, " "))
			parent, word := c.Root(), args[0]
			if err == nil {
				parent, word = cmd, rest[0]
			}
			// The command meant, as for a mistyped one: two letters off is
			// cobra's own distance.
			parent.SuggestionsMinimumDistance = 2
			if sug := parent.SuggestionsFor(word); len(sug) > 0 {
				path := strings.TrimSpace(strings.TrimPrefix(parent.CommandPath(), c.Root().Name()) + " " + sug[0])
				return hint(mistake, "did you mean: "+c.Root().Name()+" help "+path)
			}
			return hint(mistake, c.Root().Name()+" --help lists the commands")
		},
		// The commands, for shell completion, as cobra's own help offers them.
		ValidArgsFunction: func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			cmd, _, err := c.Root().Find(args)
			if err != nil || cmd == nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			var names []string
			for _, sub := range cmd.Commands() {
				if sub.IsAvailableCommand() && strings.HasPrefix(sub.Name(), toComplete) {
					names = append(names, sub.Name()+"\t"+sub.Short)
				}
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
	}
}

// wrapShort wraps a command's short line in a help's list of commands to
// 80 columns: its later lines start under its first, after the name and its
// padding.
func wrapShort(s string, pad int) string {
	lead := 2 + pad + 1
	var lines []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && lead+len([]rune(line))+1+len([]rune(w)) > 80 {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n"+strings.Repeat(" ", lead))
}

// withoutGlobalFlags leaves the flags every command takes out of cmd's help,
// and its commands'.
func withoutGlobalFlags(cmd *cobra.Command) {
	t := cmd.UsageTemplate()
	start := strings.Index(t, "{{if .HasAvailableInheritedFlags}}")
	if start < 0 {
		return
	}
	end := strings.Index(t[start:], "{{end}}")
	if end < 0 {
		return
	}
	cmd.SetUsageTemplate(t[:start] + t[start+end+len("{{end}}"):])
}

// cobraUsage reports whether an error is cobra's own about how the command
// was called: an unknown command, or the wrong number of arguments. Cobra
// returns these as plain errors, so they are told apart by their words.
func cobraUsage(err error) bool {
	msg := err.Error()
	for _, p := range []string{"unknown command", "accepts ", "requires at least", "requires at most", "unknown flag", "unknown shorthand"} {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	return false
}

// noArgs refuses arguments to a command that takes none. Cobra's own words
// for it, "unknown command", point at a missing command instead.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%w: %s takes no arguments, got %q", errUsage, cmd.Name(), args[0])
	}
	return nil
}

// schema is the version of the JSON the commands print: api's.
const schema = api.Schema

// printJSON writes v as indented JSON.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// versionCmd prints the build.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  noArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "wsl-spawner", version.String())
		},
	}
}
