package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/kanggu12/wsl-spawner/internal/front/api"

	"github.com/spf13/cobra"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/distros"
	"github.com/kanggu12/wsl-spawner/internal/engine/setup"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// logonName is the name of wsl-spawner's sign-in entry, as Task Manager's
// Startup apps lists it.
const logonName = "wsl-spawner"

// setCmd changes what wsl-spawner does with one distribution.
func setCmd(p *apps) *cobra.Command {
	var managed, keep, logon, skip, yes, asJSON bool
	c := &cobra.Command{
		Use:   "set distribution [--managed] [--keep] [--logon] [--skip]",
		Short: "Choose what wsl-spawner does with a distribution: set up in it, kept running, started at sign-in",
		Long: "set changes what wsl-spawner does with one distribution. Each flag is a choice,\n" +
			"on as it is (--keep) or off with =false (--keep=false):\n\n" +
			"  --managed  wsl-spawner set up in it: a service notes each start and starts again\n" +
			"             the containers the last stop noted\n" +
			"  --keep     kept running with nothing open in it: a service holds Windows'\n" +
			"             waitfor.exe, and WSL keeps a distribution up while it runs\n" +
			"  --logon    started when you sign in to Windows, by a sign-in program Task\n" +
			"             Manager's Startup apps lists\n" +
			"  --skip     left to WSL: wsl-spawner neither stops nor starts it\n\n" +
			"Starting at sign-in keeps it running, which needs wsl-spawner set up in it; left\n" +
			"to WSL, it has none of these. A change inside a stopped distribution starts it.\n" +
			"It says what changes and asks first; --yes does it without the question.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("%w: set takes one distribution", errUsage)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var ask setup.Ask
			flag := func(name string, v *bool) *bool {
				if cmd.Flags().Changed(name) {
					return v
				}
				return nil
			}
			ask.Managed, ask.Keep, ask.Logon, ask.Skip = flag("managed", &managed), flag("keep", &keep), flag("logon", &logon), flag("skip", &skip)
			if ask == (setup.Ask{}) {
				return hint(fmt.Errorf("%w: set needs a choice", errUsage), "wsl-spawner set "+args[0]+" --managed, --keep, --logon or --skip, each =false to turn off")
			}
			a, err := p.get()
			if err != nil {
				return err
			}
			return runSet(cmd, a, args[0], ask, yes, asJSON, false)
		},
	}
	c.Flags().BoolVar(&managed, "managed", false, "wsl-spawner set up in it (=false takes it out)")
	c.Flags().BoolVar(&keep, "keep", false, "keep it running with nothing open in it")
	c.Flags().BoolVar(&logon, "logon", false, "start it when you sign in to Windows")
	c.Flags().BoolVar(&skip, "skip", false, "leave it to WSL: wsl-spawner neither stops nor starts it")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "change it without asking")
	c.Flags().BoolVar(&asJSON, "json", false, "print the distribution's choices after, as JSON")
	return c
}

