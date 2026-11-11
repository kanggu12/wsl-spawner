package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/machine/gpo"
	"github.com/kanggu12/wsl-spawner/internal/machine/winapi"
)

// PC is what the program reaches on Windows besides WSL: the sign-out
// scripts, the programs Windows starts at sign-in, a few files and commands,
// and this program started again. Tests and the demo give one of their own,
// so they never reach this PC; New gives the real one.
type PC struct {
	// ReadScripts reads the local Group Policy's scripts.ini;
	// ReadPowerShellScripts its psscripts.ini, whose sign-out scripts
	// Windows runs after the others unless it is set to run them first.
	ReadScripts           func() (string, error)
	ReadPowerShellScripts func() (string, error)
	// InstallHook and UninstallHook add and remove a sign-out script in the
	// policy's files, and take out those of the programs drop names, the
	// hooks of copies that are gone; they need an administrator.
	InstallHook   func(exe, params string, drop ...string) error
	UninstallHook func(exe string, drop ...string) error
	// RefreshPolicy has Windows read the policy of the user this process
	// runs as, which is when a sign-out script starts or stops counting for
	// that user. It needs no administrator.
	RefreshPolicy func() error
	// Elevated reports whether this process is an administrator's.
	Elevated func() bool
	// RunElevated starts a program as an administrator, Windows asking,
	// and returns its exit code.
	RunElevated func(exe string, args []string) (int, error)
	// MaxWait is how long Windows waits for the sign-out scripts.
	MaxWait func() time.Duration
	// LogonEntry, SetLogonEntry and DeleteLogonEntry read and change a
	// program Windows starts at sign-in.
	LogonEntry       func(name string) (command string, ok, off bool, err error)
	SetLogonEntry    func(name, command string) error
	DeleteLogonEntry func(name string) error
	// QuietExe is the build of this program with no console, beside it:
	// what the sign-in program and the sign-out script start.
	QuietExe func() (string, error)
	// FixedDisk reports whether a path is on a fixed disk of this PC, from
	// which Windows can start a program by itself whatever else runs: not a
	// share, a distribution's own files, or a drive that can be taken out.
	FixedDisk func(path string) bool
	// LookAt looks at the file at path, and only looks: nil when it is
	// there, an error that is fs.ErrNotExist when it is not, another when
	// this user may not look at it. A program a sign-out script names may
	// have been moved or removed.
	LookAt func(path string) error
	// ReadFile reads a file of Windows'; HomeDir is the user's folder.
	ReadFile func(path string) ([]byte, error)
	HomeDir  func() (string, error)
	// ExcludedPorts is what netsh lists of the ports Windows keeps.
	ExcludedPorts func(proto string) (string, error)
	// StartDetached starts this program again on its own.
	StartDetached func(args []string) error
	// TerminalDistro is the distribution the terminal this runs from is in,
	// "" outside WSL: a stop of it goes on without that terminal.
	TerminalDistro func() string
	// What an uninstall reaches: whether this is Windows, this program's
	// file, the user's PATH as stored, whether another program holds a
	// file, how many other copies of a program run, and this program's
	// file removed once it has ended, with its folder.
	OnWindows       bool
	Executable      func() (string, error)
	UserPath        func() (value string, expand bool, err error)
	SetUserPath     func(value string, expand bool) error
	InUse           func(path string) bool
	OthersRunning   func(exe string) (int, error)
	DeleteAfterExit func(exe, dir string) error
}

// Refresh is RefreshPolicy; a made-up PC without one, as the demo's, has
// nothing for Windows to read: its policy counts as it is changed.
func (pc PC) Refresh() error {
	if pc.RefreshPolicy == nil {
		return nil
	}
	return pc.RefreshPolicy()
}

// PowerShellScripts is ReadPowerShellScripts; a made-up PC without one, as
// the demo's, has none.
func (pc PC) PowerShellScripts() (string, error) {
	if pc.ReadPowerShellScripts == nil {
		return "", nil
	}
	return pc.ReadPowerShellScripts()
}

// OnFixedDisk is FixedDisk; on a made-up PC without one, as the demo's,
// every path is on a disk of its own.
func (pc PC) OnFixedDisk(path string) bool {
	if pc.FixedDisk == nil {
		return true
	}
	return pc.FixedDisk(path)
}

// Gone reports whether the program at path is gone for certain: its file
// is not there, on a fixed disk of this PC that is. A share, or a drive
// taken out, may be back later; a file this user may not look at may be
// another account's. A made-up PC without LookAt, as the demo's, has none.
func (pc PC) Gone(path string) bool {
	return pc.LookAt != nil && pc.OnFixedDisk(path) && errors.Is(pc.LookAt(path), fs.ErrNotExist)
}

// There reports whether the program at path is there for this user to
// start: Windows starts a sign-out script as the user who signs out.
func (pc PC) There(path string) bool { return pc.LookAt != nil && pc.LookAt(path) == nil }

// ErrNoQuietExe is a copy of this program without its build with no
// console beside it.
var ErrNoQuietExe = errors.New("wsl-spawnerw.exe is not beside wsl-spawner.exe")

// RealPC is this PC.
func RealPC() PC {
	return PC{
		ReadScripts: gpo.ReadScripts, ReadPowerShellScripts: gpo.ReadPowerShellScripts,
		InstallHook: gpo.Install, UninstallHook: gpo.Uninstall, RefreshPolicy: gpo.Refresh,
		Elevated: gpo.Elevated, RunElevated: winapi.RunElevated, MaxWait: gpo.MaxWait,
		LogonEntry: winapi.LogonEntry, SetLogonEntry: winapi.SetLogonEntry, DeleteLogonEntry: winapi.DeleteLogonEntry,
		QuietExe: func() (string, error) {
			self, err := os.Executable()
			if err != nil {
				return "", err
			}
			p := filepath.Join(filepath.Dir(self), "wsl-spawnerw.exe")
			if _, err := os.Stat(p); err != nil {
				return p, ErrNoQuietExe
			}
			return p, nil
		},
		FixedDisk: winapi.FixedDisk,
		// os.Stat says it as LookAt does: no such file or folder is
		// fs.ErrNotExist, a file this user may not look at another error.
		LookAt: func(p string) error {
			_, err := os.Stat(p)
			return err
		},
		ReadFile: os.ReadFile, HomeDir: os.UserHomeDir, ExcludedPorts: winapi.ExcludedPorts,
		StartDetached: winapi.StartDetached,
		// The launcher passes the distribution of the terminal it runs in.
		TerminalDistro: func() string { return os.Getenv("WSL_DISTRO_NAME") },
		OnWindows:      runtime.GOOS == "windows", Executable: os.Executable,
		UserPath: winapi.UserPath, SetUserPath: winapi.SetUserPath, InUse: winapi.InUse,
		OthersRunning: winapi.OthersRunning, DeleteAfterExit: winapi.DeleteAfterExit,
	}
}
