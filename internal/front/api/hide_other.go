//go:build !windows

package api

import "os/exec"

// hideWindow has nothing to hide off Windows.
func hideWindow(*exec.Cmd) {}
