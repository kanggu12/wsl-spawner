// Package setup puts wsl-spawner's part in a distribution and takes it out,
// and works out what a change of a distribution's choices does: set up
// inside or not, kept running or not, started at sign-in or not, left to WSL
// or not.
package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
)

// ErrNoSystemd is a distribution without systemd: the start and keep-running
// services are systemd's.
var ErrNoSystemd = errors.New("systemd does not run in it")

// ErrNoInterop is a distribution where Windows programs do not run: the
// keep-running service holds one.
//
//lint:ignore ST1005 "Windows" is a name, and the error is said to people as it is.
var ErrNoInterop = errors.New("Windows programs do not run in it")

// In sets wsl-spawner up in a running distribution: the start script and
// its service, and the keep-running service when keep.
func In(ctx context.Context, sys wsl.System, name string, keep bool) error {
	return run(ctx, sys, name, inside.SetupScript(keep, inside.Signal(name)), "setting wsl-spawner up in")
}

// Out takes everything wsl-spawner put in a running distribution out.
func Out(ctx context.Context, sys wsl.System, name string) error {
	return run(ctx, sys, name, inside.RemoveScript(), "taking wsl-spawner out of")
}

// run runs a set-up or removal script as root and reads how it ended.
func run(ctx context.Context, sys wsl.System, name, script, doing string) error {
	out, err := sys.Run(ctx, name, "root", script)
	what, ok := inside.SetupError(out)
	switch {
	case ok:
		return nil
	case what == "systemd":
		return fmt.Errorf("%s %s: %w", doing, name, ErrNoSystemd)
	case what == "interop":
		return fmt.Errorf("%s %s: %w", doing, name, ErrNoInterop)
	case err != nil:
		return fmt.Errorf("%s %s: %w", doing, name, err)
	}
	return fmt.Errorf("%s %s: the script did not end: %s", doing, name, lastLine(out))
}

// lastLine is the last line a script printed, which says where it stopped.
func lastLine(out string) string {
	ls := strings.Split(strings.TrimSpace(strings.ReplaceAll(out, "\r", "")), "\n")
	return ls[len(ls)-1]
}

// State is a distribution's choices.
type State struct {
	// Managed: wsl-spawner is set up in it.
	Managed bool
	// Keep: kept running with nothing open in it.
	Keep bool
	// Logon: started at sign-in.
	Logon bool
	// Skip: left to WSL.
	Skip bool
}

// Ask is what a change asks for: nil leaves a choice as it is.
type Ask struct {
	Managed, Keep, Logon, Skip *bool
}

// Want is the state a change asks for, from cur, with what each choice
// needs: starting at sign-in keeps it running, keeping it running needs
// wsl-spawner set up in it, and a distribution left to WSL has none of
// these. A choice asked for both ways at once is an error.
func Want(cur State, a Ask) (State, error) {
	on := func(p *bool) bool { return p != nil && *p }
	off := func(p *bool) bool { return p != nil && !*p }
	// What one choice turns on, another asked off.
	switch {
	case on(a.Logon) && (off(a.Keep) || off(a.Managed) || on(a.Skip)):
		return cur, errors.New("starting at sign-in keeps it running, set up and wsl-spawner's")
	case on(a.Keep) && (off(a.Managed) || on(a.Skip)):
		return cur, errors.New("keeping it running needs wsl-spawner set up in it")
	case on(a.Managed) && on(a.Skip):
		return cur, errors.New("a distribution left to WSL has wsl-spawner set up in it")
	}
	w := cur
	if a.Skip != nil {
		w.Skip = *a.Skip
	}
	if a.Managed != nil {
		w.Managed = *a.Managed
	}
	if a.Keep != nil {
		w.Keep = *a.Keep
	}
	if a.Logon != nil {
		w.Logon = *a.Logon
	}
	// What a choice asked on needs comes on with it: starting at sign-in
	// keeps it running, keeping it running needs wsl-spawner set up, and
	// either is no longer leaving it to WSL. Only what was asked pulls:
	// a choice already on never undoes one asked off.
	if on(a.Logon) {
		w.Keep = true
	}
	if on(a.Logon) || on(a.Keep) {
		w.Managed = true
	}
	if on(a.Logon) || on(a.Keep) || on(a.Managed) {
		w.Skip = false
	}
	// Then what is off takes off what needs it.
	if w.Skip {
		w.Managed = false
	}
	if !w.Managed {
		w.Keep = false
	}
	if !w.Keep {
		w.Logon = false
	}
	return w, nil
}

// Inside reports whether going from cur to w changes the distribution
// itself: set up or taken out, or the keep-running service.
func Inside(cur, w State) bool { return cur.Managed != w.Managed || (w.Managed && cur.Keep != w.Keep) }
