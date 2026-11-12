//go:build !windows

package winapi

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
)

// Korean follows LANG off Windows, for the demo and the screenshots.
func Korean() bool {
	l := os.Getenv("LANG")
	return len(l) >= 2 && l[:2] == "ko"
}

// LocalAppData stands in with the user's cache folder off Windows.
func LocalAppData() string {
	d, _ := os.UserCacheDir()
	return filepath.Clean(d)
}

// AppData stands in with the user's config folder off Windows.
func AppData() string {
	d, _ := os.UserConfigDir()
	return filepath.Clean(d)
}

// ProcessStart cannot say off Windows.
func ProcessStart(int) (time.Time, bool) { return time.Time{}, false }

// Running reports whether process pid runs the program named exe, from
// /proc: off Windows only the tests and the demo run, but the journal's
// check works the same.
func Running(pid int, exe string) bool {
	target, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	return err == nil && filepath.Base(target) == exe
}

// Colors reports whether fd is a terminal: off Windows every terminal takes
// escape-code colors.
func Colors(fd uintptr) bool { return term.IsTerminal(fd) }

// RestoreConsole has nothing to put back off Windows.
func RestoreConsole() {}

// ClassicConsole is false off Windows: a Linux terminal draws with fonts of
// its own choosing.
func ClassicConsole() bool { return false }

// ConsoleEastAsian is false off Windows, which has no console code page.
func ConsoleEastAsian() bool { return false }

// StartDetached needs Windows: off it there is no program to start.
func StartDetached([]string) error { return ErrNotWindows }

// LogonEntry needs Windows: off it there are no sign-in programs.
func LogonEntry(string) (string, bool, bool, error) { return "", false, false, ErrNotWindows }

// SetLogonEntry needs Windows.
func SetLogonEntry(string, string) error { return ErrNotWindows }

// DeleteLogonEntry needs Windows.
func DeleteLogonEntry(string) error { return ErrNotWindows }

// RunElevated needs Windows.
func RunElevated(string, []string) (int, error) { return 0, ErrNotWindows }

// ExcludedPorts needs Windows.
func ExcludedPorts(string) (string, error) { return "", ErrNotWindows }

// FixedDisk is false off Windows: there is no Windows disk to start from.
func FixedDisk(string) bool { return false }

// OpenReport opens the file at path for an answer, as on Windows, when it
// is an empty file of its own: a link is not followed, nothing is made, and
// a file another name also leads to, or one that holds anything, is
// refused. Off Windows only the tests' copies use it.
func OpenReport(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !st.Mode().IsRegular() || st.Size() != 0 || !ok || sys.Nlink != 1 {
		f.Close()
		return nil, ErrNotAReport
	}
	return f, nil
}

// WhoElseCanChange needs Windows: its access lists are what it reads.
func WhoElseCanChange(string, bool) (string, string, error) { return "", "", ErrNotWindows }

// StandardUser is false off Windows: there is no UAC to ask.
func StandardUser() bool { return false }
