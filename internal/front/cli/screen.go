package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/app/demo"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/front/tui"
	"github.com/kanggu12/wsl-spawner/internal/machine/winapi"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// themes are the values --theme takes: "" follows the settings.
var themes = append([]string{""}, config.Themes...)

// screenFlags are the root command's flags for the screen.
type screenFlags struct {
	theme, snapshot, keys string
	demo, cliMode, setup  bool
	// at is how far into a stop the --keys began a --snapshot is drawn.
	at time.Duration
}

// addScreenFlags puts the screen's flags on the root command.
func addScreenFlags(root *cobra.Command, f *screenFlags) {
	root.Flags().BoolVar(&f.demo, "demo", false, "open the screen on a made-up PC; nothing real is read or changed")
	root.Flags().StringVar(&f.theme, "theme", "", "the `colors` of the screen: auto, dark or light (default: the settings, then ask the terminal)")
	root.Flags().StringVar(&f.snapshot, "snapshot", "", "print one frame WIDTHxHEIGHT of the demo and exit")
	root.Flags().StringVar(&f.keys, "keys", "", "keys to press before --snapshot prints, like \"down s enter\"")
	root.Flags().BoolVar(&f.cliMode, "cli", false, "with --snapshot, print the demo's overview instead of the screen")
	root.Flags().BoolVar(&f.setup, "setup", false, "with --snapshot, print the setup wizard instead of the screen")
	root.Flags().DurationVar(&f.at, "at", 0, "with --snapshot and a stop in --keys, the frame this far into the stop, like 9s")
	for _, name := range []string{"snapshot", "keys", "cli", "setup", "at"} {
		_ = root.Flags().MarkHidden(name)
	}
}

// size parses WIDTHxHEIGHT.
func size(s string) (int, int, error) {
	ws, hs, ok := strings.Cut(s, "x")
	w, err1 := strconv.Atoi(ws)
	h, err2 := strconv.Atoi(hs)
	if !ok || err1 != nil || err2 != nil || w < 40 || h < 12 {
		return 0, 0, fmt.Errorf("%w: --snapshot wants WIDTHxHEIGHT of at least 40x12, like 120x30", errUsage)
	}
	return w, h, nil
}

// bare is what a start without a command does: the demo's frame, the demo,
// or the screen or the overview, as the settings say.
func bare(cmd *cobra.Command, p *apps, f screenFlags) error {
	ctx, out := cmd.Context(), cmd.OutOrStdout()
	if !contains(themes, f.theme) {
		return fmt.Errorf("%w: --theme takes %s", errUsage, orList(config.Themes))
	}
	lang := string(i18n.Current())
	if f.snapshot != "" {
		w, h, err := size(f.snapshot)
		if err != nil {
			return err
		}
		a, _, done, err := demo.New(true)
		if err != nil {
			return err
		}
		defer done()
		a.Config.Language = lang
		if f.cliMode {
			return overview(ctx, a, out, look{on: true}, w)
		}
		c := api.Client{Run: InProcess(func() (*app.App, error) { return a, nil }), Lang: lang}
		if f.setup {
			fmt.Fprintln(out, tui.SetupSnapshot(c, w, h, f.theme != "light", tui.ParseKeys(f.keys)))
			return nil
		}
		fmt.Fprintln(out, tui.SnapshotAt(c, w, h, f.theme != "light", tui.ParseKeys(f.keys), demo.Now, f.at))
		return nil
	}
	if f.demo {
		a, _, done, err := demo.New(false)
		if err != nil {
			return err
		}
		defer done()
		c := api.Client{Run: InProcess(func() (*app.App, error) { return a, nil }), Lang: lang}
		return screenBug(tui.Run(ctx, c, tui.Options{Theme: f.theme}), cmd.ErrOrStderr())
	}
	// In WSL the settings are Windows': the program there reads them.
	if runtime.GOOS != "windows" {
		return screenFromWSL(cmd, f)
	}
	a, err := p.get()
	if err != nil {
		return err
	}
	// Without a terminal there is no screen to draw: the status, for a
	// script. CLI mode prints the overview instead of the screen.
	if !stdoutIsTerminal() && !relayed(1) {
		return status(ctx, a, out, false)
	}
	// The first start in a terminal asks how to work, once: with no
	// settings file, the wizard comes first; leaving it writes the
	// defaults, so it does not ask again.
	if !exists(a.ConfigPath) && (stdinIsTerminal() || relayed(0)) {
		c, err := screenClient()
		if err != nil {
			return err
		}
		if _, err := tui.RunSetup(ctx, c, tui.Options{Theme: f.theme}, true); err != nil {
			return screenBug(err, cmd.ErrOrStderr())
		}
		if c, err := config.Load(a.ConfigPath); err == nil {
			a.Config = c
		}
		if !cmd.Flags().Changed("lang") {
			i18n.Set(i18n.Choose(a.Config.Language, winapi.Korean()))
		}
	}
	if a.Config.Mode == "cli" {
		return overview(ctx, a, out, lookFor(out), widthOf(out))
	}
	return screen(cmd, a.Config.Theme, f.theme)
}

// contains reports whether vs holds v.
func contains(vs []string, v string) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}

