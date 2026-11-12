package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/distros"
	"github.com/kanggu12/wsl-spawner/internal/engine/setup"
	"github.com/kanggu12/wsl-spawner/internal/machine/gpo"
	"github.com/kanggu12/wsl-spawner/internal/machine/winapi"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// programFiles are what an install puts in its folder (the program, its
// build with no console, and its launcher for WSL), then the copies an
// install cut short leaves beside them. Nothing else in the folder is the
// program's.
var programFiles = []string{"wsl-spawner.exe", "wsl-spawnerw.exe", "wsl-spawner",
	"wsl-spawner.exe.new", "wsl-spawnerw.exe.new", "wsl-spawner.new"}

// The files the program keeps, by folder. A file that matches none is not
// the program's, and keeps its folder.
var (
	settingsFiles = []*regexp.Regexp{regexp.MustCompile(`^config\.toml(\.new)?$`)}
	stateFiles    = []*regexp.Regexp{
		regexp.MustCompile(`^history\.jsonl$`), regexp.MustCompile(`^history-.*\.tmp$`),
		regexp.MustCompile(`^crash-\d+-\d+(-shown)?\.txt$`),
	}
)

// uninstallCmd removes wsl-spawner from this PC, after a question that says
// what goes and what stays.
func uninstallCmd(p *apps) *cobra.Command {
	var purge, yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove wsl-spawner from this PC, and what it put in it",
		Long: "Takes out what wsl-spawner put on this PC first: the sign-out hook (Windows asks\n" +
			"for an administrator), the sign-in program, and what it set up in each running\n" +
			"distribution. Then it removes wsl-spawner.exe, wsl-spawnerw.exe and the launcher\n" +
			"for WSL from the folder they run from, and that folder and its PATH entry when\n" +
			"it is named wsl-spawner and nothing else is in it. The settings and the history\n" +
			"stay unless --purge is given. The program stays where this terminal may not\n" +
			"remove it: installed with Scoop, scoop uninstall wsl-spawner removes it after;\n" +
			"under Program Files, wsl-spawner uninstall in a terminal opened as\n" +
			"administrator does.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A settings file that does not load is no obstacle: it may be
			// what the person is removing. The App is still this command
			// line's own: a test's made-up PC in a test.
			a, err := p.anyway()
			if err != nil {
				return err
			}
			return uninstall(cmd, a, purge, yes)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "remove the settings and the history too")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "uninstall without asking")
	return cmd
}

// uninstall is the command.
func uninstall(cmd *cobra.Command, a *app.App, purge, yes bool) error {
	pc := a.PC
	// The Linux build hands this to the .exe inside WSL; elsewhere there is
	// no Windows program to remove.
	if !pc.OnWindows {
		return wsl.ErrNotWindows
	}
	// The question needs someone to answer it: without a terminal and
	// without --yes, stop before anything is looked at.
	if err := needAnswer(yes, "uninstall"); err != nil {
		return err
	}
	exe, err := pc.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	// The settings and the history are found through APPDATA and
	// LOCALAPPDATA. Without them their folders would be looked for, and
	// emptied, under the current folder.
	appData, localAppData := winapi.AppData(), winapi.LocalAppData()
	if !filepath.IsAbs(appData) || !filepath.IsAbs(localAppData) {
		return errors.New("APPDATA and LOCALAPPDATA must name folders: without them the settings and the history cannot be found")
	}
	settings := filepath.Dir(config.Path(appData))
	scoop := byScoop(dir)
	plan := uninstallPlan{exe: exe, dir: dir, settings: settings, state: a.Dir, scoop: scoop, purge: purge}
	// A program this user may not delete, as one an administrator put
	// under Program Files, is the administrator's to remove: the rest goes,
	// and the program stays, as Scoop's does.
	plan.adminsOwn = !scoop && !plan.deletable(pc)
	plan.leftovers(cmd.Context(), a)
	// A hook that cannot come out stops it all, as one Windows' answer
	// keeps would: left behind, every sign-out would start a program that
	// is gone. Said before the question, and before Windows asks, with how
	// to put the file right.
	if plan.cannot != nil {
		return unsureHint(fmt.Errorf("nothing was removed: the sign-out hook cannot be taken out of the sign-out scripts as they are: %w", plan.cannot))
	}
	// A standard user whose program anyone else could change is refused
	// the administrator's permission it would take: said before the
	// question, with the way the hook comes out instead.
	if run := plan.elevates(pc); run != "" {
		if err := nobodyElseElevates(pc, run, "remove"); err != nil {
			msg, what := splitHint(err)
			return hint(errors.New("nothing was removed: the sign-out hook stays, as "+msg), what)
		}
	}
	if err := plan.blocked(pc); err != nil {
		return err
	}
	// The folder goes, and its PATH entry with it, only when it is the
	// install's own: named for the program, with nothing else in it.
	plan.own, plan.why = ownFolder(dir)
	if !yes {
		// Whether the PATH has an entry for the folder, for the plan to say;
		// read only.
		if value, _, err := pc.UserPath(); plan.own && err == nil {
			_, n := pathWithout(value, dir, os.LookupEnv)
			plan.inPath = n > 0
		}
		plan.say(cmd.ErrOrStderr())
		q := "Uninstall? [y/N] "
		if scoop || plan.adminsOwn {
			q = "Take these out? [y/N] "
		}
		if err := confirm(cmd, q, "not uninstalled"); err != nil {
			return err
		}
		// The question may have waited long: another copy of the program or
		// a screen may have started meanwhile.
		if err := plan.blocked(pc); err != nil {
			return err
		}
	}
	return plan.now(cmd.Context(), a, cmd.OutOrStdout())
}

