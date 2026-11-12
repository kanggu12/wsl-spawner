package cli

import (
	"encoding/json"
	"errors"
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
	"github.com/kanggu12/wsl-spawner/internal/engine/stop"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/version"
)

// stopCmd stops distributions the slow way.
func stopCmd(p *apps) *cobra.Command {
	var all, shutdown, yes, asJSON, events, background bool
	var by string
	c := &cobra.Command{
		Use:   "stop [distribution...]",
		Short: "Stop distributions, each service and container with the time it asks for",
		Long: "stop brings each distribution named down the slow way: the hooks in\n" +
			"/etc/wsl-spawner/pre-stop.d, the units in /etc/wsl-spawner/stop-units, then\n" +
			"Docker and Podman, every container with its own stop timeout, all within\n" +
			"stop_limit seconds. WSL then ends what is left. Distributions stop at the same\n" +
			"time.\n\n" +
			"It says what it will do and asks first; --yes does it without the question.\n" +
			"--shutdown stops WSL itself after, every distribution with it. Exit code 3: a\n" +
			"step ran out of time or failed.",
		RunE: func(cmd *cobra.Command, names []string) error {
			if by != "hand" && by != "signout" {
				return fmt.Errorf("%w: --by takes hand or signout", errUsage)
			}
			if len(names) > 0 && all {
				return fmt.Errorf("%w: name distributions or give --all, not both", errUsage)
			}
			a, err := p.get()
			if err != nil {
				return err
			}
			if events && !asJSON {
				return fmt.Errorf("%w: --events goes with --json", errUsage)
			}
			return runStop(cmd, a, stopAsk{names: names, all: all, shutdown: shutdown, yes: yes, json: asJSON, events: events, background: background, by: by})
		},
	}
	c.Flags().BoolVar(&all, "all", false, "stop every running distribution wsl-spawner stops")
	c.Flags().BoolVar(&shutdown, "shutdown", false, "then stop WSL itself, as wsl --shutdown does")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "stop without asking")
	c.Flags().BoolVar(&asJSON, "json", false, "print the results as JSON")
	c.Flags().BoolVar(&events, "events", false, "with --json, print each step as a line of JSON as it happens, the results last")
	c.Flags().BoolVar(&background, "background", false, "run without a terminal, as the stop a closing terminal leaves behind")
	c.Flags().StringVar(&by, "by", "hand", "who asks, for the history: `hand|signout`")
	for _, f := range []string{"background", "by"} {
		_ = c.Flags().MarkHidden(f)
	}
	return c
}

// stopAsk is what the command line asked of a stop.
type stopAsk struct {
	names                                        []string
	all, shutdown, yes, json, events, background bool
	by                                           string
	// settingsErr is why the settings did not load, where a stop goes on
	// with the defaults all the same: the sign-out hook's. The history
	// says it.
	settingsErr error
}

