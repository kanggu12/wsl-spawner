// Package stop stops distributions the slow way: the stop script inside
// each, which gives every service and container the time it asks for, then
// WSL's own stop for what is left; all of them at once, and WSL itself after
// when asked.
package stop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
)

// Options is how a stop goes.
type Options struct {
	// Limit is how long the steps inside a distribution may take together.
	Limit time.Duration
	// Resume is which running containers come back at the next start.
	Resume string
	// By is who asked: "hand" or "signout".
	By string
	// Shutdown stops WSL itself after the distributions.
	Shutdown bool
}

// Margin is how much longer than its limit a distribution's script may run
// before it is taken for one that will not end: starting it, and WSL
// answering slowly, take time of their own.
const Margin = 30 * time.Second

// Event is one thing that happened, for a front to show as it happens.
type Event struct {
	// Distro is the distribution it is about; "" for the shutdown.
	Distro string
	// Kind is "line" (a line of the stop script, in Line), "terminate"
	// (WSL ends what is left, Done once it did), "shutdown" (WSL itself
	// stops, Done once it did) or "finished" (Result says how it went).
	Kind   string
	Line   inside.StopLine
	Done   bool
	Result *Result
	// Err is what went wrong with a terminate or the shutdown.
	Err error
}

// Result is how one distribution's stop went.
type Result struct {
	Distro string
	// End is the stop script's "clean", "limit" or "failed", or "error"
	// when it did not run to its end.
	End string
	// Took is how long it took, the end of the distribution included.
	Took time.Duration
	// Steps are the steps inside it, as they ended.
	Steps []history.Step
	// Err says what went wrong: the script not starting or not ending, or
	// WSL not ending the distribution.
	Err error
}

// Clean reports whether every step had its time and the distribution ended.
func (r Result) Clean() bool { return r.End == "clean" && r.Err == nil }

// Run stops the distributions named, all at once, and WSL after them when
// asked. on is called with each event, one at a time; now is read from
// every distribution's goroutine at once. A distribution that fails is said
// in its Result; the others go on.
func Run(ctx context.Context, sys wsl.System, names []string, o Options, now func() time.Time, on func(Event)) ([]Result, error) {
	script, err := inside.StopScript(inside.StopOptions{Limit: o.Limit, Resume: o.Resume, By: o.By, Now: now()})
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	say := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		on(e)
	}
	results := make([]Result, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = one(ctx, sys, name, script, o.Limit, now, say)
		}()
	}
	wg.Wait()
	if o.Shutdown {
		say(Event{Kind: "shutdown"})
		err := sys.Shutdown(context.WithoutCancel(ctx))
		say(Event{Kind: "shutdown", Done: true, Err: err})
		if err != nil {
			return results, fmt.Errorf("stopping WSL: %w", err)
		}
	}
	return results, nil
}

// one stops one distribution: its script, then WSL's own stop.
func one(ctx context.Context, sys wsl.System, name, script string, limit time.Duration, now func() time.Time, say func(Event)) Result {
	start := now()
	r := Result{Distro: name, End: "error"}
	// The script gets its limit and the margin; Ctrl+C stops waiting for
	// it, though the distribution still goes down below.
	sctx, cancel := context.WithTimeout(ctx, limit+Margin)
	// A step's start says how many it stops; its end, how it went.
	counts := map[string]int{}
	err := sys.Stream(sctx, name, "root", script, func(text string) {
		l, ok := inside.ParseStopLine(text)
		if !ok {
			return
		}
		switch l.Kind {
		case "start":
			counts[l.Step+"/"+l.Hook] = l.Count
		case "done":
			r.Steps = append(r.Steps, history.Step{Step: l.Step, Hook: l.Hook, Code: l.Code, Took: l.Took.Seconds(), Count: counts[l.Step+"/"+l.Hook]})
		case "end":
			r.End = l.Result
		}
		say(Event{Distro: name, Kind: "line", Line: l})
	})
	cancel()
	if err != nil && r.End == "error" {
		r.Err = fmt.Errorf("the stop inside %s did not run to its end: %w", name, err)
	}
	// WSL ends what is left, whatever the steps did: a stop that was asked
	// for leaves the distribution stopped. It is not cut short by Ctrl+C,
	// which would leave it half stopped.
	say(Event{Distro: name, Kind: "terminate"})
	terr := sys.Terminate(context.WithoutCancel(ctx), name)
	say(Event{Distro: name, Kind: "terminate", Done: true, Err: terr})
	if terr != nil && r.Err == nil {
		r.Err = fmt.Errorf("WSL did not end %s: %w", name, terr)
	}
	r.Took = now().Sub(start)
	say(Event{Distro: name, Kind: "finished", Result: &r})
	return r
}

// Entry is the history's record of a stop that started at start and ran
// for took.
func Entry(results []Result, o Options, start time.Time, took time.Duration, err error, version string) history.Entry {
	e := history.Entry{At: start, Took: took.Seconds(), What: "stop", By: o.By, Shutdown: o.Shutdown, Version: version}
	for _, r := range results {
		d := history.Distro{Name: r.Distro, Result: r.End, Took: r.Took.Seconds(), Steps: r.Steps}
		if r.Err != nil {
			d.Err = r.Err.Error()
		}
		e.Distros = append(e.Distros, d)
	}
	if err != nil {
		e.Err = err.Error()
	}
	return e
}