// uninstallPlan is what an uninstall works on, worked out once: the
// program and its folder, the settings and history folders, whether Scoop
// installed it, whether the folder is the install's own (why not, if not)
// and has a PATH entry, whether --purge was given, and what wsl-spawner put
// on the PC and in the distributions.
type uninstallPlan struct {
	exe, dir, settings, state, why string
	scoop, own, inPath, purge      bool
	// adminsOwn is set when this process may not delete the program's files,
	// as those under Program Files for anyone not elevated: the program
	// stays, as Scoop's does, for an uninstall run as administrator.
	adminsOwn bool
	// hook is the sign-out hook's program, "" when there is none; logon
	// is set when the sign-in program is there.
	hook  string
	logon bool
	// quiet is this copy's build with no console, which Windows starts as
	// an administrator for the hook's removal; gone the programs of copies
	// that are gone, whose hooks go with it; staying the copies whose hooks
	// stay, theirs to take out.
	quiet   string
	gone    []string
	staying []hookCopy
	// noQuiet is set when the quiet build is not there: this program itself
	// is started as an administrator instead, for the hook must come out.
	noQuiet bool
	// cannot is why the hook cannot be taken out of the sign-out scripts as
	// they are, as Windows reads them, nil when it can: tried on the text
	// read, before Windows asks for an administrator who could not either.
	cannot error
	// unread is why the sign-out scripts could not be read, nil when they
	// were: a hook of wsl-spawner's among them, if any, stays.
	unread error
	// setUp are the running distributions wsl-spawner is set up in;
	// unknown those of wsl-spawner's that do not run, which it may be set
	// up in, read only when they run.
	setUp, unknown []string
}

// leftovers finds what wsl-spawner put on the PC and in the distributions.
func (p *uninstallPlan) leftovers(ctx context.Context, a *app.App) {
	// Scripts that cannot be read are said, not taken for none: a hook of
	// wsl-spawner's may be among them, which would then stay.
	switch h, err := readHook(a.PC); {
	case err != nil:
		p.unread = err
	default:
		if h.installed() {
			p.hook = h.Exe
		}
		p.quiet, p.gone, p.staying, p.noQuiet = h.Exe, h.gone(), h.staying(), h.NoQuiet
		// The removal tried on the text, as hook remove tries it.
		if p.hook != "" || len(p.gone) > 0 {
			_, p.cannot = gpo.WithoutHook(h.text, h.Exe, p.gone...)
		}
	}
	if _, ok, _, err := a.PC.LogonEntry(logonName); err == nil && ok {
		p.logon = true
	}
	ds, err := distros.Read(ctx, a.WSL, a.Config)
	if err != nil {
		return
	}
	for _, d := range ds {
		switch {
		case d.Kind() == distros.KindSetUp:
			p.setUp = append(p.setUp, d.Name)
		case d.Ours() && !d.Running && d.Version == 2:
			p.unknown = append(p.unknown, d.Name)
		}
	}
}

