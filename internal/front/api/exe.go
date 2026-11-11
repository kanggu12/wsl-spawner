package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// startExe runs the program at path with args and waits for it. It runs
// from Windows' own folder, with no window of its own on Windows.
func startExe(ctx context.Context, path string, env, args []string, out, errOut io.Writer) (int, error) {
	if testing.Testing() {
		return 0, errNoProgramInTests
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	if root := os.Getenv("SystemRoot"); root != "" && runtime.GOOS == "windows" {
		cmd.Dir = root
	}
	hideWindow(cmd)
	cmd.Stdout, cmd.Stderr = out, errOut
	// Ended with the screen, it may leave something holding its output.
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	var exited *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exited) && exited.ExitCode() >= 0:
		return exited.ExitCode(), nil
	}
	return 0, fmt.Errorf("%s: %w", describe(args), err)
}
