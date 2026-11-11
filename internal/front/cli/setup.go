package cli

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/front/tui"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// setupCmd opens the setup wizard, or with flags writes those settings.
func setupCmd(p *apps) *cobra.Command {
	var language, theme, mode, resume string
	var stopLimit int
	c := &cobra.Command{
		Use:   "setup",
		Short: "Choose how wsl-spawner works: in a wizard, or with flags",
		Long: "setup opens a wizard, page by page: the language, the colors, the screen or the\n" +
			"overview, the distributions to set wsl-spawner up in, keep running and start at\n" +
			"sign-in, and the sign-out hook. With flags, it writes those settings and opens\n" +
			"nothing. The file keeps its notes and any setting written by hand.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			values := map[string]string{}
			for flag, key := range map[string]string{"language": "language", "theme": "theme", "mode": "mode", "resume": "resume"} {
				if cmd.Flags().Changed(flag) {
					v, _ := cmd.Flags().GetString(flag)
					values[key] = config.Quote(v)
				}
			}
			if cmd.Flags().Changed("stop-limit") {
				values["stop_limit"] = strconv.Itoa(stopLimit)
			}
			if len(values) > 0 {
				return writeSettings(cmd, p, values)
			}
			if !stdinIsTerminal() && !relayed(0) {
				return hint(fmt.Errorf("%w: setup's wizard needs a terminal", errUsage), "wsl-spawner setup --help lists the flags that set it without one")
			}
			c, err := screenClient()
			if err != nil {
				return err
			}
			_, err = tui.RunSetup(cmd.Context(), c, tui.Options{}, false)
			return screenBug(err, cmd.ErrOrStderr())
		},
	}
	c.Flags().StringVar(&language, "language", "", "the `language`: auto, en or ko")
	c.Flags().StringVar(&theme, "theme", "", "the `colors`: auto, dark or light")
	c.Flags().StringVar(&mode, "mode", "", "the `mode` a bare start opens in: tui, the screen, or cli, the overview")
	c.Flags().StringVar(&resume, "resume", "", "`what` wsl-spawner brings back at a start: running, policy or off")
	c.Flags().IntVar(&stopLimit, "stop-limit", 0, fmt.Sprintf("the `seconds` a distribution's steps may take, %d to %d", config.MinStopLimit, config.MaxStopLimit))
	return c
}

// writeSettings writes values into the settings file and says where.
func writeSettings(cmd *cobra.Command, p *apps, values map[string]string) error {
	a, err := p.get()
	if err != nil {
		// A file that does not load is what setup puts right: the path is
		// where the settings are.
		var ke *config.KeyError
		if !errors.As(err, &ke) {
			return err
		}
		a = &app.App{ConfigPath: ke.Path}
	}
	if err := config.SetKeys(a.ConfigPath, values); err != nil {
		var inv *config.InvalidError
		if errors.As(err, &inv) {
			// A value a flag gave is the flag's mistake, not the file's:
			// said by the flag, as nothing was written.
			var ke *config.KeyError
			if errors.As(inv.Err, &ke) && values[ke.Key] != "" && strings.HasPrefix(ke.Msg, ke.Key+" ") {
				return fmt.Errorf("%w: --%s%s", errUsage, strings.ReplaceAll(ke.Key, "_", "-"), strings.TrimPrefix(ke.Msg, ke.Key))
			}
			return fmt.Errorf("%w: %w", errUsage, inv.Err)
		}
		return err
	}
	sayDone(cmd.OutOrStdout(), "saved: "+a.ConfigPath)
	return nil
}

// screenClient is the client the screen and the wizard ask through: this
// program started again on Windows, wsl-spawner.exe from a WSL terminal,
// which the launcher found.
func screenClient() (api.Client, error) {
	lang := string(i18n.Current())
	if runtime.GOOS == "windows" {
		exe, err := os.Executable()
		if err != nil {
			return api.Client{}, err
		}
		return api.Client{Run: api.Exe(exe, os.Environ()), Lang: lang}, nil
	}
	exe := os.Getenv("WSL_SPAWNER_EXE")
	if exe == "" {
		return api.Client{}, wsl.ErrNotWindows
	}
	return api.Client{Run: api.Exe(exe, os.Environ()), Lang: lang}, nil
}