// runSet works out what the choices change, says it, asks, and does it.
// asked is set once the change was said and answered, before the
// distribution was started to be read: it is then not said again.
func runSet(cmd *cobra.Command, a *app.App, name string, ask setup.Ask, yes, asJSON, asked bool) error {
	ctx, out := cmd.Context(), cmd.OutOrStdout()
	// With --json, the plan and the done line go nowhere: the JSON says.
	shown := out
	if asJSON {
		shown = io.Discard
	}
	d, err := oneDistro(ctx, a, name)
	if err != nil {
		return err
	}
	if d.Owner != "" || d.Version == 1 {
		return hint(fmt.Errorf("%w: %s", errUsage, notOurs(d)), howToStop(d))
	}
	// Not read, as it does not run: what is set up in it is not known. A
	// change that may touch what is in it is said as asked, and made once
	// it runs and is read.
	if d.Probe == nil && !asked && insideAsked(ask) {
		return runSetUnread(cmd, a, d, ask, yes, asJSON, shown)
	}
	cur := stateOf(d)
	want, err := setup.Want(cur, ask)
	if err != nil {
		return fmt.Errorf("%w: %s do not go together: %w", errUsage, askedFlags(ask), err)
	}
	result := func(s setup.State, changed bool) error {
		if !asJSON {
			return nil
		}
		return printJSON(out, api.SetResult{Schema: schema, Name: d.Name, Changed: changed, Managed: s.Managed, Keep: s.Keep, Logon: s.Logon, Skip: s.Skip})
	}
	// The choice may be as asked while the sign-in program is not: missing
	// after a write that failed, removed by hand, starting another copy, or
	// left over. --logon, or --logon=false, puts it as the settings say, as
	// check tells the person to.
	repair := ""
	if want == cur && ask.Logon != nil {
		if repair, err = logonRepair(a.PC, a.Config); err != nil {
			return err
		}
	}
	if want == cur && repair == "" {
		fmt.Fprintln(shown, d.Name+": nothing to change")
		return result(cur, false)
	}
	// The sign-in program the change ends with, made by this copy while a
	// distribution starts at sign-in: one this copy cannot make is said
	// before the question, and before the settings change.
	if err := logonCheck(a.PC, a.Config, d.Name, cur, want); err != nil {
		return err
	}
	l := lookFor(out)
	inside := setup.Inside(cur, want)
	if !asked {
		fmt.Fprintln(shown, "For "+d.Name+":")
		for _, line := range append(changeWords(cur, want), repair) {
			// A choice that changes needs no repair line: its own says it.
			if line != "" {
				fmt.Fprintln(shown, "  "+line)
			}
		}
		if inside && !d.Running {
			fmt.Fprintln(shown, l.muted("It is not running: it starts first."))
		}
		if err := needAnswer(yes, "change it"); err != nil {
			return err
		}
		if !yes {
			if err := confirm(cmd, "Change "+d.Name+"? [y/N] ", "nothing changed"); err != nil {
				return err
			}
		}
	}
	if inside {
		if !d.Running {
			if err := a.WSL.Start(ctx, d.Name); err != nil {
				return fmt.Errorf("starting %s: %w", d.Name, err)
			}
			// What runs in it now, which says what is set up already.
			if d, err = oneDistro(ctx, a, d.Name); err != nil {
				return err
			}
			cur.Managed, cur.Keep = stateOf(d).Managed, stateOf(d).Keep
		}
		if want.Managed {
			err = setup.In(ctx, a.WSL, d.Name, want.Keep)
		} else if cur.Managed {
			err = setup.Out(ctx, a.WSL, d.Name)
		}
		if err != nil {
			return insideHint(err, d.Name)
		}
	}
	if err := saveChoices(a, d.Name, want); err != nil {
		return err
	}
	if err := syncLogonEntry(a.PC, a.Config); err != nil {
		return err
	}
	sayDone(shown, d.Name+": "+summary(want))
	return result(want, true)
}

// askedFlags names the choices asked as the flags that asked them, for a
// refusal: "--keep=false and --logon".
func askedFlags(ask setup.Ask) string {
	var fs []string
	for _, f := range []struct {
		name string
		v    *bool
	}{{"--managed", ask.Managed}, {"--keep", ask.Keep}, {"--logon", ask.Logon}, {"--skip", ask.Skip}} {
		switch {
		case f.v == nil:
			// Not asked: not part of it.
		case *f.v:
			fs = append(fs, f.name)
		default:
			fs = append(fs, f.name+"=false")
		}
	}
	if len(fs) < 2 {
		return strings.Join(fs, "")
	}
	return strings.Join(fs[:len(fs)-1], ", ") + " and " + fs[len(fs)-1]
}

// insideAsked reports whether a change may touch what is set up inside a
// distribution: setting it up or taking it out, keeping it running or not,
// leaving it to WSL, which takes out what is set up, or starting it at
// sign-in, which sets it up and keeps it running.
func insideAsked(ask setup.Ask) bool {
	on := func(p *bool) bool { return p != nil && *p }
	return ask.Managed != nil || ask.Keep != nil || on(ask.Skip) || on(ask.Logon)
}

// runSetUnread is set on a distribution not read, as it does not run: the
// change is said as asked, each choice as though it were the other way now,
// and asked; then the distribution is started, read, and only what is not
// so already changes.
func runSetUnread(cmd *cobra.Command, a *app.App, d distros.Distro, ask setup.Ask, yes, asJSON bool, shown io.Writer) error {
	ctx, l := cmd.Context(), lookFor(cmd.OutOrStdout())
	assumed := stateOf(d)
	want, err := setup.Want(assumed, ask)
	if err != nil {
		return fmt.Errorf("%w: %s do not go together: %w", errUsage, askedFlags(ask), err)
	}
	// What the change turns, asked or needed by what was, is said as a
	// change from the other way: what is in it now is not known yet.
	on := func(p *bool) bool { return p != nil && *p }
	off := func(p *bool) bool { return p != nil && !*p }
	if ask.Managed != nil || on(ask.Skip) || on(ask.Logon) || on(ask.Keep) {
		assumed.Managed = !want.Managed
	}
	if ask.Keep != nil || on(ask.Skip) || on(ask.Logon) || off(ask.Managed) {
		assumed.Keep = !want.Keep
		// Kept running or not alone: only one set up is kept running.
		if off(ask.Keep) && ask.Managed == nil && !on(ask.Skip) {
			assumed.Managed, want.Managed = true, true
		}
	}
	// A sign-in program this copy cannot make is said before the question.
	if err := logonCheck(a.PC, a.Config, d.Name, assumed, want); err != nil {
		return err
	}
	fmt.Fprintln(shown, "For "+d.Name+":")
	for _, line := range changeWords(assumed, want) {
		fmt.Fprintln(shown, "  "+line)
	}
	if d.Running {
		fmt.Fprintln(shown, l.muted("What runs in it is read again first; only what is not so already changes."))
	} else {
		fmt.Fprintln(shown, l.muted("It is not running: it starts first, and only what is not so already changes."))
	}
	if err := needAnswer(yes, "change it"); err != nil {
		return err
	}
	if !yes {
		if err := confirm(cmd, "Change "+d.Name+"? [y/N] ", "nothing changed"); err != nil {
			return err
		}
	}
	if !d.Running {
		if err := a.WSL.Start(ctx, d.Name); err != nil {
			return fmt.Errorf("starting %s: %w", d.Name, err)
		}
	}
	read, err := oneDistro(ctx, a, d.Name)
	if err != nil {
		return err
	}
	if read.Probe == nil {
		why := "it did not answer"
		if read.ProbeErr != nil {
			why = read.ProbeErr.Error()
		}
		return fmt.Errorf("what runs in %s could not be read: %s", d.Name, why)
	}
	return runSet(cmd, a, d.Name, ask, true, asJSON, true)
}