// deletable reports whether this user may delete the program's files, as
// Windows answers it, without deleting any: no to any of them is no. One
// Windows gives no answer for, in use or not there, counts as yes: the
// removal itself says how it went. A process run as administrator is not
// asked: it is the one a program under Program Files is left to, and
// Windows' answer for one is not always the access lists'.
func (p uninstallPlan) deletable(pc app.PC) bool {
	if pc.CanDelete == nil || pc.Elevated() {
		return true
	}
	for _, f := range programFiles {
		path := filepath.Join(p.dir, f)
		if st, err := os.Lstat(path); err != nil || !st.Mode().IsRegular() {
			continue
		}
		if errors.Is(pc.CanDelete(path), fs.ErrPermission) {
			return false
		}
	}
	return true
}

// elevates is the program Windows would start as an administrator for the
// hook's removal, "" when none is: no hook to take out, or this process
// is one already.
func (p uninstallPlan) elevates(pc app.PC) string {
	switch {
	case p.hook == "" && len(p.gone) == 0, pc.Elevated():
		return ""
	case p.noQuiet:
		return p.exe
	}
	return p.quiet
}

// blocked says what stands in the way of an uninstall now: for a program
// it removes, a copy of it still running, or a screen open in WSL.
func (p uninstallPlan) blocked(pc app.PC) error {
	if p.scoop || p.adminsOwn {
		return nil
	}
	// A copy still running holds the program's file: it would stay behind,
	// and with it the folder. A list Windows does not give stops nothing:
	// the removal after the end tries for half a minute.
	if n, err := pc.OthersRunning(p.exe); err == nil && n > 0 {
		return hint(fmt.Errorf("another wsl-spawner is running from %s", p.dir), "close it, then uninstall")
	}
	// A screen open in WSL runs the launcher, and WSL holds its file:
	// Windows would remove it, and the screen's next question to the
	// program would fail. It is closed first.
	for _, f := range programFiles[1:] {
		if path := filepath.Join(p.dir, f); exists(path) && pc.InUse(path) {
			return hint(fmt.Errorf("%s is in use", path), "close the wsl-spawner screens open in WSL, then uninstall")
		}
	}
	return nil
}