// runStop works out which distributions stop, says how, asks, and stops.
func runStop(cmd *cobra.Command, a *app.App, ask stopAsk) error {
	ctx, out, errOut := cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr()
	// What runs in each is read for the plan alone. A stop that says none,
	// the sign-out hook's or a script's, reads nothing inside first: the
	// probes would spend the time Windows waits for the hook.
	read := distros.Read
	if ask.json || ask.background {
		read = distros.List
	}
	ds, err := read(ctx, a.WSL, a.Config)
	if err != nil {
		return err
	}
	targets, notes, err := pickTargets(ds, ask.names, ask.all)
	if err != nil {
		return err
	}
	l := lookFor(out)
	if !ask.json && !ask.background {
		for _, n := range notes {
			fmt.Fprintln(out, l.muted(n))
		}
	}
	if len(targets) == 0 && !ask.shutdown {
		if !ask.json && !ask.background {
			fmt.Fprintln(out, "nothing to stop")
		}
		if ask.json {
			return writeJSON(out, ask.events, stopJSON(nil, false, nil))
		}
		return nil
	}
	o := stop.Options{Limit: time.Duration(a.Config.StopLimit) * time.Second, Resume: a.Config.Resume, By: ask.by, Shutdown: ask.shutdown}
	if !ask.json && !ask.background {
		writePlan(out, l, targets, o)
	}
	if err := needAnswer(ask.yes, "stop"); err != nil {
		return err
	}
	if !ask.yes {
		if err := confirm(cmd, "Stop "+joinNames(targets, ask.shutdown)+"? [y/N] ", "nothing stopped"); err != nil {
			return err
		}
	}
	// The terminal this runs in closes as its distribution stops: the stop
	// goes on by itself, and the history says how it went.
	if here := a.PC.TerminalDistro(); here != "" && !ask.background && (ask.shutdown || hasDistro(targets, here)) {
		args := []string{"stop", "--yes", "--background", "--by", ask.by}
		if ask.shutdown {
			args = append(args, "--shutdown")
		}
		for _, d := range targets {
			args = append(args, d.Name)
		}
		if len(targets) == 0 {
			// Nothing to stop first, WSL alone: the copy is asked for what
			// was, as one with no names and no --all is refused. What it
			// finds not running is a note, as it was here.
			if ask.all {
				args = append(args, "--all")
			} else {
				args = append(args, ask.names...)
			}
		}
		if err := a.PC.StartDetached(args); err != nil {
			return fmt.Errorf("could not go on without this terminal: %w", err)
		}
		if ask.json {
			res := stopJSON(nil, ask.shutdown, nil)
			res.Detached = here
			return writeJSON(out, ask.events, res)
		}
		fmt.Fprintln(out, "This terminal runs in "+here+", which is stopping: it closes as the distribution does.")
		fmt.Fprintln(out, l.muted("The stop goes on without it; wsl-spawner history shows how it went."))
		return nil
	}
	names := make([]string, len(targets))
	for i, d := range targets {
		names[i] = d.Name
	}
	width := 0
	for _, n := range names {
		width = max(width, len(n))
	}
	// The App's clock: the demo's made-up PC keeps its own time.
	started := a.Now()
	// A step's start says how many it stops; its end line says it again.
	counts := map[string]int{}
	results, runErr := stop.Run(ctx, a.WSL, names, o, a.Now, func(e stop.Event) {
		switch {
		case ask.events:
			_ = writeJSON(out, true, eventJSON(e))
		case !ask.json && !ask.background:
			writeEvent(out, l, width, counts, e)
		}
	})
	took := a.Now().Sub(started)
	// The history keeps it, whoever reads the terminal; a history that
	// cannot be written is said, and the stop's own result still counts.
	entry := stop.Entry(results, o, started, took, runErr, version.String())
	if ask.settingsErr != nil {
		// Stopped with the defaults: the entry says why, after any error
		// of the stop's own.
		note := "the settings did not load, so the defaults stood in: " + bareError(ask.settingsErr)
		if entry.Err != "" {
			note = entry.Err + "; " + note
		}
		entry.Err = note
	}
	// Every distribution was another stop's, which records it, and this one
	// did nothing else: there is nothing of its own to keep.
	if len(entry.Distros) > 0 || o.Shutdown || entry.Err != "" {
		if herr := history.Open(a.Dir).Add(entry); herr != nil && !ask.background {
			fmt.Fprintln(errOut, l.warn("! ")+"the history was not written: "+herr.Error())
		}
	}
	if ask.json {
		if err := writeJSON(out, ask.events, stopJSON(results, ask.shutdown, runErr)); err != nil {
			return err
		}
	} else if !ask.background {
		for _, r := range results {
			writeResult(out, l, r)
		}
	}
	if runErr != nil {
		return runErr
	}
	// One another stop is stopping is that stop's to end, and no step of
	// this one's that went wrong.
	if notClean(results) > 0 {
		return attention{errors.New(counted(notClean(results), "distribution") + " did not get every step in its time: wsl-spawner history shows the steps")}
	}
	return nil
}

// pickTargets is the distributions a stop is for: the ones named, or every
// running one of wsl-spawner's. A name WSL does not have, or one wsl-spawner
// leaves be, is a mistake in the command line; one that does not run is a
// note, as there is nothing to stop.
func pickTargets(ds []distros.Distro, names []string, all bool) ([]distros.Distro, []string, error) {
	var targets []distros.Distro
	var notes []string
	if !all && len(names) == 0 {
		running := []string{}
		for _, d := range ds {
			if d.Running && d.Ours() {
				running = append(running, d.Name)
			}
		}
		h := "nothing runs that wsl-spawner stops"
		if len(running) > 0 {
			h = "running: " + strings.Join(running, ", ")
		}
		return nil, nil, hint(fmt.Errorf("%w: name the distributions to stop, or give --all", errUsage), h)
	}
	if all {
		for _, d := range ds {
			if d.Running && d.Ours() {
				targets = append(targets, d)
			}
		}
		return targets, nil, nil
	}
	for _, name := range names {
		i := slices.IndexFunc(ds, func(d distros.Distro) bool { return strings.EqualFold(d.Name, name) })
		if i < 0 {
			return nil, nil, fmt.Errorf("%w: %s", wsl.ErrNoDistro, name)
		}
		d := ds[i]
		if why := notOurs(d); why != "" {
			return nil, nil, hint(fmt.Errorf("%w: %s", errUsage, why), howToStop(d))
		}
		if !d.Running {
			notes = append(notes, d.Name+" is not running")
			continue
		}
		if !hasDistro(targets, d.Name) {
			targets = append(targets, d)
		}
	}
	return targets, notes, nil
}