// screen opens the screen on this PC: each of its commands is this program
// started again, as the screen in a WSL terminal starts wsl-spawner.exe.
func screen(cmd *cobra.Command, setting, flag string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	theme := flag
	if theme == "" {
		theme = setting
	}
	c := api.Client{Run: api.Exe(exe, os.Environ()), Lang: string(i18n.Current())}
	return screenBug(tui.Run(cmd.Context(), c, tui.Options{Theme: theme}), cmd.ErrOrStderr())
}

// screenFromWSL opens the screen in a WSL terminal, drawn here, each
// command asked of wsl-spawner.exe, which the launcher found.
func screenFromWSL(cmd *cobra.Command, f screenFlags) error {
	exe := os.Getenv("WSL_SPAWNER_EXE")
	if exe == "" {
		return wsl.ErrNotWindows
	}
	c := api.Client{Run: api.Exe(exe, os.Environ()), Lang: string(i18n.Current())}
	var st settingsOut
	if err := c.Settings(cmd.Context(), &st); err != nil {
		return err
	}
	// The first start asks how to work, once, as on Windows.
	if !st.Exists {
		if _, err := tui.RunSetup(cmd.Context(), c, tui.Options{Theme: f.theme}, true); err != nil {
			return screenBug(err, cmd.ErrOrStderr())
		}
		if err := c.Settings(cmd.Context(), &st); err != nil {
			return err
		}
	}
	// The language the settings choose, unless this command line said.
	if !cmd.Flags().Changed("lang") {
		i18n.Set(i18n.Choose(st.Language, st.WindowsKorean))
		c.Lang = string(i18n.Current())
	}
	if st.Mode == "cli" && !cmd.Flags().Changed("theme") {
		// CLI mode: the overview, printed by the Windows program here.
		_, err := c.Run(cmd.Context(), []string{"overview", "--lang", c.Lang}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		return err
	}
	theme := f.theme
	if theme == "" {
		theme = st.Theme
	}
	return screenBug(tui.Run(cmd.Context(), c, tui.Options{Theme: theme}), cmd.ErrOrStderr())
}

// settingsOut is what the hidden settings command prints: the settings,
// for the screen in WSL, which cannot read Windows' files.
type settingsOut struct {
	Schema        int      `json:"schema"`
	Language      string   `json:"language"`
	Theme         string   `json:"theme"`
	Mode          string   `json:"mode"`
	Resume        string   `json:"resume"`
	StopLimit     int      `json:"stop_limit"`
	Logon         []string `json:"logon"`
	Skip          []string `json:"skip"`
	Path          string   `json:"path"`
	Exists        bool     `json:"exists"`
	WindowsKorean bool     `json:"windows_korean"`
}

// settingsCmd prints the settings as JSON, for the screen in WSL.
func settingsCmd(p *apps) *cobra.Command {
	return &cobra.Command{
		Use:    "settings",
		Short:  "Print the settings as JSON",
		Args:   noArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := p.get()
			if err != nil {
				return err
			}
			c := a.Config
			return printJSON(cmd.OutOrStdout(), settingsOut{Schema: schema, Language: c.Language, Theme: c.Theme, Mode: c.Mode,
				Resume: c.Resume, StopLimit: c.StopLimit, Logon: nonNil(c.Logon), Skip: nonNil(c.Skip), Path: a.ConfigPath,
				Exists: exists(a.ConfigPath), WindowsKorean: winapi.Korean()})
		},
	}
}

// overviewCmd prints the overview, for CLI mode in WSL.
func overviewCmd(p *apps) *cobra.Command {
	return &cobra.Command{
		Use:    "overview",
		Short:  "Print the overview CLI mode prints",
		Args:   noArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := p.get()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			return overview(cmd.Context(), a, out, lookFor(out), widthOf(out))
		},
	}
}

// tuiCmd opens the screen, whatever the mode.
func tuiCmd(p *apps) *cobra.Command {
	var theme string
	c := &cobra.Command{
		Use:   "tui",
		Short: "Open the screen, whatever the mode the settings choose",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !contains(themes, theme) {
				return fmt.Errorf("%w: --theme takes %s", errUsage, orList(config.Themes))
			}
			if runtime.GOOS != "windows" {
				return screenFromWSL(cmd, screenFlags{theme: theme})
			}
			a, err := p.get()
			if err != nil {
				return err
			}
			return screen(cmd, a.Config.Theme, theme)
		},
	}
	c.Flags().StringVar(&theme, "theme", "", "the `colors` of the screen: auto, dark or light")
	return c
}

// screenBug says a screen that stopped on a bug did so, in the program's
// words, with where to report it, under where it happened.
func screenBug(err error, errOut io.Writer) error {
	var bug *tui.BugError
	switch {
	case errors.As(err, &bug):
		errOut.Write(bug.Stack)
		return hint(fmt.Errorf("the screen stopped on a bug: %v", bug.Value), "please report it with the lines above: "+IssuesURL)
	case errors.Is(err, tea.ErrProgramPanic):
		return hint(errors.New("the screen stopped on a bug"), "please report it with the lines above: "+IssuesURL)
	case errors.Is(err, context.Canceled):
		return nil
	}
	return err
}
