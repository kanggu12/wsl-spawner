package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/kanggu12/wsl-spawner/internal/front/ui"

	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"

	"github.com/spf13/cobra"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/distros"
	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/version"
)

// statusCmd prints every distribution with what wsl-spawner does with it.
func statusCmd(p *apps) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "List the distributions: running or not, what wsl-spawner does with each, how each last stopped",
		Long: "status lists every WSL distribution: whether it runs, what wsl-spawner does\n" +
			"with it, whether it starts at sign-in and is kept running, its containers, and\n" +
			"how its last start ended. A distribution that does not run is not started to\n" +
			"be read.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := p.get()
			if err != nil {
				return err
			}
			return status(cmd.Context(), a, cmd.OutOrStdout(), asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

// status prints the distributions as a table, in English, or as JSON.
func status(ctx context.Context, a *app.App, out io.Writer, asJSON bool) error {
	// Timed on the App's clock, as the check's: a distribution's clock is
	// set beside it, for what ended a start that was cut.
	ds, err := distros.ReadWithClock(ctx, a.WSL, a.Config, a.Now)
	if err != nil {
		return err
	}
	info, _ := a.WSL.Info(ctx)
	// What ended each start that was cut, where it can be told.
	causes := cutCauses(ctx, a, ds)
	if asJSON {
		return printJSON(out, statusJSON(info.Version, ds, a.Config, causes))
	}
	// The commands print English, for scripts: only the overview speaks
	// the person's language.
	was := i18n.Current()
	i18n.Set(i18n.English)
	defer i18n.Set(was)
	// The history fills in a last stop nothing inside a distribution notes;
	// one that cannot be read leaves those dashes.
	hist, _ := history.Open(a.Dir).All()
	// Every column, however wide: a script reads them all.
	return writeStatus(out, lookFor(out), info.Version, ds, hist, causes, 0)
}

// overview is what a bare start prints in CLI mode: the table in the
// person's language, then the commands to use next, with the names of this
// PC's distributions in them, all in width cells, the terminal's.
func overview(ctx context.Context, a *app.App, out io.Writer, l look, width int) error {
	ds, err := distros.ReadWithClock(ctx, a.WSL, a.Config, a.Now)
	if err != nil {
		return err
	}
	info, _ := a.WSL.Info(ctx)
	hist, _ := history.Open(a.Dir).All()
	if err := writeStatus(out, l, info.Version, ds, hist, cutCauses(ctx, a, ds), width); err != nil {
		return err
	}
	var next [][2]string
	if h, err := readHook(a.PC); err == nil && !h.last() {
		next = append(next, [2]string{"wsl-spawner hook install", i18n.T("next.hook")})
	}
	for _, d := range ds {
		if !d.Ours() {
			continue
		}
		name := ui.ShellArg(d.Name)
		switch {
		case d.Running && d.Probe != nil && !d.Probe.SetUp():
			next = append(next, [2]string{"wsl-spawner set " + name + " --managed", i18n.T("next.setup")})
		case d.Running:
			next = append(next, [2]string{"wsl-spawner stop " + name, i18n.T("next.stop")})
		default:
			next = append(next, [2]string{"wsl-spawner start " + name, i18n.T("next.start")})
		}
	}
	next = append(next, [2]string{"wsl-spawner check", i18n.T("next.check")}, [2]string{"wsl-spawner tui", i18n.T("next.tui")})
	fmt.Fprintln(out)
	fmt.Fprintln(out, l.bold(i18n.T("next.title")))
	writeNext(out, l, next, width)
	return nil
}

// minWords is the least room the words of a command to use next get beside
// it: with less, they stand on lines of their own under it.
const minWords = 32

// writeNext prints the commands to use next, each with what it does, in
// width cells: the words wrap under their first line, or, where there is
// no room beside the commands, stand under each, indented.
func writeNext(out io.Writer, l look, next [][2]string, width int) {
	cmdW := 0
	for _, n := range next {
		cmdW = max(cmdW, ansi.StringWidth(n[0]))
	}
	// lead is where the words start beside the commands: two cells in, the
	// command, two cells.
	lead := 2 + cmdW + 2
	for _, n := range next {
		if width-lead < minWords {
			// Too narrow: the command alone, then its words under it.
			fmt.Fprintln(out, "  "+n[0])
			for _, line := range wrapWords(n[1], width-4) {
				fmt.Fprintln(out, "    "+l.muted(line))
			}
			continue
		}
		for i, line := range wrapWords(n[1], width-lead) {
			// The first line beside the command, the rest under the first.
			start := "  " + n[0] + strings.Repeat(" ", cmdW-ansi.StringWidth(n[0])) + "  "
			if i > 0 {
				start = strings.Repeat(" ", lead)
			}
			fmt.Fprintln(out, start+l.muted(line))
		}
	}
}

// wrapWords wraps words to w cells, breaking only at spaces: a word that
// holds a hyphen, as wsl-spawner does, stays whole.
func wrapWords(s string, w int) []string {
	// A hyphen that does not break stands in for each one while wrapping.
	const hyphen = "‑"
	wrapped := ansi.Wordwrap(strings.ReplaceAll(s, "-", hyphen), max(10, w), "")
	return strings.Split(strings.ReplaceAll(wrapped, hyphen, "-"), "\n")
}

// writeStatus prints the headline and the table, in width cells when width
// is more than 0: the columns go as the screen's do, the least needed
// first. causes are what ended the starts that were cut (cutCauses).
func writeStatus(out io.Writer, l look, wslVersion string, ds []distros.Distro, hist []history.Entry, causes map[string]inside.Cause, width int) error {
	head := l.bold("wsl-spawner " + version.String())
	if wslVersion != "" {
		head += l.muted(" · WSL " + wslVersion)
	}
	fmt.Fprintln(out, head)
	if len(ds) == 0 {
		fmt.Fprintln(out, l.muted("no WSL distribution is registered"))
		return nil
	}
	fmt.Fprintln(out)
	// The screen's columns, in its order.
	rows := [][]string{{
		l.muted(strings.ToUpper(i18n.T("col.distro"))), l.muted(strings.ToUpper(i18n.T("col.state"))),
		l.muted(strings.ToUpper(i18n.T("col.kind"))), l.muted(strings.ToUpper(i18n.T("col.containers"))),
		l.muted(strings.ToUpper(i18n.T("col.laststop"))),
	}}
	for _, d := range ds {
		rows = append(rows, statusRow(l, d, hist, causes[d.Name]))
	}
	if width > 0 {
		rows = fitColumns(rows, width)
	}
	if err := writeTable(out, nil, rows); err != nil {
		return err
	}
	// A probe that failed says why, under the table, once per distribution.
	for _, d := range ds {
		if d.ProbeErr != nil {
			fmt.Fprintln(out, l.warn("! ")+d.Name+": "+firstLine(d.ProbeErr.Error()))
		}
	}
	return nil
}

// fitColumns drops columns of the status table until it fits width cells,
// in the order the screen's table drops them: the containers, the last
// stop, then what wsl-spawner does. The name and the state stay.
func fitColumns(rows [][]string, width int) [][]string {
	keep := []bool{true, true, true, true, true}
	// tableWidth is the table's width with the columns kept, two cells
	// between each two, as writeTable prints it.
	tableWidth := func() int {
		total, n := 0, 0
		for c := range keep {
			if !keep[c] {
				continue
			}
			cw := 0
			for _, r := range rows {
				if c < len(r) {
					cw = max(cw, ansi.StringWidth(r[c]))
				}
			}
			total += cw
			n++
		}
		return total + 2*(n-1)
	}
	for _, c := range []int{3, 4, 2} {
		if tableWidth() <= width {
			break
		}
		keep[c] = false
	}
	out := make([][]string, len(rows))
	for i, r := range rows {
		for c, cell := range r {
			if c < len(keep) && keep[c] {
				out[i] = append(out[i], cell)
			}
		}
	}
	return out
}

// statusRow is one distribution's row of the table; cause is what ended
// its last start, when that was cut and it is known.
func statusRow(l look, d distros.Distro, hist []history.Entry, cause inside.Cause) []string {
	state := l.muted(i18n.T("state.stopped"))
	if d.Running {
		state = l.running(i18n.T("state.running"))
	}
	row := []string{d.Name, state, kindWords(l, d)}
	// A distribution wsl-spawner leaves be has nothing more to say.
	if !d.Ours() {
		return append(row, "", "")
	}
	pr := d.Probe
	if pr == nil {
		// Not running, or not read: what runs in it is not known; its last
		// stop may be in the history.
		return append(row, l.muted("–"), lastStopWords(l, d.Name, hist, l.muted("–")))
	}
	end := endWords(l, *pr, cause)
	// Nothing noted inside, or only this start, the first since it was set
	// up: wsl-spawner's last stop of it is the history's, as on the screen.
	if e, _ := pr.LastEnd(); e == inside.EndUnknown || e == inside.EndFirst {
		end = lastStopWords(l, d.Name, hist, end)
	}
	return append(row, containerWords(l, *pr), end)
}

// lastStopWords is a distribution's newest stop in the history, as the
// screen's table says it, or none when there is none.
func lastStopWords(l look, name string, hist []history.Entry, none string) string {
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].What != "stop" {
			continue
		}
		for _, d := range hist[i].Distros {
			if d.Name != name {
				continue
			}
			s := i18n.T("end." + d.Result)
			switch d.Result {
			case "clean":
				s = l.clean(s)
			case "limit", "failed":
				s = l.warn(s)
			default:
				s = l.bad(s)
			}
			return s + l.muted(" "+hist[i].At.Local().Format("01-02 15:04"))
		}
	}
	return none
}

