//go:build windows

package wsl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/kanggu12/wsl-spawner/internal/core/plain"
)

// lxssKey is where WSL registers each user's distributions.
const lxssKey = `Software\Microsoft\Windows\CurrentVersion\Lxss`

// Real is the System of this PC.
type Real struct{}

// New returns the System of this PC.
func New() System { return Real{} }

// wslExePath is wsl.exe in Windows' own folder, where it always is, and
// which forwards to the WSL from the Store: not one a folder on the PATH may
// hold in its place, as the sign-out hook runs it as whoever signs out.
func wslExePath() string {
	if sys, err := windows.GetSystemDirectory(); err == nil {
		p := filepath.Join(sys, "wsl.exe")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return "wsl.exe"
}

// wslExe runs wsl.exe without a console window of its own and returns its
// decoded output; the error carries that output, which says what went wrong.
func wslExe(ctx context.Context, args ...string) (string, error) {
	// A call with no limit of its own gets one: a WSL that never answers
	// must not hold the screen or a command forever.
	ctx, cancel := limited(ctx)
	defer cancel()
	limit := timeLeft(ctx)
	cmd := exec.CommandContext(ctx, wslExePath(), args...)
	// From Windows' own folder: started from a folder inside WSL (a
	// \\wsl.localhost path), wsl.exe warns into its output that it cannot
	// carry that folder over to the distribution it starts.
	if root := os.Getenv("SystemRoot"); root != "" {
		cmd.Dir = root
	}
	// Ask for UTF-8 where wsl.exe honours it, and keep any window hidden.
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	// Ended at its limit, wsl.exe may leave something holding its output
	// open; the call returns a few seconds later all the same.
	cmd.WaitDelay = 5 * time.Second
	var out capped
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := decode(out.Bytes())
	if err != nil {
		// Out of time, it said nothing that counts as an answer.
		if na := noAnswer(ctx, args, limit); na != nil {
			return text, na
		}
		return text, fmt.Errorf("wsl %s: %w: %s", describe(args), err, plain.Lines(strings.TrimSpace(text)))
	}
	return text, nil
}

// Distros reads the registry and asks wsl.exe which distributions run.
func (Real) Distros(ctx context.Context) ([]Distro, error) {
	root, err := registry.OpenKey(registry.CURRENT_USER, lxssKey, registry.READ)
	if err != nil {
		// No key: WSL has no distribution for this user.
		if err == registry.ErrNotExist {
			return nil, nil
		}
		return nil, fmt.Errorf("reading the WSL registry: %w", err)
	}
	defer root.Close()
	def, _, _ := root.GetStringValue("DefaultDistribution")
	ids, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil, fmt.Errorf("listing the WSL registry: %w", err)
	}
	var entries []regEntry
	for _, id := range ids {
		k, err := registry.OpenKey(root, id, registry.READ)
		// A subkey that cannot be opened is skipped, as if it were absent.
		if err != nil {
			continue
		}
		// Missing values read as empty or zero; buildDistros judges them.
		name, _, _ := k.GetStringValue("DistributionName")
		base, _, _ := k.GetStringValue("BasePath")
		file, _, _ := k.GetStringValue("VhdFileName")
		// A missing Flags is WSL's default flags, which make WSL 1; one
		// that is there but is not a number is noted as unread.
		flags, _, ferr := k.GetIntegerValue("Flags")
		ver, _, _ := k.GetIntegerValue("Version")
		k.Close()
		entries = append(entries, regEntry{id: id, name: name, base: base, file: file, flags: uint32(flags),
			flagsUnread: ferr != nil && ferr != registry.ErrNotExist, version: uint32(ver)})
	}
	// Building the list and asking which run is the part tested off Windows.
	return buildDistros(ctx, entries, def, wslExe)
}

// Info runs `wsl --version` and `wsl --help`.
func (Real) Info(ctx context.Context) (Info, error) {
	return info(ctx, wslExe)
}

// Run runs the script with sh, inside a distribution that already runs.
func (r Real) Run(ctx context.Context, distro, user, script string) (string, error) {
	// Refuse rather than boot a stopped distribution as a side effect, and
	// refuse a name WSL does not have before asking wsl.exe anything.
	ds, err := r.Distros(ctx)
	if err != nil {
		return "", err
	}
	d, err := find(ds, distro)
	if err != nil {
		return "", err
	}
	if !d.Running {
		return "", fmt.Errorf("%s is %w", d.Name, ErrNotRunning)
	}
	// The registered spelling of the name, whatever case was asked for.
	return wslExe(ctx, "--distribution", d.Name, "--user", user, "--exec", "sh", "-c", script)
}

// Stream runs the script like Run, and hands each line to line as it comes.
func (r Real) Stream(ctx context.Context, distro, user, script string, line func(string)) error {
	ds, err := r.Distros(ctx)
	if err != nil {
		return err
	}
	d, err := find(ds, distro)
	if err != nil {
		return err
	}
	if !d.Running {
		return fmt.Errorf("%s is %w", d.Name, ErrNotRunning)
	}
	args := []string{"--distribution", d.Name, "--user", user, "--exec", "sh", "-c", script}
	cmd := exec.CommandContext(ctx, wslExePath(), args...)
	if root := os.Getenv("SystemRoot"); root != "" {
		cmd.Dir = root
	}
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	cmd.WaitDelay = 5 * time.Second
	var errOut capped
	cmd.Stderr = &errOut
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("wsl %s: %w", describe(args), err)
	}
	// What a script prints inside a distribution is UTF-8, as it is; a
	// line is as long as it is.
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line(strings.TrimRight(sc.Text(), "\r"))
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("wsl %s: %w: %s", describe(args), err, plain.Lines(strings.TrimSpace(decode(errOut.Bytes()))))
	}
	return nil
}

// Start runs `true` in the distribution, which boots it.
func (Real) Start(ctx context.Context, distro string) error {
	_, err := wslExe(ctx, "--distribution", distro, "--exec", "true")
	return err
}

// Terminate stops the distribution.
func (Real) Terminate(ctx context.Context, distro string) error {
	_, err := wslExe(ctx, "--terminate", distro)
	return err
}

// Shutdown stops every distribution and the VM.
func (Real) Shutdown(ctx context.Context) error {
	_, err := wslExe(ctx, "--shutdown")
	return err
}
