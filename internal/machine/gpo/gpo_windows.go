//go:build windows

package gpo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// errNotInTests is what the calls that change this PC answer in a test
// binary: a test never changes the person's Group Policy.
var errNotInTests = errors.New("a test does not change this PC")

// dir is where this PC's local Group Policy lives.
func dir() string { return filepath.Join(os.Getenv("SystemRoot"), "System32", "GroupPolicy") }

// ScriptsPath is the user side's scripts.ini; GptPath is gpt.ini.
func ScriptsPath() string { return filepath.Join(dir(), "User", "Scripts", "scripts.ini") }
func GptPath() string     { return filepath.Join(dir(), "gpt.ini") }

// powerShellPath is the user side's psscripts.ini: the PowerShell sign-in
// and sign-out scripts, kept as scripts.ini keeps the others.
func powerShellPath() string { return filepath.Join(dir(), "User", "Scripts", "psscripts.ini") }

// ReadScripts reads scripts.ini, "" when there is none. Any user can read
// it. A file wsl-spawner cannot read for certain as Windows does is an
// *Unsure error that names it.
func ReadScripts() (string, error) { return readScriptsFile(ScriptsPath()) }

// ReadPowerShellScripts reads psscripts.ini as ReadScripts reads
// scripts.ini. wsl-spawner only reads it: Windows runs the PowerShell
// sign-out scripts after the others, unless it is set to run them first.
func ReadPowerShellScripts() (string, error) { return readScriptsFile(powerShellPath()) }

// readScriptsFile reads a scripts file at p, "" when there is none.
func readScriptsFile(p string) (string, error) {
	f, err := readFile(p)
	if err != nil || !f.there {
		return "", err
	}
	text, err := decodeScripts(f.b)
	return text, named(err, p)
}

// Install adds exe with params as the last sign-out script, and takes out
// those of the programs drop names. It needs an administrator. The files as
// they were are kept once beside them, as *.before-wsl-spawner. Windows
// takes the change in as it next reads the user's policy: Refresh has it
// read now.
func Install(exe, params string, drop ...string) error {
	return change(func(text string) (string, error) { return WithHook(text, exe, params, drop...) })
}

// Uninstall removes exe's sign-out script, and those of the programs drop
// names. It needs an administrator; Refresh has Windows read the change.
func Uninstall(exe string, drop ...string) error {
	return change(func(text string) (string, error) { return WithoutHook(text, exe, drop...) })
}

// change changes this PC's policy files with edit, as changeFiles does. A
// test never gets this far.
func change(edit func(string) (string, error)) error {
	if testing.Testing() {
		return errNotInTests
	}
	return changeFiles(ScriptsPath(), GptPath(), edit, os.WriteFile, markAsEditor)
}

// markAsEditor makes a scripts.ini wsl-spawner made hidden and a system
// file, as the Group Policy editor makes it.
func markAsEditor(p string) {
	if u, err := windows.UTF16PtrFromString(p); err == nil {
		_ = windows.SetFileAttributes(u, windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM|windows.FILE_ATTRIBUTE_ARCHIVE)
	}
}

// Refresh has Windows read the policy of the user this process runs as,
// as gpupdate does, with no window: that is when Windows takes in a sign-out
// script for that user. It needs no administrator; run as an administrator
// whom Windows asked for, it would read that administrator's instead.
// gpupdate is Windows' own, from its system folder, not one a folder on the
// PATH may hold, and reads only what changed (RefreshArgs).
func Refresh() error {
	if testing.Testing() {
		return errNotInTests
	}
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// The user's side, what changed in it: the change raised gpt.ini's
	// version, which is what Windows reads it again for.
	cmd := exec.CommandContext(ctx, filepath.Join(sys, "gpupdate.exe"), RefreshArgs...)
	cmd.Dir = sys
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gpupdate: %w: %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}

// Elevated reports whether this process runs as an administrator.
func Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// MaxWait is how long Windows waits for the sign-out scripts, all together,
// before it goes on: the policy's value, or its 600 seconds by default; 0
// waits as long as they take.
func MaxWait() time.Duration {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Policies\Microsoft\Windows\System`, registry.QUERY_VALUE)
	if err != nil {
		return 600 * time.Second
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("MaxGPOScriptWait")
	if err != nil {
		return 600 * time.Second
	}
	return time.Duration(v) * time.Second
}
