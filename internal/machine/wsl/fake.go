package wsl

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Fake is a System held in memory, for the tests and the demo. Calls that
// change something are recorded in Calls, in order.
type Fake struct {
	mu sync.Mutex
	// scriptMu calls Script one at a time: distributions are read and
	// stopped at the same time, and a test's Script keeps notes without a
	// lock of its own.
	scriptMu sync.Mutex
	// List is what Distros returns.
	List []Distro
	// Version shapes what Info returns.
	Version string
	// Script answers Run, and Stream line by line; nil answers with empty
	// output.
	Script func(distro, user, script string) (string, error)
	// Streamed answers Stream instead of Script, when set: it calls line
	// as the script would, for a test to time the lines.
	Streamed func(ctx context.Context, distro, user, script string, line func(string)) error
	// Fail makes the named calls fail ("terminate:Ubuntu", "start:Ubuntu",
	// "shutdown:").
	Fail map[string]error
	// Flaky makes a call Fail names fail only this many times, then go
	// through, as WSL refusing for a moment does.
	Flaky map[string]int
	// InfoErr makes Info fail, as when WSL does not answer.
	InfoErr error
	// Calls records "terminate:X", "start:X", "run:X" and "shutdown:".
	Calls []string
	// DistrosErr makes Distros fail, as when WSL does not answer.
	DistrosErr error
}

// Distros returns the list.
func (f *Fake) Distros(context.Context) ([]Distro, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DistrosErr != nil {
		return nil, f.DistrosErr
	}
	return slices.Clone(f.List), nil
}

// Info returns the version.
func (f *Fake) Info(context.Context) (Info, error) {
	if f.InfoErr != nil {
		return Info{NoAnswer: errors.Is(f.InfoErr, ErrNoAnswer)}, f.InfoErr
	}
	v := f.Version
	if v == "" {
		v = "3.0.3.0"
	}
	return Info{Version: v}, nil
}

// Run answers with Script, and refuses a stopped distribution as WSL's
// real Run does.
func (f *Fake) Run(ctx context.Context, distro, user, script string) (string, error) {
	if err := f.record("run", distro); err != nil {
		return "", err
	}
	// A cancelled context fails the call, as it kills wsl.exe for real.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// An unknown name and a stopped distribution fail as the real Run does.
	d, err := find(f.list(), distro)
	if err != nil {
		return "", err
	}
	if !d.Running {
		return "", fmt.Errorf("%s is %w", d.Name, ErrNotRunning)
	}
	if f.Script == nil {
		return "", nil
	}
	f.scriptMu.Lock()
	defer f.scriptMu.Unlock()
	return f.Script(distro, user, script)
}

// Stream is Run, with the output handed over line by line.
func (f *Fake) Stream(ctx context.Context, distro, user, script string, line func(string)) error {
	if f.Streamed != nil {
		if err := f.record("stream", distro); err != nil {
			return err
		}
		d, err := find(f.list(), distro)
		if err != nil {
			return err
		}
		if !d.Running {
			return fmt.Errorf("%s is %w", d.Name, ErrNotRunning)
		}
		return f.Streamed(ctx, d.Name, user, script, line)
	}
	out, err := f.Run(ctx, distro, user, script)
	for _, l := range lines(out) {
		line(l)
	}
	return err
}

// Start marks the distribution running.
func (f *Fake) Start(ctx context.Context, distro string) error {
	if err := f.record("start", distro); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.set(distro, true)
	return nil
}

// Terminate marks it stopped.
func (f *Fake) Terminate(_ context.Context, distro string) error {
	if err := f.record("terminate", distro); err != nil {
		return err
	}
	f.set(distro, false)
	return nil
}

// Shutdown marks every distribution stopped, as `wsl --shutdown` leaves them.
func (f *Fake) Shutdown(ctx context.Context) error {
	if err := f.record("shutdown", ""); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.List {
		f.List[i].Running = false
	}
	return nil
}

// record notes a call and returns the failure set up for it.
func (f *Fake) record(op, distro string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := op + ":" + distro
	f.Calls = append(f.Calls, key)
	if n, flaky := f.Flaky[key]; flaky {
		if n == 0 {
			return nil
		}
		f.Flaky[key] = n - 1
	}
	return f.Fail[key]
}

// list returns the distributions as they stand.
func (f *Fake) list() []Distro {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.List)
}

// set changes whether a distribution runs.
func (f *Fake) set(distro string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.List {
		if strings.EqualFold(f.List[i].Name, distro) {
			f.List[i].Running = on
		}
	}
}

// CallList returns a copy of the calls so far.
func (f *Fake) CallList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Calls)
}