// now removes what the plan said, in the order that leaves the least
// behind should a step fail: first what wsl-spawner put on the PC, the
// sign-out hook before all, as it runs the program about to go; then the
// PATH entry, while all of the program is there to try again; then the
// files beside the program, the settings and the history, and last the
// program itself, which goes once it has ended.
func (p uninstallPlan) now(ctx context.Context, a *app.App, out io.Writer) error {
	pc := a.PC
	exe, dir, settings, state, scoop, own, why, purge := p.exe, p.dir, p.settings, p.state, p.scoop, p.own, p.why, p.purge
	var notes []string
	// The sign-out hook, as an administrator: left behind, every sign-out
	// would start a program that is gone. Not taken out, nothing goes. The
	// hooks of copies that are gone go with it, or on their own.
	if p.hook != "" || len(p.gone) > 0 {
		var err error
		switch run := p.elevates(pc); {
		case run == "":
			err = change(pc, false, p.quiet, p.gone)
		default:
			// The quiet build, or, without it, as when a virus scanner took
			// it, this program, started hidden: the hook must come out.
			// Who else could change it was asked before the question.
			err = changeAsAdmin(pc, false, run)
		}
		// What went, as the end says it.
		what := "the sign-out hook"
		if p.hook == "" {
			what = goneHooks(len(p.gone))
		}
		var nr notRead
		var ns notStarted
		switch {
		case errors.As(err, &ns):
			// No copy could be started as an administrator: trying again
			// may not help, one run as one from the start does.
			return hint(fmt.Errorf("nothing was removed: the sign-out hook stays: %w", ns.error), "in a terminal run as administrator, wsl-spawner hook remove takes it out, then wsl-spawner uninstall")
		case errors.Is(err, errDeclined):
			// No was the answer: asked again by the same command.
			return hint(fmt.Errorf("nothing was removed: %w", errDeclined), "wsl-spawner uninstall asks again")
		case errors.As(err, &nr) && p.hook != "":
			// Out of the policy's files, which is what counts from Windows'
			// next reading: the uninstall goes on, and says so.
			notes = append(notes, "took the sign-out hook out of the local Group Policy, but Windows has not read that yet ("+nr.Error()+"): the next sign-out may still try to start the program removed now")
		case err != nil && !errors.As(err, &nr):
			// What to do is the error's own when it says one: another
			// line after it would say another thing.
			msg, what := splitHint(err)
			if what == "" {
				what = "wsl-spawner uninstall tries again"
			}
			return hint(errors.New("nothing was removed: the sign-out hook stays: "+msg), what)
		default:
			// Done; for a copy that is gone, read or not: it starts nothing.
			notes = append(notes, "took "+what+" out of the local Group Policy")
		}
	}
	// Another copy's hook is that copy's, whose own uninstall takes it out.
	for _, c := range p.staying {
		notes = append(notes, "the sign-out hook of another copy stays: "+hookLine(c.Exe)+": that copy's own uninstall takes it out")
	}
	if p.unread != nil {
		// Nothing was found to take out, and nothing could be: said, so
		// a hook left among the scripts is not a surprise.
		notes = append(notes, "any sign-out hook of wsl-spawner's stays: "+bareError(p.unread))
	}
	if p.logon {
		if err := pc.DeleteLogonEntry(logonName); err != nil {
			return hint(fmt.Errorf("the sign-in program stays: %w", err), "wsl-spawner uninstall tries again")
		}
		notes = append(notes, "took the sign-in program out")
	}
	for _, name := range p.setUp {
		if err := setup.Out(ctx, a.WSL, name); err != nil {
			notes = append(notes, "what wsl-spawner set up in "+name+" stays: "+firstLine(err.Error()))
			continue
		}
		notes = append(notes, "took wsl-spawner out of "+name)
	}
	// What the settings and the history folders hold before anything goes.
	kept := filled(settings, state)
	if own && !p.adminsOwn {
		// The PATH as stored, its other entries kept as they are written.
		value, expand, err := pc.UserPath()
		if err == nil {
			if rest, n := pathWithout(value, dir, os.LookupEnv); n > 0 {
				if err = pc.SetUserPath(rest, expand); err == nil {
					notes = append(notes, "took it out of your PATH: terminals opened from now on no longer find it")
				}
			}
		}
		if err != nil {
			return hint(fmt.Errorf("could not take %s out of your PATH: %w", dir, err), "the program was not removed; wsl-spawner uninstall tries again")
		}
	}
	if !scoop && !p.adminsOwn {
		// The quiet build, the launcher and the copies: the program's own
		// file is in use by this process until it ends.
		for _, f := range programFiles[1:] {
			// Only the program's files: a folder by such a name is the
			// person's.
			path := filepath.Join(dir, f)
			if st, err := os.Lstat(path); err != nil || !st.Mode().IsRegular() {
				continue
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return hint(err, commandLine(exe)+" uninstall tries again")
			}
		}
	}
	// The settings and the history: removed with --purge, file by file,
	// or said where they stay.
	var left []string
	if purge {
		// This run's own crash file is held until it ends, which then
		// removes it, and the folder with it once it is empty.
		left = append(purgeDir(settings, settingsFiles, ""), purgeDir(state, stateFiles, ownCrash)...)
	}
	purged := ""
	switch {
	case len(kept) == 0:
		// Nothing was there: nothing to say.
	case purge && len(left) == 0:
		purged = "removed the settings and the history"
	case purge:
		purged = "removed the settings and the history; kept " + strings.Join(left, " and ") + ": other files are in it"
	case len(kept) == 1:
		purged = "kept " + kept[0] + ": delete that folder to remove it too"
	default:
		purged = "kept " + strings.Join(kept, " and ") + ": delete those folders to remove them too"
	}
	for _, name := range p.unknown {
		notes = append(notes, name+" does not run: what wsl-spawner may have set up in it stays (/etc/systemd/system/wsl-spawner*.service)")
	}
	if scoop || p.adminsOwn {
		// The program stays, its remover's to remove.
		head, last := "took out what wsl-spawner put on this PC", "scoop uninstall wsl-spawner removes the program"
		if p.adminsOwn {
			last = "the program stays in " + dir + ": wsl-spawner uninstall in a terminal opened as administrator removes it"
		}
		lines := append([]string{head}, notes...)
		// Nothing is said of the settings where none were there.
		if purged != "" {
			lines = append(lines, purged)
		}
		sayDone(out, strings.Join(append(lines, last), "\n"))
		return nil
	}
	// The program itself, once it has ended; and its folder, when it is
	// the install's own.
	head, folder := "uninstalled wsl-spawner from "+dir, ""
	if own {
		folder = dir
	}
	// Windows removes no folder a terminal is in: looked at before the
	// removal starts.
	held := own && cwdIn(dir)
	if err := pc.DeleteAfterExit(exe, folder); err != nil {
		head += ", but for the program itself"
		leftPath := exe
		if own {
			leftPath = dir
		}
		notes = append(notes, "remove "+leftPath+" by hand once this program has ended: "+err.Error())
	}
	switch {
	case !own:
		notes = append(notes, "kept "+dir+" and any PATH entry for it: "+why)
	case held:
		notes = append(notes, "the folder stays while this terminal is in it: remove "+dir+" once you have left it")
	}
	if purged != "" {
		notes = append(notes, purged)
	}
	for i := range notes {
		notes[i] = "note: " + notes[i]
	}
	sayDone(out, strings.Join(append([]string{head}, notes...), "\n"))
	return nil
}