// kindWords says how far wsl-spawner goes with a distribution, as the
// screen's table says it: stop only, set up, kept running or at sign-in,
// or who it is left to. One stopped is not read: a dash, unless the
// settings start it at sign-in, which holds the other two.
func kindWords(l look, d distros.Distro) string {
	switch d.Kind() {
	case distros.KindSkipped:
		return l.muted(i18n.T("kind.skipped"))
	case distros.KindOwned:
		return l.muted(i18n.T("kind.owned", d.Owner))
	case distros.KindWSL1:
		return l.muted(i18n.T("kind.wsl1"))
	}
	lv := 0
	switch pr := d.Probe; {
	case pr != nil && pr.SetUp():
		lv = 1
		if pr.KeptRunning() {
			lv = 2
			if d.AtLogon {
				lv = 3
			}
		}
	case pr == nil && d.AtLogon:
		lv = 3
	case pr == nil:
		return l.muted("–")
	}
	if lv == 0 {
		return i18n.T("level.0")
	}
	return l.clean(i18n.T(fmt.Sprintf("level.%d", lv)))
}

// containerWords counts the running containers, and those to come back.
func containerWords(l look, p inside.Probe) string {
	// An engine that does not answer is worth a look, before all. One
	// asleep behind its socket, or another's daemon, is not: said quietly
	// where no engine of the distribution's own runs.
	quiet := ""
	for _, e := range []string{"docker", "podman"} {
		switch p.EngineState(e) {
		case inside.EngineDown:
			if _, found := p.Engines[e]; found {
				return l.warn(e + " down")
			}
		case inside.EngineAsleep:
			quiet = e + " asleep"
		case inside.EngineForeign:
			if quiet == "" {
				quiet = e + " not its own"
			}
		}
	}
	up := false
	for _, isUp := range p.Engines {
		up = up || isUp
	}
	switch {
	case !up && quiet != "":
		return l.muted(quiet)
	case !up:
		return l.muted("–")
	}
	running := 0
	for _, c := range p.Containers {
		if c.Running() {
			running++
		}
	}
	s := fmt.Sprintf("%d/%d", running, len(p.Containers))
	if n := len(p.Bring); n > 0 {
		s += l.starting(fmt.Sprintf(" +%d", n))
	}
	return s
}

