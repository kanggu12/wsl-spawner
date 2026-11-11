// Package config reads the settings file, %APPDATA%\wsl-spawner\config.toml.
// Every setting has a default, so the file is optional.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is every setting.
type Config struct {
	// Language is "auto" (follow Windows), "en" or "ko".
	Language string `toml:"language"`
	// Theme is "auto" (ask the terminal), "dark" or "light".
	Theme string `toml:"theme"`
	// Mode is what wsl-spawner opens in a terminal when started bare: "tui",
	// the screen, or "cli", the distributions as a table with the commands
	// to use. Every command works in both.
	Mode string `toml:"mode"`
	// Skip lists distributions, by name, that wsl-spawner leaves to WSL: it
	// neither stops nor starts them, from the screen, the command line or
	// the sign-out hook.
	Skip []string `toml:"skip"`
	// Logon lists the distributions started when the person signs in to
	// Windows, in this order.
	Logon []string `toml:"logon"`
	// Resume is what comes back when a distribution starts again after
	// wsl-spawner stopped it: "running", every container that ran; "policy",
	// only those their engine restarts by their restart policy; "off", none
	// that wsl-spawner brings back itself.
	Resume string `toml:"resume"`
	// StopLimit is how many seconds a distribution's services and
	// containers get to stop, at most, before WSL ends what is left.
	StopLimit int `toml:"stop_limit"`
}

// Default is what applies without a file.
func Default() Config {
	return Config{Language: "auto", Theme: "auto", Mode: "tui", Resume: "running", StopLimit: 300}
}

// Path is where the file is, under dir (%APPDATA%).
func Path(dir string) string { return filepath.Join(dir, "wsl-spawner", "config.toml") }

// Load reads the file at path; a missing file gives the defaults.
func Load(path string) (Config, error) {
	c := Default()
	b, err := readSoon(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return parse(path, b)
}

// parse reads a settings file's text; path names it in the errors.
func parse(path string, b []byte) (Config, error) {
	c := Default()
	md, err := toml.Decode(string(b), &c)
	if err != nil {
		return Default(), fmt.Errorf("%s: %w", path, err)
	}
	// A key this version does not know is a typo more often than not.
	if und := md.Undecoded(); len(und) > 0 {
		return Default(), &KeyError{path, und[0].String(), fmt.Sprintf("unknown setting %q", und[0].String())}
	}
	return c, c.check(path)
}

// KeyError is a setting in the file at Path the program cannot use: Key
// names it, for the fronts to say how to put it right.
type KeyError struct{ Path, Key, Msg string }

// Error says it with the file's path in front, the file the person knows.
func (e *KeyError) Error() string { return e.Path + ": " + e.Msg }

// check rejects values the program cannot use.
func (c Config) check(path string) error {
	bad := func(key, format string, a ...any) error { return &KeyError{path, key, fmt.Sprintf(format, a...)} }
	switch {
	case !slices.Contains(Languages, c.Language):
		return bad("language", "language must be auto, en or ko")
	case !slices.Contains(Themes, c.Theme):
		return bad("theme", "theme must be auto, dark or light")
	case !slices.Contains(Modes, c.Mode):
		return bad("mode", "mode must be tui or cli")
	case !slices.Contains(Resumes, c.Resume):
		return bad("resume", "resume must be running, policy or off")
	case c.StopLimit < MinStopLimit || c.StopLimit > MaxStopLimit:
		// Below the least a service gets anyway, or past what Windows
		// waits for a sign-out script at its longest.
		return bad("stop_limit", "stop_limit must be from %d to %d seconds", MinStopLimit, MaxStopLimit)
	}
	// A name is checked as wsl.exe takes one: no name is empty, and none
	// holds a character a distribution name cannot.
	for key, names := range map[string][]string{"skip": c.Skip, "logon": c.Logon} {
		for _, n := range names {
			if !ValidName(n) {
				return bad(key, "%s takes distribution names as wsl --list shows them, not %q", key, n)
			}
		}
	}
	// A distribution left to WSL is not one wsl-spawner starts either.
	for _, n := range c.Logon {
		if c.Skipped(n) {
			return bad("logon", "%s is in skip too: wsl-spawner does not start a distribution it leaves to WSL", n)
		}
	}
	return nil
}

// MinStopLimit and MaxStopLimit bound stop_limit: ten seconds is what WSL
// gives a distribution's services when it stops one itself; 3600 is past
// any sign-out script wait Windows is set to by default (600).
const (
	MinStopLimit = 10
	MaxStopLimit = 3600
)

// ValidName reports whether name can be a distribution's name: WSL allows
// letters, digits, '.', '_' and '-', up to 64 of them.
func ValidName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

// Skipped reports whether the distribution named name is left to WSL. Names
// are compared without case, as wsl.exe compares them.
func (c Config) Skipped(name string) bool { return hasName(c.Skip, name) }

// AtLogon reports whether the distribution named name starts at sign-in.
func (c Config) AtLogon(name string) bool { return hasName(c.Logon, name) }

// hasName reports whether names holds name, without case.
func hasName(names []string, name string) bool {
	return slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, name) })
}

// Languages, Themes, Modes and Resumes are the values the settings take, in
// the order setup offers them; every front checks and offers these.
var (
	Languages = []string{"auto", "en", "ko"}
	Themes    = []string{"auto", "dark", "light"}
	Modes     = []string{"tui", "cli"}
	Resumes   = []string{"running", "policy", "off"}
)

// Example is a commented settings file with every key at its default.
const Example = `# wsl-spawner settings. Every key is optional.

# Language of the screen, setup and CLI mode's overview: "en", "ko", or
# "auto": WSL_SPAWNER_LANG when it is set, then Windows' display language.
# The other commands print English, for scripts.
language = "auto"

# Colors: "auto" asks the terminal, or "dark", "light".
theme = "auto"

# What wsl-spawner opens in a terminal: "tui", the screen, or "cli", the
# distributions as a table with the commands to use. Every command works in
# both.
mode = "tui"

# Distributions, by name, that wsl-spawner leaves to WSL: it neither stops
# nor starts them. Docker Desktop's, Rancher Desktop's and Podman's own are
# always left to those tools.
skip = []

# Distributions started when you sign in to Windows, in this order. Each is
# kept running after, with nothing open in it.
logon = []

# What comes back when a distribution starts again after wsl-spawner stopped
# it: "running", every container that was running; "policy", only those
# Docker or Podman restart by their restart policy; "off", none that
# wsl-spawner brings back itself.
resume = "running"

# Seconds a distribution's services and containers get to stop, at most,
# before WSL ends what is left. From 10 to 3600.
stop_limit = 300
`
