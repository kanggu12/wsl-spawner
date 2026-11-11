// Package app joins the parts every front needs: the settings, WSL, and the
// folder of the program's own files.
package app

import (
	"path/filepath"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/machine/winapi"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// App is the program as one run sees it.
type App struct {
	// Config is the settings, as the file says or by default.
	Config config.Config
	// ConfigPath is where the settings file is, or would be written.
	ConfigPath string
	// WSL is what the program asks of WSL.
	WSL wsl.System
	// Dir is the folder of the program's own files: the history and the
	// crash files.
	Dir string
	// PC is what the program reaches on Windows besides WSL.
	PC PC
	// Clock is the time now as this run reads it; nil is this PC's own.
	// The demo gives its made-up PC's, so what it shows and keeps agrees
	// with the times it was made with.
	Clock func() time.Time
}

// Now is the time on the App's clock.
func (a *App) Now() time.Time {
	// No clock given: this PC's.
	if a.Clock != nil {
		return a.Clock()
	}
	return time.Now()
}

// New reads the settings and sets up the parts. A settings file that does
// not load is an error: running with defaults the person did not choose
// would be a surprise. The App comes with that error all the same, the
// defaults standing in and the path still naming the file, for the commands
// that read no settings: the sign-out hook's, and uninstall, which may be
// removing the very file that does not load.
func New() (*App, error) {
	// Where the settings are, whether or not they load.
	path := config.Path(winapi.AppData())
	c, err := config.Load(path)
	if err != nil {
		// The defaults in place of what did not load: only a command that
		// asks for the App whatever the file holds goes on with them.
		c = config.Default()
	}
	return &App{Config: c, ConfigPath: path, WSL: wsl.New(), Dir: StateDir(), PC: RealPC()}, err
}

// StateDir is the folder of the program's own files, under
// %LOCALAPPDATA%: they are this PC's, not settings to carry to another.
func StateDir() string { return filepath.Join(winapi.LocalAppData(), "wsl-spawner") }