// oneDistro reads the distribution named name, probed when it runs.
func oneDistro(ctx context.Context, a *app.App, name string) (distros.Distro, error) {
	ds, err := distros.Read(ctx, a.WSL, a.Config)
	if err != nil {
		return distros.Distro{}, err
	}
	i := slices.IndexFunc(ds, func(d distros.Distro) bool { return strings.EqualFold(d.Name, name) })
	if i < 0 {
		return distros.Distro{}, fmt.Errorf("%w: %s", wsl.ErrNoDistro, name)
	}
	return ds[i], nil
}

// stateOf is a distribution's choices as they are: inside it as its probe
// read, in the settings for the rest.
func stateOf(d distros.Distro) setup.State {
	s := setup.State{Logon: d.AtLogon, Skip: d.Skipped}
	if d.Probe != nil {
		s.Managed, s.Keep = d.Probe.SetUp(), d.Probe.KeptRunning()
	}
	return s
}

// changeWords says each change going from cur to w does, one a line.
func changeWords(cur, w setup.State) []string {
	var out []string
	switch {
	case w.Managed && !cur.Managed:
		out = append(out, "set wsl-spawner up in it: a service notes each start and starts again the containers the last stop noted")
	case !w.Managed && cur.Managed:
		out = append(out, "take wsl-spawner out of it: its services, its start script and what it noted; your hooks and stop-units stay")
	}
	switch {
	case w.Keep && !cur.Keep:
		out = append(out, "keep it running: a service holds Windows' waitfor.exe, and WSL keeps it up while that runs")
	case !w.Keep && cur.Keep && w.Managed:
		out = append(out, "no longer keep it running: WSL stops it when nothing runs in it")
	}
	switch {
	case w.Logon && !cur.Logon:
		out = append(out, "start it when you sign in to Windows: a sign-in program, listed in Task Manager's Startup apps")
	case !w.Logon && cur.Logon:
		out = append(out, "no longer start it at sign-in")
	}
	switch {
	case w.Skip && !cur.Skip:
		out = append(out, "leave it to WSL: wsl-spawner neither stops nor starts it")
	case !w.Skip && cur.Skip:
		out = append(out, "make it wsl-spawner's to stop again")
	}
	return out
}

// summary says a distribution's choices, after a change.
func summary(s setup.State) string {
	if s.Skip {
		return "left to WSL"
	}
	var parts []string
	if s.Managed {
		parts = append(parts, "set up")
	} else {
		parts = append(parts, "stopped with its services' time, nothing set up in it")
	}
	if s.Keep {
		parts = append(parts, "kept running")
	}
	if s.Logon {
		parts = append(parts, "starts at sign-in")
	}
	return strings.Join(parts, ", ")
}

// saveChoices writes what the settings hold of a distribution's choices,
// skip and logon, and keeps the App's settings as written.
func saveChoices(a *app.App, name string, w setup.State) error {
	c := a.Config
	skip, logon := withName(c.Skip, name, w.Skip), withName(c.Logon, name, w.Logon)
	values := map[string]string{}
	if !slices.Equal(skip, c.Skip) {
		values["skip"] = config.QuoteList(skip)
	}
	if !slices.Equal(logon, c.Logon) {
		values["logon"] = config.QuoteList(logon)
	}
	if len(values) == 0 {
		return nil
	}
	if err := config.SetKeys(a.ConfigPath, values); err != nil {
		return fmt.Errorf("writing the settings: %w", err)
	}
	a.Config.Skip, a.Config.Logon = skip, logon
	return nil
}