// bareError is an error's words without what to do about it: inside a
// line of a plan, the line says what happens instead.
func bareError(err error) string {
	var h hinted
	if errors.As(err, &h) {
		return h.err.Error()
	}
	return err.Error()
}

// cwdIn reports whether the current folder is dir or a folder in it.
func cwdIn(dir string) bool {
	wd, err := os.Getwd()
	if err != nil {
		return false
	}
	wd, dir = strings.ToLower(filepath.Clean(wd)), strings.ToLower(filepath.Clean(dir))
	return wd == dir || strings.HasPrefix(wd, dir+string(filepath.Separator))
}

// say says what an uninstall removes and what it keeps, one thing to a
// line, before the question.
func (p uninstallPlan) say(w io.Writer) {
	l := lookFor(w)
	line := func(verb, what string) { fmt.Fprintf(w, "  %s  %s\n", l.muted(verb), what) }
	fmt.Fprintln(w, "wsl-spawner uninstall:")
	// Windows asks once, for the hook and those of copies gone together:
	// said on the first of their lines.
	admin := " (Windows asks for an administrator)"
	if p.hook != "" {
		line("removes", "the sign-out hook from the local Group Policy"+admin)
		admin = ""
	}
	for _, exe := range p.gone {
		line("removes", "the sign-out hook of a copy that is gone: "+hookLine(exe)+admin)
		admin = ""
	}
	for _, c := range p.staying {
		line("keeps  ", "the sign-out hook of another copy: "+hookLine(c.Exe)+" (that copy's own uninstall takes it out)")
	}
	if p.unread != nil {
		line("keeps  ", "any sign-out hook of wsl-spawner's: "+bareError(p.unread))
	}
	if p.logon {
		line("removes", "the sign-in program, and with it the start at sign-in")
	}
	for _, name := range p.setUp {
		line("removes", "what wsl-spawner set up in "+name+": its services and start script; your hooks and stop-units stay")
	}
	for _, name := range p.unknown {
		line("keeps  ", "what wsl-spawner may have set up in "+name+": it does not run (wsl-spawner set "+name+" --managed=false takes it out)")
	}
	switch {
	case p.scoop:
		line("keeps  ", "the program: scoop uninstall wsl-spawner removes it")
	case p.adminsOwn:
		line("keeps  ", "the program in "+p.dir+": wsl-spawner uninstall in a terminal opened as administrator removes it")
	case p.own && p.inPath:
		line("removes", p.exe+", wsl-spawnerw.exe and the launcher for WSL")
		line("removes", "the folder "+p.dir+" and its entry in your PATH")
	case p.own:
		line("removes", p.exe+", wsl-spawnerw.exe and the launcher for WSL")
		line("removes", "the folder "+p.dir)
	default:
		line("removes", p.exe+", wsl-spawnerw.exe and the launcher for WSL")
		line("keeps  ", "the folder "+p.dir+": "+p.why)
	}
	// A folder with nothing in it is not named: this run makes the
	// history's for its own crash file, which it removes as it ends.
	for _, f := range []struct{ what, dir, it string }{{"the settings in ", p.settings, "them"}, {"the history in ", p.state, "it"}} {
		switch {
		case !holds(f.dir):
		case p.purge:
			line("removes", f.what+f.dir)
		default:
			line("keeps  ", f.what+f.dir+" (--purge removes "+f.it+")")
		}
	}
}

// ownFolder reports whether dir is a folder an install made: holding
// nothing but the program's files, and named for the program. If it is
// not, it says why the folder stays.
func ownFolder(dir string) (bool, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err.Error()
	}
	for _, e := range entries {
		// A folder by a program file's name is not the program's.
		mine := false
		for _, f := range programFiles {
			mine = mine || e.Type().IsRegular() && strings.EqualFold(e.Name(), f)
		}
		if !mine {
			return false, "other files are in it"
		}
	}
	// A folder of the person's own may hold the program alone, as a bin
	// folder on the PATH for it.
	if !strings.EqualFold(filepath.Base(dir), "wsl-spawner") {
		return false, "it is not named wsl-spawner"
	}
	return true, ""
}

