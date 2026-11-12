package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/front/api"

	"github.com/spf13/cobra"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/distros"
	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/version"
)

// What start waits with: tests make it short.
var (
	// startWait is how long a start waits for the service that brings
	// containers back to finish.
	startWait = 2 * time.Minute
	// startPoll is how often it looks.
	startPoll = 2 * time.Second
)

// startCmd starts distributions, and says what came back in them.
func startCmd(p *apps) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "start distribution...",
		Short: "Start distributions, and say which containers came back in them",
		Long: "start starts each distribution named. Where wsl-spawner is set up, its service\n" +
			"starts again the containers the last stop noted; start waits for it and says\n" +
			"what came back.",
		RunE: func(cmd *cobra.Command, names []string) error {
			if len(names) == 0 {
				return fmt.Errorf("%w: name the distributions to start", errUsage)
			}
			a, err := p.get()
			if err != nil {
				return err
			}
			ds, err := distros.Read(cmd.Context(), a.WSL, a.Config)
			if err != nil {
				return err
			}
			var targets []distros.Distro
			for _, name := range names {
				i := slices.IndexFunc(ds, func(d distros.Distro) bool { return strings.EqualFold(d.Name, name) })
				if i < 0 {
					return fmt.Errorf("%w: %s", wsl.ErrNoDistro, name)
				}
				if why := notOurs(ds[i]); why != "" {
					return hint(fmt.Errorf("%w: %s", errUsage, strings.Replace(why, "stops it", "starts it", 1)), "wsl -d "+ds[i].Name+" starts it")
				}
				targets = append(targets, ds[i])
			}
			return startAll(cmd.Context(), a, targets, "hand", cmd.OutOrStdout(), lookFor(cmd.OutOrStdout()), asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the results as JSON")
	return c
}

// logonCmd is what the sign-in program runs: it starts the distributions
// the settings name, in their order. It is started with no console, so what
// it did goes to the history.
func logonCmd(p *apps) *cobra.Command {
	return &cobra.Command{
		Use:    "logon",
		Short:  "Start the distributions the settings start at sign-in",
		Args:   noArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := p.anyway()
			if err != nil {
				return err
			}
			// The settings say what starts at sign-in: unread, nothing
			// does, and the history says why, as no console shows it.
			if serr := p.settingsErr(); serr != nil {
				_ = history.Open(a.Dir).Add(history.Entry{At: a.Now(), What: "start", By: "logon",
					Err: "the settings did not load, so nothing started at sign-in: " + bareError(serr), Version: version.String()})
				return serr
			}
			ds, err := distros.Read(cmd.Context(), a.WSL, a.Config)
			if err != nil {
				_ = history.Open(a.Dir).Add(history.Entry{At: a.Now(), What: "start", By: "logon", Err: err.Error(), Version: version.String()})
				return err
			}
			var targets []distros.Distro
			for _, name := range a.Config.Logon {
				i := slices.IndexFunc(ds, func(d distros.Distro) bool { return strings.EqualFold(d.Name, name) })
				// One no longer registered, or left to another tool since,
				// is passed by: the others still start.
				if i >= 0 && ds[i].Ours() {
					targets = append(targets, ds[i])
				}
			}
			return startAll(cmd.Context(), a, targets, "logon", cmd.OutOrStdout(), lookFor(cmd.OutOrStdout()), false)
		},
	}
}