// withName is names with name in it or out of it, in the order kept, the
// name as WSL spells it. A name there already keeps its place: the sign-in
// start goes by this order, and a change of another choice must not move it.
func withName(names []string, name string, in bool) []string {
	at := slices.IndexFunc(names, func(n string) bool { return strings.EqualFold(n, name) })
	out := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return strings.EqualFold(n, name) })
	switch {
	case in && at >= 0:
		// Where it was, once, spelled as WSL spells it.
		out = slices.Insert(out, at, name)
	case in:
		// New: after the others.
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// logonWant is the sign-in program's command line as the settings want it:
// the quiet build of this copy, while a distribution starts at sign-in; ""
// for none.
func logonWant(pc app.PC, c config.Config) (string, error) {
	if len(c.Logon) == 0 {
		return "", nil
	}
	exe, err := pc.QuietExe()
	if err != nil {
		return "", hint(err, "install wsl-spawner again: the release has both programs")
	}
	if err := startable(pc, exe, "sign-in program"); err != nil {
		return "", err
	}
	return `"` + exe + `" logon`, nil
}

// logonCheck refuses a change of name's choices, from cur to want, that ends
// with a sign-in program this copy cannot make: while a distribution starts
// at sign-in, every change ends with the sign-in program made to start this
// copy (syncLogonEntry). A change that is not the one starting name at
// sign-in is told which distributions make it the sign-in program's, or a
// choice that has nothing to do with sign-in would read as about it.
func logonCheck(pc app.PC, c config.Config, name string, cur, want setup.State) error {
	// Those that start at sign-in once the change is made.
	logon := withName(c.Logon, name, want.Logon)
	_, err := logonWant(pc, config.Config{Logon: logon})
	// No refusal; or the change starts name at sign-in, which the words are
	// about already.
	if err == nil || want.Logon && !cur.Logon {
		return err
	}
	// Who starts at sign-in, as the words put it before "at sign-in".
	who := andList(logon) + " start"
	if len(logon) == 1 {
		who = logon[0] + " starts"
	}
	why := "while " + who + " at sign-in, a change makes the sign-in program start this copy"
	// The refusal's own what to do stays with it, after the why.
	var h hinted
	if errors.As(err, &h) {
		return hint(fmt.Errorf("%s: %w", why, h.err), h.hint)
	}
	return fmt.Errorf("%s: %w", why, err)
}

// syncLogonEntry makes the sign-in program there while a distribution
// starts at sign-in, and gone when none does.
func syncLogonEntry(pc app.PC, c config.Config) error {
	have, ok, _, err := pc.LogonEntry(logonName)
	if err != nil {
		return fmt.Errorf("reading the sign-in programs: %w", err)
	}
	want, err := logonWant(pc, c)
	if err != nil {
		return err
	}
	if want == "" {
		if !ok {
			return nil
		}
		if err := pc.DeleteLogonEntry(logonName); err != nil {
			return fmt.Errorf("removing the sign-in program: %w", err)
		}
		return nil
	}
	if ok && have == want {
		return nil
	}
	if err := pc.SetLogonEntry(logonName, want); err != nil {
		return fmt.Errorf("adding the sign-in program: %w", err)
	}
	return nil
}

// logonRepair says what syncLogonEntry would change of the sign-in program,
// for the plan, or "" when it is as the settings want it. One Task Manager
// turned off is the person's to turn on: it is no repair.
func logonRepair(pc app.PC, c config.Config) (string, error) {
	have, ok, _, err := pc.LogonEntry(logonName)
	if err != nil {
		return "", fmt.Errorf("reading the sign-in programs: %w", err)
	}
	want, err := logonWant(pc, c)
	if err != nil {
		return "", err
	}
	switch {
	case want == "" && ok:
		return "take out the sign-in program left over: nothing starts at sign-in", nil
	case want != "" && !ok:
		return "put the sign-in program back: it is not there, though the settings start distributions at sign-in", nil
	case want != "" && have != want:
		return "make the sign-in program start this copy, " + want + ", not " + have, nil
	}
	return "", nil
}

// insideHint says what to do when a distribution cannot have wsl-spawner
// set up in it.
func insideHint(err error, name string) error {
	switch {
	case errors.Is(err, setup.ErrNoSystemd):
		return hint(err, "turn systemd on in it: [boot] systemd=true in /etc/wsl.conf, then wsl --terminate "+name)
	case errors.Is(err, setup.ErrNoInterop):
		return hint(err, "turn Windows programs on in it: [interop] enabled=true in /etc/wsl.conf, then wsl --terminate "+name)
	}
	return err
}