// notOurs says why wsl-spawner does not stop a distribution, or "".
func notOurs(d distros.Distro) string {
	switch d.Kind() {
	case distros.KindOwned:
		return d.Name + " is " + d.Owner + "'s, which stops it itself"
	case distros.KindSkipped:
		return d.Name + " is left to WSL in the settings"
	case distros.KindWSL1:
		return d.Name + " is WSL 1, which has no services to give time to"
	}
	return ""
}

// howToStop says what does stop a distribution wsl-spawner does not.
func howToStop(d distros.Distro) string {
	switch d.Kind() {
	case distros.KindSkipped:
		return "wsl-spawner set " + d.Name + " --skip=false makes it wsl-spawner's to stop"
	case distros.KindOwned:
		return "quit " + d.Owner + " to stop it"
	}
	return "wsl --terminate " + d.Name + " stops it"
}

// hasDistro reports whether ds holds the distribution named name.
func hasDistro(ds []distros.Distro, name string) bool {
	return slices.ContainsFunc(ds, func(d distros.Distro) bool { return strings.EqualFold(d.Name, name) })
}

// joinNames names the distributions for the question.
func joinNames(ds []distros.Distro, shutdown bool) string {
	names := make([]string, len(ds))
	for i, d := range ds {
		names[i] = d.Name
	}
	s := strings.Join(names, ", ")
	if shutdown {
		if s == "" {
			return "WSL"
		}
		s += " and then WSL"
	}
	return s
}

// writePlan says what the stop will do, before it is asked.
func writePlan(out io.Writer, l look, ds []distros.Distro, o stop.Options) {
	if len(ds) > 0 {
		fmt.Fprintf(out, "Each service and container gets the time it asks for, %d s at most together:\n", int(o.Limit/time.Second))
		width := 0
		for _, d := range ds {
			width = max(width, len(d.Name))
		}
		for _, d := range ds {
			fmt.Fprintln(out, "  "+d.Name+strings.Repeat(" ", width-len(d.Name))+"  "+planWords(d.Probe))
		}
		fmt.Fprintln(out, l.muted("Then WSL ends what is left in each."))
	}
	if o.Shutdown {
		fmt.Fprintln(out, "Then WSL itself stops, as wsl --shutdown does: every distribution, wsl-spawner's or not.")
	}
}

// planWords says what one distribution's steps are, from its probe.
func planWords(p *inside.Probe) string {
	if p == nil {
		return "what runs in it could not be read: its steps are found as it stops"
	}
	var parts []string
	if n := len(p.Hooks); n > 0 {
		parts = append(parts, counted(n, "hook"))
	}
	// The units a stop takes: those that run now.
	if n := len(p.TakenStopUnits()); n > 0 {
		parts = append(parts, counted(n, "unit"))
	}
	for _, e := range []string{"docker", "podman"} {
		up, ok := p.Engines[e]
		if !ok || !up {
			continue
		}
		running, longest := 0, time.Duration(0)
		for _, c := range p.Containers {
			if c.Engine == e && c.Running() {
				running++
				longest = max(longest, c.Wait())
			}
		}
		w := e + ": " + counted(running, "container")
		if running > 0 {
			w += fmt.Sprintf(", the longest stop %d s", int(longest/time.Second))
		}
		parts = append(parts, w)
	}
	if len(parts) == 0 {
		return "nothing of its own to stop first"
	}
	return strings.Join(parts, " · ")
}