// holds reports whether dir has anything in it but this run's own crash
// file.
func holds(dir string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Join(dir, e.Name()) != ownCrash {
			return true
		}
	}
	return false
}

// filled is those of the settings and the history folders that hold
// something, named as the person reads them.
func filled(settings, state string) []string {
	var out []string
	if holds(settings) {
		out = append(out, "the settings in "+settings)
	}
	if holds(state) {
		out = append(out, "the history in "+state)
	}
	return out
}

// byScoop reports whether Scoop installed the program in dir: Scoop keeps
// an app in apps\NAME\VERSION (or apps\NAME\current, the link to it), with
// the manifest it installed from and its own record beside the program.
func byScoop(dir string) bool {
	appDir, apps := filepath.Dir(dir), filepath.Dir(filepath.Dir(dir))
	if !strings.EqualFold(filepath.Base(appDir), "wsl-spawner") || !strings.EqualFold(filepath.Base(apps), "apps") {
		return false
	}
	// Scoop names them scoop-manifest.json and scoop-install.json; an
	// install by an older Scoop has manifest.json and install.json.
	for _, f := range scoopFiles {
		if exists(filepath.Join(dir, f)) {
			return true
		}
	}
	return false
}

// scoopFiles are the files Scoop writes beside an app it installed.
var scoopFiles = []string{"scoop-manifest.json", "scoop-install.json", "manifest.json", "install.json"}

// purgeDir removes the files in dir whose names one of names matches, but
// keep, then dir itself once nothing is left in it. It returns dir when
// something else stays there, and nothing when the folder is gone or goes
// with keep.
func purgeDir(dir string, names []*regexp.Regexp, keep string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	other := false
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		mine := false
		for _, re := range names {
			mine = mine || re.MatchString(e.Name())
		}
		switch {
		case path == keep:
			// Removed by the run that holds it, as it ends.
		case !mine:
			other = true
		default:
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				other = true
			}
		}
	}
	// Never RemoveAll: only an empty folder goes.
	_ = os.Remove(dir)
	if other {
		return []string{dir}
	}
	return nil
}

// pathWithout is the PATH value without its entries for dir, and how many
// went. An entry is compared as Windows reads it: its %NAME% variables
// expanded, quotes and a trailing backslash dropped, without regard to
// case. Every other entry is kept as it is written, empty ones too.
func pathWithout(value, dir string, lookup func(string) (string, bool)) (string, int) {
	want := pathKey(dir, lookup)
	var kept []string
	n := 0
	for _, entry := range splitPath(value) {
		if entry != "" && pathKey(entry, lookup) == want {
			n++
			continue
		}
		kept = append(kept, entry)
	}
	if n == 0 {
		return value, 0
	}
	return strings.Join(kept, ";"), n
}

// splitPath splits a PATH value at its semicolons, but for those inside
// quotes: a quoted entry may hold one.
func splitPath(value string) []string {
	var out []string
	start, quoted := 0, false
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '"':
			quoted = !quoted
		case ';':
			if !quoted {
				out = append(out, value[start:i])
				start = i + 1
			}
		}
	}
	return append(out, value[start:])
}

// pathKey is a PATH entry as compared: expanded, unquoted, without its
// trailing backslashes, in lower case.
func pathKey(entry string, lookup func(string) (string, bool)) string {
	s := strings.Trim(strings.TrimSpace(expandVars(entry, lookup)), `"`)
	return strings.ToLower(strings.TrimRight(s, `\/`))
}

// expandVars replaces each %NAME% in s with the variable's value, as
// Windows expands a PATH entry: a name that has no value stays as it is
// written, percent signs and all.
func expandVars(s string, lookup func(string) (string, bool)) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '%')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			break
		}
		name := s[i+1 : i+1+j]
		if v, ok := lookup(name); ok && name != "" {
			b.WriteString(s[:i] + v)
			s = s[i+j+2:]
			continue
		}
		// Not a variable: the first % is text, and the second may open one.
		b.WriteString(s[:i+1])
		s = s[i+1:]
	}
	return b.String() + s
}

// commandLine is a program's path as a person types it to run it: in quotes
// when it holds a blank, which would end it.
func commandLine(exe string) string {
	if strings.ContainsAny(exe, " \t") {
		return `"` + exe + `"`
	}
	return exe
}
