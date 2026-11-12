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
// waits as long as they take. A registry that cannot be read is Windows'
// own default.
func MaxWait() time.Duration {
	w, err := machine.Wait()
	if err != nil {
		return DefaultWait * time.Second
	}
	return time.Duration(w.Effective()) * time.Second
}

// The policy's wait, where Windows reads it, and wsl-spawner's note of a
// wait it raised: values of a key of its own, which only administrators may
// change, as the policy's.
const (
	policyKey   = `Software\Policies\Microsoft\Windows\System`
	waitValue   = "MaxGPOScriptWait"
	noteKey     = `Software\wsl-spawner`
	raisedValue = "RaisedMaxGPOScriptWait"
	beforeValue = "MaxGPOScriptWaitBefore"
)

// regStore is a WaitStore in the registry under root: the machine's on
// this PC; a runner's test gives keys of its own.
type regStore struct {
	root         registry.Key
	policy, note string
}

// machine is this PC's: what Windows reads.
var machine = regStore{registry.LOCAL_MACHINE, policyKey, noteKey}

// RaiseWait raises the policy's wait to seconds, and notes what was there
// (Raise). It needs an administrator. A test never gets this far.
func RaiseWait(seconds int) (Raised, error) {
	if testing.Testing() {
		return Raised{}, errNotInTests
	}
	return Raise(machine, seconds)
}

// PutBackWait puts back a wait wsl-spawner raised (PutBack). It needs an
// administrator. A test never gets this far.
func PutBackWait() (Wait, bool, error) {
	if testing.Testing() {
		return Wait{}, false, errNotInTests
	}
	return PutBack(machine)
}

// RaisedWait is the note of a wait wsl-spawner raised that is still the
// one it set (StillRaised). Anyone may read it.
func RaisedWait() (Raised, bool) { return StillRaised(machine) }

// Wait reads the policy's wait: none set, where its key or value is not there.
func (r regStore) Wait() (Wait, error) {
	k, err := registry.OpenKey(r.root, r.policy, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return Wait{}, nil
	}
	if err != nil {
		return Wait{}, err
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue(waitValue)
	if errors.Is(err, registry.ErrNotExist) {
		return Wait{}, nil
	}
	if err != nil {
		return Wait{}, err
	}
	return Wait{Seconds: int(v), Set: true}, nil
}

// SetWait sets the policy's wait, as the policy's editor does, a number
// of seconds; one not Set takes the value out, for Windows' own.
func (r regStore) SetWait(w Wait) error {
	if !w.Set {
		return deleteValue(r.root, r.policy, waitValue)
	}
	k, _, err := registry.CreateKey(r.root, r.policy, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetDWordValue(waitValue, uint32(w.Seconds))
}

// Note reads wsl-spawner's note: none where its key or value is not there.
// A Before not noted is a wait none set.
func (r regStore) Note() (Raised, bool, error) {
	k, err := registry.OpenKey(r.root, r.note, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return Raised{}, false, nil
	}
	if err != nil {
		return Raised{}, false, err
	}
	defer k.Close()
	to, _, err := k.GetIntegerValue(raisedValue)
	if errors.Is(err, registry.ErrNotExist) {
		return Raised{}, false, nil
	}
	if err != nil {
		return Raised{}, false, err
	}
	n := Raised{To: int(to)}
	switch before, _, err := k.GetIntegerValue(beforeValue); {
	case err == nil:
		n.Before = Wait{Seconds: int(before), Set: true}
	case !errors.Is(err, registry.ErrNotExist):
		return Raised{}, false, err
	}
	return n, true, nil
}

// SetNote writes wsl-spawner's note, or with nil takes it out, with its
// key when nothing else is in it.
func (r regStore) SetNote(n *Raised) error {
	if n == nil {
		if err := deleteValue(r.root, r.note, raisedValue); err != nil {
			return err
		}
		if err := deleteValue(r.root, r.note, beforeValue); err != nil {
			return err
		}
		return deleteEmptyKey(r.root, r.note)
	}
	k, _, err := registry.CreateKey(r.root, r.note, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetDWordValue(raisedValue, uint32(n.To)); err != nil {
		return err
	}
	if !n.Before.Set {
		// None was set: nothing to note beside what wsl-spawner set.
		return deleteValue(r.root, r.note, beforeValue)
	}
	return k.SetDWordValue(beforeValue, uint32(n.Before.Seconds))
}

// deleteValue takes a value out of the key at path; a key or a value that
// is not there is taken out already.
func deleteValue(root registry.Key, path, name string) error {
	k, err := registry.OpenKey(root, path, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// deleteEmptyKey takes out the key at path when it holds nothing: one with
// a value or a key of someone else's stays.
func deleteEmptyKey(root registry.Key, path string) error {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	info, err := k.Stat()
	k.Close()
	if err != nil || info.SubKeyCount > 0 || info.ValueCount > 0 {
		return err
	}
	return registry.DeleteKey(root, path)
}
