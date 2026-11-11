//go:build windows

package winapi

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The programs Windows starts when this user signs in are values under the
// Run key, one each: the value's name is the program's, its data the
// command line. Task Manager's Startup apps lists them, and can turn one off
// without removing it: it notes that under StartupApproved\Run, by the same
// name, as a binary value whose first byte is odd when off.
const (
	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

// LogonEntry reads the sign-in program named name: its command line, and
// whether Task Manager has it turned off. ok is false when there is none.
func LogonEntry(name string) (command string, ok, off bool, err error) {
	if testing.Testing() {
		return "", false, false, errNotInTests
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	defer k.Close()
	command, _, err = k.GetStringValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	return command, true, approvedOff(name), nil
}

// approvedOff reports whether Task Manager has the sign-in program named
// name turned off.
func approvedOff(name string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	b, _, err := k.GetBinaryValue(name)
	return err == nil && len(b) > 0 && b[0]&1 == 1
}

// SetLogonEntry makes command a sign-in program named name. It is asked for
// as a choice of its own, so a turning-off in Task Manager from before goes:
// the entry is on, as asked.
func SetLogonEntry(name, command string) error {
	if testing.Testing() {
		return errNotInTests
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue(name, command); err != nil {
		return err
	}
	return deleteApproved(name)
}

// DeleteLogonEntry removes the sign-in program named name, and Task
// Manager's note about it; none is nothing to remove.
func DeleteLogonEntry(name string) error {
	if testing.Testing() {
		return errNotInTests
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err == nil {
		err = k.DeleteValue(name)
		k.Close()
	}
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return deleteApproved(name)
}

// deleteApproved removes Task Manager's note about the sign-in program named
// name, if there is one.
func deleteApproved(name string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.SET_VALUE)
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
