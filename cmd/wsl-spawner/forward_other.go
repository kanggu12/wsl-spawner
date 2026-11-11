//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/charmbracelet/x/term"

	"github.com/kanggu12/wsl-spawner/internal/front/cli"
)

// forward hands the command line to wsl-spawner.exe when this Linux build
// runs inside a WSL distribution: WSL is Windows', so the work is done
// there. WSLENV carries this distribution's name across ("/w": from WSL to
// Windows only), so the Windows side knows which distribution its terminal
// runs in, and a stop of that one can say so first. It returns false to run
// here instead.
func forward() bool {
	// The demo and its frames are made-up: they run here, anywhere.
	if os.Getenv("WSL_DISTRO_NAME") == "" || slices.ContainsFunc(os.Args[1:], func(a string) bool {
		return a == "--demo" || strings.HasPrefix(a, "--snapshot")
	}) {
		return false
	}
	exe := locate()
	if exe == "" {
		// Help and the version need nothing from Windows: they are answered
		// here, so a launcher without its .exe can still say what it is.
		if answersAlone(os.Args[1:]) {
			return false
		}
		cli.PrintError(os.Stderr, "wsl-spawner.exe was not found next to this file or on the Windows PATH", "install it in PowerShell on Windows: "+installHint)
		exit(1)
		return true
	}
	// A .exe for another processor is never started: Windows would refuse
	// it with a dialog on the desktop, not a line here. Nothing above this
	// line starts it.
	if why := wrongBuild(exe); why != "" {
		if answersAlone(os.Args[1:]) {
			return false
		}
		cli.PrintError(os.Stderr, why, "install the build for this PC in PowerShell: "+installHint)
		exit(1)
		return true
	}
	// The distribution's name, and the language and marks chosen in this
	// shell.
	wslenv := appendWSLENV(os.Getenv("WSLENV"), "WSL_DISTRO_NAME/w")
	wslenv = appendWSLENV(wslenv, "WSL_SPAWNER_LANG/w")
	wslenv = appendWSLENV(wslenv, "WSL_SPAWNER_SYMBOLS/w")
	env := withWSLENV(os.Environ(), wslenv)
	// The screen, asked for in a terminal, is drawn here: keys, sizes and
	// colors stay this terminal's, and every command it runs is asked of
	// wsl-spawner.exe, which reaches this terminal only through pipes.
	if screenAsked(os.Args[1:]) && terminals() == "012" {
		os.Setenv("WSL_SPAWNER_EXE", exe)
		os.Setenv("WSLENV", wslenv)
		return false
	}
	here := relayEnv(env, wslenv, terminals(), termWidth())
	// Replace this process: the terminal, the exit code and Ctrl+C all
	// belong to the Windows program from here on.
	err := execve(exe, append([]string{exe}, os.Args[1:]...), here)
	cli.PrintError(os.Stderr, "could not start "+exe+": "+err.Error(), startHint(err))
	exit(1)
	return true
}

// What forward reaches the PC through: tests replace them, to see what
// would have started.
var (
	// locate finds wsl-spawner.exe.
	locate = findExe
	// execve replaces this process with the .exe. A test never starts it:
	// a stand-in .exe would put Windows' dialog for a program it cannot run
	// on the person's screen.
	execve = func(argv0 string, argv, env []string) error {
		if testing.Testing() {
			return errNoProgramInTests
		}
		return syscall.Exec(argv0, argv, env)
	}
	// exit ends the launcher, as main ends the program.
	exit = leave
	// terminals says which of stdin, stdout and stderr are a terminal here:
	// "0", "1" and "2", in that order.
	terminals = func() string {
		fds := ""
		for i, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
			if term.IsTerminal(f.Fd()) {
				fds += strconv.Itoa(i)
			}
		}
		return fds
	}
)

// errNoProgramInTests is what starting the .exe gives in a test, whatever
// path it took to get there: tests give stand-ins for what would start.
var errNoProgramInTests = errors.New("a test starts no program; give it a stand-in")

// installHint is how wsl-spawner is installed, in PowerShell on Windows:
// the installer picks the build for the PC's processor.
const installHint = "irm https://github.com/kanggu12/wsl-spawner/releases/latest/download/install.ps1 | iex"

// relayEnv is env for the .exe started in this terminal: fds says which of
// its ends a person is at ("0" stdin, "1" stdout, "2" stderr) and goes
// across as WSL_SPAWNER_TTY, with the terminal's width. Started from WSL,
// the .exe reaches this terminal only through pipes; told so, it asks its
// questions, colors its lines and lays out its tables as at a console.
// With no end at a terminal, env as it is.
func relayEnv(env []string, wslenv, fds string, width int) []string {
	if fds == "" {
		return env
	}
	wslenv = appendWSLENV(appendWSLENV(wslenv, "WSL_SPAWNER_TTY/w"), "WSL_SPAWNER_COLUMNS/w")
	env = withVar(withVar(env, "WSL_SPAWNER_TTY", fds), "WSL_SPAWNER_COLUMNS", strconv.Itoa(width))
	return withWSLENV(env, wslenv)
}

// withVar is env with name set to value, replacing what it had.
func withVar(env []string, name, value string) []string {
	out := slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return strings.HasPrefix(kv, name+"=") })
	return append(out, name+"="+value)
}

// termWidth is this terminal's width, 80 when it does not say.
func termWidth() int {
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		return w
	}
	return 80
}

// startHint says what to do when wsl-spawner.exe could not be started. A
// distribution with WSL's interop turned off runs no Windows program: the
// kernel finds no handler for the file and says "exec format error".
func startHint(err error) string {
	if errors.Is(err, syscall.ENOEXEC) {
		return "this distribution runs no Windows programs: set enabled=true under [interop] in /etc/wsl.conf, then stop and start the distribution"
	}
	return ""
}

// answersAlone reports whether a command line asks only for help or the
// version, which this build can answer without Windows.
func answersAlone(args []string) bool {
	if len(args) > 0 && (args[0] == "version" || args[0] == "help") {
		return true
	}
	return slices.ContainsFunc(args, func(a string) bool { return a == "--help" || a == "-h" })
}

// findExe looks next to this file first, then on the PATH, which in WSL
// includes Windows' own.
func findExe() string {
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), "wsl-spawner.exe")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("wsl-spawner.exe"); err == nil {
		return p
	}
	return ""
}

// withWSLENV returns env with WSLENV set to v. The entry is replaced, not
// added: with two, the first one counts, and that is the old value.
func withWSLENV(env []string, v string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, "WSLENV=") {
			out = append(out, e)
		}
	}
	return append(out, "WSLENV="+v)
}

// appendWSLENV adds an entry to WSLENV's colon-separated list once.
func appendWSLENV(cur, entry string) string {
	for _, e := range strings.Split(cur, ":") {
		if e == entry {
			return cur
		}
	}
	if cur == "" {
		return entry
	}
	return cur + ":" + entry
}

// valueFlags are the flags that take a value as the next word.
var valueFlags = []string{"--lang", "--theme", "--keys", "--snapshot"}

// screenAsked reports whether a command line opens the screen: no command,
// or tui, with only flags around it.
func screenAsked(args []string) bool {
	// Help is answered by the program, whatever else is asked.
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		return false
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if slices.Contains(valueFlags, a) {
			// Its value is the next word, never a command.
			i++
			continue
		}
		if !strings.HasPrefix(a, "-") {
			return a == "tui"
		}
	}
	return true
}