// startAll starts each distribution in turn, then waits for what comes
// back in all of them at once, each its own time, says it in their order,
// or prints it as JSON, and keeps it in the history.
func startAll(ctx context.Context, a *app.App, targets []distros.Distro, by string, out io.Writer, l look, asJSON bool) error {
	// The App's clock, as a stop's: the demo's made-up PC keeps its own.
	began := a.Now()
	e := history.Entry{At: began, What: "start", By: by, Version: version.String()}
	res := api.StartResult{Schema: schema, Results: []api.DistroStart{}}
	failed := 0
	say := func(s string) {
		if !asJSON {
			fmt.Fprintln(out, s)
		}
	}
	// One distribution's start: what is said of it, kept and printed, and
	// done once that is known.
	type start struct {
		line string
		de   history.Distro
		r    api.DistroStart
		done chan struct{}
	}
	starts := make([]*start, len(targets))
	// Each started in the targets' order, as WSL starts them one after the
	// other; what comes back in each is waited for at once with the others,
	// each from its own start for startWait: one after the other, a third
	// distribution at sign-in came minutes late.
	for i, d := range targets {
		t0 := a.Now()
		s := &start{de: history.Distro{Name: d.Name, Result: "started"},
			r: api.DistroStart{Name: d.Name, Result: "started", Back: []api.CameBack{}}, done: make(chan struct{})}
		starts[i] = s
		// took is how long its start took, from t0 to now.
		took := func() float64 { return a.Now().Sub(t0).Seconds() }
		if d.Running {
			s.de.Result, s.r.Result = "running", "running"
			s.line, s.de.Took = d.Name+" is running already", took()
			close(s.done)
		} else if err := a.WSL.Start(ctx, d.Name); err != nil {
			s.de.Result, s.de.Err, s.r.Result, s.r.Err = "error", err.Error(), "error", err.Error()
			failed++
			s.line, s.de.Took = l.bad("✗")+" "+d.Name+": "+firstLine(err.Error()), took()
			close(s.done)
		} else {
			go func() {
				defer close(s.done)
				back := waitForStart(ctx, a, d.Name, t0)
				for _, b := range back {
					s.r.Back = append(s.r.Back, api.CameBack{Engine: b.Engine, Name: b.Name, What: b.What})
				}
				s.line, s.de.Took = l.done(d.Name+" started"+backWords(back)), took()
			}()
		}
	}
	// Said and kept in the targets' order, each as soon as it and those
	// before it are known.
	for _, s := range starts {
		<-s.done
		say(s.line)
		e.Distros = append(e.Distros, s.de)
		res.Results = append(res.Results, s.r)
	}
	e.Took = a.Now().Sub(began).Seconds()
	if err := history.Open(a.Dir).Add(e); err != nil && !asJSON {
		fmt.Fprintln(out, l.warn("! ")+"the history was not written: "+err.Error())
	}
	if asJSON {
		if err := printJSON(out, res); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("%s did not start", counted(failed, "distribution"))
	}
	return nil
}

// waitForStart waits, where wsl-spawner is set up, for its service to have
// brought back what the last stop noted, and returns what it did; nothing
// where it is not set up, or when it did not finish in time.
func waitForStart(ctx context.Context, a *app.App, name string, since time.Time) []inside.Resumed {
	deadline := a.Now().Add(startWait)
	for {
		p, err := distros.Probe(ctx, a.WSL, name)
		if err != nil || !p.SetUp() {
			return nil
		}
		// The service has run for this start once it is no longer starting
		// and what it did is from this start; with nothing noted to bring
		// back, it leaves nothing new.
		if u := p.Units[inside.StartUnit]; u.Active != "activating" {
			if !p.ResumedAt.Before(since.Add(-5 * time.Second)) {
				return p.Resumed
			}
			if len(p.Bring) == 0 {
				return nil
			}
		}
		if !a.Now().Before(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(startPoll):
		}
	}
}

// backWords says what came back at a start, after "started".
func backWords(rs []inside.Resumed) string {
	if len(rs) == 0 {
		return ""
	}
	var started, already, other []string
	for _, r := range rs {
		switch r.What {
		case "started":
			started = append(started, r.Name)
		case "running":
			already = append(already, r.Name)
		default:
			other = append(other, r.Name+" "+r.What)
		}
	}
	var parts []string
	if len(started) > 0 {
		parts = append(parts, "brought back "+strings.Join(started, ", "))
	}
	if len(already) > 0 {
		parts = append(parts, "back by their restart policy: "+strings.Join(already, ", "))
	}
	if len(other) > 0 {
		parts = append(parts, strings.Join(other, ", "))
	}
	return "; " + strings.Join(parts, "; ")
}