// endWords says how the start before this one ended, and when; for one
// that was cut, what ended it where cause says.
func endWords(l look, p inside.Probe, cause inside.Cause) string {
	end, prev := p.LastEnd()
	words := map[inside.Ending]string{
		inside.EndUnknown: "end.unknown", inside.EndFirst: "end.first", inside.EndClean: "end.clean",
		inside.EndLimit: "end.limit", inside.EndFailed: "end.failed", inside.EndCut: "end.cut",
	}
	s := i18n.T(words[end])
	switch end {
	case inside.EndClean, inside.EndLimit, inside.EndFailed:
		if end == inside.EndClean {
			s = l.clean(s)
		} else {
			s = l.warn(s)
		}
		// When wsl-spawner's stop ended.
		if p.LastStop != nil && !p.LastStop.At.IsZero() {
			s += l.muted(" " + p.LastStop.At.Local().Format("01-02 15:04"))
		}
		return s
	case inside.EndCut:
		s = l.bad(s)
		// What ended it, and when, where its journal and Windows' logs
		// tell it.
		if words, when := causeWords(cause); words != "" {
			return s + l.muted(": "+words+" "+when)
		}
		// When it ended is not known: when the start it ended began is.
		if !prev.At.IsZero() {
			s += l.muted(" " + i18n.T("end.since", prev.At.Local().Format("01-02 15:04")))
		}
		return s
	}
	return l.muted(s)
}