// writeEvent prints one thing that happened, after the distribution's name.
// counts keeps each step's count from its start for its end, by
// distribution and step.
func writeEvent(out io.Writer, l look, width int, counts map[string]int, e stop.Event) {
	name := func(n string) string { return n + strings.Repeat(" ", max(0, width-len(n))) + "  " }
	switch e.Kind {
	case "line":
		ln := e.Line
		key := e.Distro + "/" + ln.Step + "/" + ln.Hook
		if ln.Kind == "start" {
			counts[key] = ln.Count
		} else {
			ln.Count = counts[key]
		}
		what := stepWords(ln)
		switch ln.Kind {
		case "start":
			// The sync takes no time worth a line of its own.
			if ln.Step != inside.StepSync {
				fmt.Fprintln(out, name(e.Distro)+what+l.muted(" …"))
			}
		case "done":
			end := l.muted(fmt.Sprintf("done, %d s", int(ln.Took/time.Second)))
			switch ln.Code {
			case 0:
			case 124, 137:
				end = l.warn(fmt.Sprintf("out of time, %d s", int(ln.Took/time.Second)))
			default:
				end = l.bad(fmt.Sprintf("failed with %d, %d s", ln.Code, int(ln.Took/time.Second)))
			}
			fmt.Fprintln(out, name(e.Distro)+what+"  "+end)
		}
	case "terminate":
		if !e.Done {
			fmt.Fprintln(out, name(e.Distro)+"WSL ends the rest"+l.muted(" …"))
		}
	case "finished":
		// One another stop is stopping is said as soon as it is known: a
		// shutdown waits for that stop before WSL stops.
		if e.Result != nil && e.Result.Busy() {
			fmt.Fprintln(out, name(e.Distro)+l.muted("another stop of "+e.Distro+" is running, and stops it"))
		}
	case "shutdown":
		if !e.Done {
			fmt.Fprintln(out, "WSL stops"+l.muted(" …"))
		}
	}
}

// stepWords names a step of the stop script.
func stepWords(ln inside.StopLine) string {
	switch ln.Step {
	case inside.StepHook:
		return "hook " + ln.Hook
	case inside.StepUnits:
		if ln.Count > 0 {
			return counted(ln.Count, "unit")
		}
		return "units"
	case inside.StepDocker, inside.StepPodman:
		return ln.Step + ": " + counted(ln.Count, "container")
	case inside.StepRecord:
		// Said only when a file of the stop's own could not be written.
		return "notes in /var/lib/wsl-spawner"
	}
	return ln.Step
}

// writeResult says how one distribution's stop went.
func writeResult(out io.Writer, l look, r stop.Result) {
	secs := fmt.Sprintf("%d s", int(r.Took.Round(time.Second)/time.Second))
	switch {
	case r.Busy():
		// Said as it came, as another stop's to end.
	case r.Clean():
		fmt.Fprintln(out, l.done(r.Distro+" stopped, every step in its time, "+secs))
	case r.Err != nil:
		fmt.Fprintln(out, l.bad("✗")+" "+r.Distro+": "+firstLine(r.Err.Error()))
	case r.End == "limit":
		fmt.Fprintln(out, l.warn("!")+" "+r.Distro+" stopped, a step ran out of the time left, "+secs)
	default:
		fmt.Fprintln(out, l.warn("!")+" "+r.Distro+" stopped, a step failed, "+secs)
	}
}

// notClean counts the stops that were not clean. One another stop is
// stopping is that stop's, and not counted.
func notClean(rs []stop.Result) int {
	n := 0
	for _, r := range rs {
		if !r.Clean() && !r.Busy() {
			n++
		}
	}
	return n
}

// eventJSON is an event as --events prints it.
func eventJSON(e stop.Event) api.StopEvent {
	o := api.StopEvent{Event: e.Kind, Distro: e.Distro, Done: e.Done}
	if e.Kind == "line" {
		o.Kind, o.Step, o.Hook, o.Count, o.Code, o.Took = e.Line.Kind, e.Line.Step, e.Line.Hook, e.Line.Count, e.Line.Code, e.Line.Took.Seconds()
		if e.Line.Kind == "end" {
			o.Result = e.Line.Result
		}
	}
	if e.Result != nil {
		o.Result, o.Took = e.Result.End, e.Result.Took.Seconds()
		if e.Result.Err != nil {
			o.Err = e.Result.Err.Error()
		}
	}
	if e.Err != nil {
		o.Err = e.Err.Error()
	}
	return o
}

// writeJSON prints v as JSON: on one line where lines come one by one, as
// --events prints them, indented otherwise.
func writeJSON(w io.Writer, oneLine bool, v any) error {
	if !oneLine {
		return printJSON(w, v)
	}
	return json.NewEncoder(w).Encode(v)
}

// stopJSON is the stop's results as JSON gives them.
func stopJSON(rs []stop.Result, shutdown bool, err error) api.StopResult {
	o := api.StopResult{Schema: schema, Results: []api.DistroStop{}, Shutdown: shutdown}
	for _, r := range rs {
		ro := api.DistroStop{Name: r.Distro, Result: r.End, Clean: r.Clean(), Took: r.Took.Seconds(), Steps: r.Steps}
		if ro.Steps == nil {
			ro.Steps = []history.Step{}
		}
		if r.Err != nil {
			ro.Err = r.Err.Error()
		}
		o.Results = append(o.Results, ro)
	}
	if err != nil {
		o.Err = err.Error()
	}
	return o
}
