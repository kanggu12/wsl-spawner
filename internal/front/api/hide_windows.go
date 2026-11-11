//go:build windows

package api

import (
	"os/exec"
	"syscall"
)

// hideWindow starts the program with no console window of its own.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
