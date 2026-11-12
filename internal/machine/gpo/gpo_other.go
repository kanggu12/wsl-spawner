//go:build !windows

package gpo

import (
	"errors"
	"time"
)

// errNotWindows is every call off Windows: there is no Group Policy.
var errNotWindows = errors.New("there is no Group Policy off Windows")

// ScriptsPath and GptPath are Windows' files; off it, none.
func ScriptsPath() string { return "" }
func GptPath() string     { return "" }

// ReadScripts fails off Windows.
func ReadScripts() (string, error) { return "", errNotWindows }

// ReadPowerShellScripts fails off Windows.
func ReadPowerShellScripts() (string, error) { return "", errNotWindows }

// Install fails off Windows.
func Install(string, string, ...string) error { return errNotWindows }

// Uninstall fails off Windows.
func Uninstall(string, ...string) error { return errNotWindows }

// Refresh fails off Windows: there is no policy to read again.
func Refresh() error { return errNotWindows }

// Elevated is false off Windows.
func Elevated() bool { return false }

// MaxWait is Windows' default off it.
func MaxWait() time.Duration { return DefaultWait * time.Second }

// RaiseWait fails off Windows: there is no policy to change.
func RaiseWait(int) (Raised, error) { return Raised{}, errNotWindows }

// PutBackWait fails off Windows.
func PutBackWait() (Wait, bool, error) { return Wait{}, false, errNotWindows }

// RaisedWait is none off Windows.
func RaisedWait() (Raised, bool) { return Raised{}, false }