// firstLine is a message's first line.
func firstLine(s string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(first)
}

// kindNames are the kinds as JSON names them.
var kindNames = map[distros.Kind]string{
	distros.KindSetUp: "set-up", distros.KindStopOnly: "stop-only", distros.KindSkipped: "skipped",
	distros.KindOwned: "owned", distros.KindWSL1: "wsl1", distros.KindNotRead: "not-read",
}

// endNames are the endings as JSON names them.
var endNames = map[inside.Ending]string{
	inside.EndUnknown: "unknown", inside.EndFirst: "first", inside.EndClean: "clean",
	inside.EndLimit: "limit", inside.EndFailed: "failed", inside.EndCut: "cut",
}

// statusJSON is the status as JSON gives it; causes are what ended the
// starts that were cut (cutCauses).
func statusJSON(wslVersion string, ds []distros.Distro, c config.Config, causes map[string]inside.Cause) api.Status {
	out := api.Status{Schema: schema, WSL: wslVersion, List: []api.Distro{}, StopLimit: c.StopLimit, Resume: c.Resume}
	for _, d := range ds {
		o := api.Distro{Name: d.Name, Default: d.Default, Version: d.Version, Running: d.Running,
			Kind: kindNames[d.Kind()], Owner: d.Owner, AtLogon: d.AtLogon}
		if d.ProbeErr != nil {
			o.ProbeError = firstLine(d.ProbeErr.Error())
		}
		if pr := d.Probe; pr != nil {
			po := &api.Probe{Systemd: pr.Systemd, Interop: pr.Interop, KeptRunning: pr.KeptRunning(), SetUp: pr.SetUp(),
				LiveRestore: pr.LiveRestore, Engines: pr.Engines, Containers: []api.Container{},
				Hooks: nonNil(pr.Hooks), Failed: nonNil(pr.Failed), Bring: []string{}, Complete: pr.Complete}
			for _, c := range pr.Containers {
				po.Containers = append(po.Containers, api.Container{Engine: c.Engine, ID: c.ID, Name: c.Name, State: c.State,
					Policy: c.Policy, StopSeconds: int(c.Wait() / time.Second), Image: c.Image, Project: c.Project, AutoRemove: c.AutoRemove})
			}
			for _, b := range pr.Bring {
				po.Bring = append(po.Bring, b.Name)
			}
			// The units a stop takes, and those it passes by.
			po.StopUnits = nonNil(pr.TakenStopUnits())
			po.StopUnitsSkipped = []string{}
			for _, u := range pr.StopUnits {
				if !slices.Contains(po.StopUnits, u) {
					po.StopUnitsSkipped = append(po.StopUnitsSkipped, u)
				}
			}
			// Each engine by its word, the ones left alone too.
			po.EngineStates = map[string]string{}
			for _, e := range []string{"docker", "podman"} {
				if _, found := pr.EngineStates[e]; found {
					po.EngineStates[e] = pr.EngineState(e)
				} else if _, found := pr.Engines[e]; found {
					po.EngineStates[e] = pr.EngineState(e)
				}
			}
			if s := pr.LastStop; s != nil {
				po.LastStop = &api.LastStop{At: s.At.UTC(), Took: s.Took.Seconds(), Result: s.Result, By: s.By}
			}
			po.Resumed = []api.Resumed{}
			for _, r := range pr.Resumed {
				po.Resumed = append(po.Resumed, api.Resumed{Engine: r.Engine, Name: r.Name, What: r.What})
			}
			if !pr.ResumedAt.IsZero() {
				at := pr.ResumedAt.UTC()
				po.ResumedAt = &at
			}
			end, prev := pr.LastEnd()
			po.LastEnd.How = endNames[end]
			if !prev.At.IsZero() {
				at := prev.At.UTC()
				po.LastEnd.Start = &at
			}
			// What ended a start that was cut, where it is known.
			if cause, ok := causes[d.Name]; ok && end == inside.EndCut && cause.Kind != "" {
				at := cause.At.UTC()
				po.LastEnd.Cause, po.LastEnd.CauseAt = cause.Kind, &at
			}
			o.Probe = po
		}
		out.List = append(out.List, o)
	}
	return out
}

// nonNil is a list that prints as [] when empty, not null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
