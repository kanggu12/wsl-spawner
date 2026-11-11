package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/front/api"

	"github.com/spf13/cobra"

	"github.com/kanggu12/wsl-spawner/internal/machine/gpo"
	"github.com/kanggu12/wsl-spawner/internal/machine/winapi"
)

// hookParams is what the sign-out script is given: the hidden command that
// stops every distribution, then WSL.
const hookParams = "logoff"

// hookState is where wsl-spawner's sign-out script stands.
type hookState struct {
	// Exe is the quiet program a hook of this copy runs.
	Exe string
	// Scripts are every sign-out script, in their order.
	Scripts []gpo.Script
	// Index is this copy's hook's place, -1 when there is none.
	Index int
	// NoQuiet is set when the build with no console is not beside this
	// one: its hook could not be installed from here.
	NoQuiet bool
	// Copies are the hooks of other copies of the program, in their order.
	Copies []hookCopy
	// text is scripts.ini's text, for a change to be tried out before
	// Windows asks for an administrator.
	text string
	// PowerShell are psscripts.ini's sign-out scripts, which Windows runs
	// after the others unless it is set to run them first, and
	// PowerShellErr why they could not be read: hook status reads them.
	// PowerShellFirst is psscripts.ini's own choice of their order, true
	// for before the others; nil when it makes none, and Windows' policy
	// decides.
	PowerShell      []gpo.Script
	PowerShellErr   error
	PowerShellFirst *bool
}

// readPowerShell reads psscripts.ini's sign-out scripts into h, and the
// order the file gives them: hook status and the check read them, as the
// hook's place among the others does not put it after them.
func (h *hookState) readPowerShell(pc app.PC) {
	text, err := pc.PowerShellScripts()
	if err != nil {
		h.PowerShellErr = unsureHint(fmt.Errorf("reading the PowerShell sign-out scripts: %w", err))
		return
	}
	h.PowerShell = gpo.Logoff(text)
	// A choice the file makes; none leaves it to Windows' policy.
	if first, set := gpo.PowerShellFirst(text); set {
		h.PowerShellFirst = &first
	}
}

// powerShellAfter reports whether PowerShell sign-out scripts may run after
// the hook: there are some, and psscripts.ini does not have them run first.
func (h hookState) powerShellAfter() bool {
	return len(h.PowerShell) > 0 && (h.PowerShellFirst == nil || !*h.PowerShellFirst)
}

// hookCopy is another copy's sign-out hook: an install moved elsewhere, or
// a second one, leaves it.
type hookCopy struct {
	// Index is its place among the sign-out scripts; Exe the program it
	// starts.
	Index int
	Exe   string
	// Gone is set when that program is gone for certain (app.PC.Gone): the
	// hook starts nothing, and every change of the hook takes it out.
	// There is set when it is there for this user, so it runs as they sign
	// out. Neither: it cannot be looked at from here, as another account's,
	// and it stays.
	Gone, There bool
}

// installed reports whether this copy's hook is there.
func (h hookState) installed() bool { return h.Index >= 0 }

// last reports whether this copy's hook runs after every sign-out script
// that may need WSL: only other copies' hooks come after it, which find
// nothing left to stop, or start nothing for this user.
func (h hookState) last() bool {
	if h.Index < 0 {
		return false
	}
	for i := h.Index + 1; i < len(h.Scripts); i++ {
		if _, ok := h.copyAt(i); !ok {
			return false
		}
	}
	return true
}

// copyAt is the copy's hook at index i, if the script there is one.
func (h hookState) copyAt(i int) (hookCopy, bool) {
	for _, c := range h.Copies {
		if c.Index == i {
			return c, true
		}
	}
	return hookCopy{}, false
}

// before reports whether c runs before this copy's hook, or this copy has
// none.
func (h hookState) before(c hookCopy) bool { return h.Index < 0 || c.Index < h.Index }

// gone are the programs of the copies that are gone, whose hooks a change
// of the hook takes out: they start nothing, and nothing else may.
func (h hookState) gone() []string {
	var out []string
	for _, c := range h.Copies {
		if c.Gone {
			out = append(out, c.Exe)
		}
	}
	return out
}

// staying are the copies whose hooks a change leaves: their programs may run,
// for this user or another, and are theirs to take out.
func (h hookState) staying() []hookCopy {
	var out []hookCopy
	for _, c := range h.Copies {
		if !c.Gone {
			out = append(out, c)
		}
	}
	return out
}

// readHook reads the sign-out scripts and finds this copy's hook, and the
// hooks of other copies.
func readHook(pc app.PC) (hookState, error) {
	exe, err := pc.QuietExe()
	if err != nil && !errors.Is(err, app.ErrNoQuietExe) {
		return hookState{}, err
	}
	text, err2 := pc.ReadScripts()
	if err2 != nil {
		return hookState{}, unsureHint(fmt.Errorf("reading the sign-out scripts: %w", err2))
	}
	h := hookState{Exe: exe, Scripts: gpo.Logoff(text), Index: -1, NoQuiet: err != nil, text: text}
	for i, s := range h.Scripts {
		switch {
		case gpo.Ours(s, exe):
			h.Index = i
		case gpo.Copy(s, exe, hookParams):
			// Looked at, never started: whether its program is gone, or there
			// for this user.
			h.Copies = append(h.Copies, hookCopy{Index: i, Exe: s.CmdLine, Gone: pc.Gone(s.CmdLine), There: pc.There(s.CmdLine)})
		}
	}
	return h, nil
}

// hookCmd holds the commands of the sign-out hook.
func hookCmd(p *apps) *cobra.Command {
	c := &cobra.Command{
		Use:   "hook",
		Short: "The sign-out hook: every distribution stopped the slow way as Windows signs out, shuts down or restarts",
		Long: "The sign-out hook is a sign-out script in this PC's local Group Policy. Windows runs\n" +
			"the sign-out scripts as you sign out, and before every shutdown and restart, an\n" +
			"update's included, and waits for each. wsl-spawner's runs last: it stops every\n" +
			"running distribution as wsl-spawner stop --all does, then WSL. Installing and\n" +
			"removing it needs an administrator; Windows asks.",
		// A word that is none of its commands is a mistake in the command
		// line, said with the command meant, as the root says it: cobra's
		// own "unknown command", which usageHint answers. Without a run of
		// its own, cobra would print the help and end with 0 instead.
		Args: cobra.NoArgs,
		// Alone, it shows what it holds.
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	c.AddCommand(hookStatusCmd(p), hookChangeCmd(p, true), hookChangeCmd(p, false))
	return c
}

// hookStatusCmd says whether the hook is there, where, and what Windows
// waits for.
func hookStatusCmd(p *apps) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Say whether the sign-out hook is installed, and what runs at sign-out",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The hook reads no settings: a file that does not load does
			// not hide where it stands.
			a, err := p.anyway()
			if err != nil {
				return err
			}
			h, err := readHook(a.PC)
			if err != nil {
				return err
			}
			// The PowerShell sign-out scripts, which the hook's place among
			// the others does not put it after, and their order.
			h.readPowerShell(a.PC)
			out, wait := cmd.OutOrStdout(), a.PC.MaxWait()
			if asJSON {
				return printJSON(out, hookJSON(h, wait))
			}
			writeHook(out, lookFor(out), h, wait)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

// writeHook says where the hook stands.
func writeHook(out io.Writer, l look, h hookState, wait time.Duration) {
	switch {
	case h.last() && h.powerShellAfter():
		// Last of the others: the PowerShell ones come after it, by
		// default, or as psscripts.ini has them run last.
		why := "unless Windows is set to run them first"
		if h.PowerShellFirst != nil {
			why = "as this PC's policy has them run last"
		}
		fmt.Fprintln(out, l.warn("!")+" sign-out hook installed, the last of "+counted(len(h.Scripts), "sign-out script")+", but PowerShell ones run after it, when WSL has stopped, "+why)
	case h.last():
		// PowerShell ones, if any, run before the others: the hook is last.
		fmt.Fprintln(out, l.done("sign-out hook installed, the last of "+counted(len(h.Scripts), "sign-out script")))
	case h.installed():
		fmt.Fprintln(out, l.warn("!")+" sign-out hook installed, but scripts after it run when WSL has stopped: wsl-spawner hook install puts it last")
	default:
		fmt.Fprintln(out, "sign-out hook not installed")
	}
	if h.installed() {
		fmt.Fprintln(out, l.muted("  runs: \""+h.Exe+"\" "+hookParams))
	}
	if wait > 0 {
		fmt.Fprintln(out, l.muted(fmt.Sprintf("  Windows waits up to %d s for the sign-out scripts", int(wait/time.Second))))
	} else {
		fmt.Fprintln(out, l.muted("  Windows waits for the sign-out scripts as long as they take"))
	}
	for i, s := range h.Scripts {
		// Another copy's hook is said on a line of its own, below.
		if _, isCopy := h.copyAt(i); i != h.Index && !isCopy {
			fmt.Fprintln(out, l.muted("  another sign-out script: "+strings.TrimSpace(s.CmdLine+" "+s.Parameters)))
		}
	}
	for _, c := range h.Copies {
		fmt.Fprintln(out, l.muted("  "+h.copyWords(c)))
	}
	// Where they run, when psscripts.ini has them run before the others.
	what := "a PowerShell sign-out script"
	if h.PowerShellFirst != nil && *h.PowerShellFirst {
		what += ", run before the others"
	}
	for _, s := range h.PowerShell {
		fmt.Fprintln(out, l.muted("  "+what+": "+strings.TrimSpace(s.CmdLine+" "+s.Parameters)))
	}
	if h.PowerShellErr != nil {
		fmt.Fprintln(out, l.muted("  "+bareError(h.PowerShellErr)))
	}
}

// copyWords says what another copy's hook does at sign-out, and what takes
// it out.
func (h hookState) copyWords(c hookCopy) string {
	cmd := hookLine(c.Exe)
	switch {
	case c.Gone:
		return "the sign-out hook of a copy that is gone, which starts nothing: " + cmd + " (wsl-spawner hook install or remove takes it out)"
	case !c.There:
		// Another account's, or on a drive not there now: it stays.
		return "another copy's sign-out hook, whose program cannot be looked at from here: " + cmd
	case h.installed() && !h.before(c):
		// After this one: everything is stopped by then.
		return "another copy's sign-out hook, after this one, which finds nothing left to stop: " + cmd
	case h.installed():
		return "another copy's sign-out hook, which stops WSL before this one: " + cmd + " (that copy's own wsl-spawner hook remove takes it out)"
	}
	return "another copy's sign-out hook, which stops WSL as you sign out: " + cmd + " (that copy's own wsl-spawner hook remove takes it out)"
}

// hookLine is the command line of a hook that starts exe, as hook status
// and its JSON give a hook's.
func hookLine(exe string) string { return `"` + exe + `" ` + hookParams }

// programState is how JSON says a copy's program stands.
func programState(c hookCopy) string {
	switch {
	case c.Gone:
		return "gone"
	case c.There:
		return "there"
	}
	return "unknown"
}

// hookJSON is the hook's state as JSON gives it.
func hookJSON(h hookState, wait time.Duration) api.Hook {
	o := api.Hook{Schema: schema, Installed: h.installed(), Last: h.last(), Others: []string{}, Copies: []api.HookCopy{},
		PowerShell: []string{}, PowerShellFirst: h.PowerShellFirst, MaxWait: int(wait / time.Second)}
	if h.installed() {
		o.Command = hookLine(h.Exe)
	}
	// Every other sign-out script, the copies' hooks among them, as before;
	// the copies again on their own, with how their programs stand.
	for i, s := range h.Scripts {
		if i != h.Index {
			o.Others = append(o.Others, strings.TrimSpace(s.CmdLine+" "+s.Parameters))
		}
	}
	for _, c := range h.Copies {
		o.Copies = append(o.Copies, api.HookCopy{Command: hookLine(c.Exe), Program: programState(c), Before: h.before(c)})
	}
	for _, s := range h.PowerShell {
		o.PowerShell = append(o.PowerShell, strings.TrimSpace(s.CmdLine+" "+s.Parameters))
	}
	return o
}

// hookChangeCmd is hook install (add true) or hook remove.
func hookChangeCmd(p *apps, add bool) *cobra.Command {
	var yes bool
	var report string
	use, short := "remove", "Remove the sign-out hook"
	if add {
		use, short = "install", "Install the sign-out hook, the last sign-out script"
	}
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The hook reads no settings, so a file that does not load
			// stops neither the change nor its report: the copy started as
			// an administrator may be uninstall's, which takes such a file
			// out, and the one that asked waits for its words.
			a, err := p.anyway()
			if err != nil {
				return err
			}
			// The copy Windows started as an administrator, for the one
			// that asked: it changes the policy and says how it went.
			if report != "" {
				return elevatedChange(a.PC, add, report)
			}
			return runHookChange(cmd, a.PC, add, yes)
		},
	}
	c.Flags().BoolVarP(&yes, "yes", "y", false, "change it without asking")
	c.Flags().StringVar(&report, "report", "", "write how it went to this file: for the copy started as an administrator")
	_ = c.Flags().MarkHidden("report")
	return c
}

// runHookChange says what installing or removing the hook does, asks, and
// does it, as an administrator.
func runHookChange(cmd *cobra.Command, pc app.PC, add, yes bool) error {
	out := cmd.OutOrStdout()
	h, err := readHook(pc)
	if err != nil {
		return err
	}
	if add && h.NoQuiet {
		return hint(app.ErrNoQuietExe, "install wsl-spawner again: the release has both programs")
	}
	// Said before anything else: no hook, nor a question, for a program
	// Windows could not count on at sign-out.
	if add {
		if err := startable(pc, h.Exe, "sign-out hook"); err != nil {
			return err
		}
	}
	// The hooks of copies that are gone go with any change: they start
	// nothing, and only wsl-spawner's own hook named them.
	gone := h.gone()
	switch {
	case add && h.last() && len(gone) == 0:
		fmt.Fprintln(out, "the sign-out hook is installed, the last sign-out script: nothing to change")
		return nil
	case !add && !h.installed() && len(gone) == 0:
		fmt.Fprintln(out, "the sign-out hook is not installed: nothing to remove")
		return nil
	}
	// The change tried here first: a file it cannot make for certain, as
	// Windows reads it, is said before Windows asks for an administrator,
	// who could do nothing with it either.
	tryErr := error(nil)
	if add {
		_, tryErr = gpo.WithHook(h.text, h.Exe, hookParams, gone...)
	} else {
		_, tryErr = gpo.WithoutHook(h.text, h.Exe, gone...)
	}
	if tryErr != nil {
		return unsureHint(fmt.Errorf("the sign-out scripts were left as they are: %w", tryErr))
	}
	switch {
	case add:
		fmt.Fprintln(out, "Adds wsl-spawner as the last sign-out script in this PC's local Group Policy:")
		fmt.Fprintln(out, "  as you sign out, and before every shutdown and restart, Windows runs it and waits;")
		fmt.Fprintln(out, "  it stops every running distribution as wsl-spawner stop --all does, then WSL.")
	case h.installed():
		fmt.Fprintln(out, "Removes wsl-spawner's sign-out script from this PC's local Group Policy.")
	}
	sayCopies(out, h)
	if !pc.Elevated() {
		fmt.Fprintln(out, lookFor(out).muted("Windows asks for an administrator's permission."))
	}
	if err := needAnswer(yes, use(add)); err != nil {
		return err
	}
	// What the change does, as the question and the end say it.
	q, done := "Install the sign-out hook? [y/N] ", "sign-out hook installed"
	switch {
	case !add && h.installed():
		q, done = "Remove the sign-out hook? [y/N] ", "sign-out hook removed"
	case !add:
		q, done = "Take out "+goneHooks(len(gone))+"? [y/N] ", "took out "+goneHooks(len(gone))
	}
	if !yes {
		if err := confirm(cmd, q, "nothing changed"); err != nil {
			return err
		}
	}
	if pc.Elevated() {
		err = change(pc, add, h.Exe, gone)
	} else {
		var run string
		if run, err = elevatedProgram(pc, h, add); err == nil {
			err = changeAsAdmin(pc, add, run)
		}
	}
	// How it went. In the policy's files, but not read by Windows yet: done,
	// and not as it should be, which a script must tell from a clean end.
	// The hook of a copy that is gone starts nothing, read or not: that is
	// just done.
	var nr notRead
	var ns notStarted
	switch {
	case errors.As(err, &ns):
		// No copy could be started as an administrator: one run as one from
		// the start changes the files itself, with nothing to start.
		return hint(fmt.Errorf("nothing changed: %w", ns.error), "in a terminal run as administrator, wsl-spawner hook "+hookVerb(add)+" changes it itself")
	case errors.As(err, &nr) && add:
		return attention{hint(fmt.Errorf("sign-out hook installed, but Windows has not read it yet: %w", nr.error), "it counts from Windows' next reading of your policy, as you sign in at the latest; "+refreshLine+" has Windows read it now")}
	case errors.As(err, &nr) && h.installed():
		return attention{hint(fmt.Errorf("sign-out hook removed, but Windows has not read that yet: %w", nr.error), "it still runs at sign-out until Windows next reads your policy, as you sign in at the latest; "+refreshLine+" has Windows read it now")}
	case err != nil && !errors.As(err, &nr):
		return err
	}
	sayDone(out, done)
	return nil
}

// sayCopies says, before the question, what a change does with other
// copies' hooks: those of copies that are gone go; the others stay, theirs
// to take out.
func sayCopies(out io.Writer, h hookState) {
	for _, exe := range h.gone() {
		fmt.Fprintln(out, "Takes out the sign-out hook of a copy that is gone: "+hookLine(exe))
	}
	for _, c := range h.staying() {
		fmt.Fprintln(out, lookFor(out).muted("The sign-out hook of another copy stays: "+hookLine(c.Exe)+"; that copy's own wsl-spawner hook remove takes it out"))
	}
}

// goneHooks names the hooks of n copies that are gone, for the words.
func goneHooks(n int) string {
	if n == 1 {
		return "the sign-out hook of a copy that is gone"
	}
	return "the sign-out hooks of " + strconv.Itoa(n) + " copies that are gone"
}

// refreshLine is the command that has Windows read the person's policy,
// for them to type: the one Refresh runs, so the two never differ.
var refreshLine = "gpupdate " + strings.Join(gpo.RefreshArgs, " ")

// notRead is a change of the sign-out hook that is in the policy's files,
// but that Windows has not read yet for the person who signs out: it counts
// from Windows' next reading of their policy.
type notRead struct{ error }

// Unwrap gives why Windows did not read it.
func (n notRead) Unwrap() error { return n.error }

// startable refuses exe as what Windows starts by itself (what: the sign-out
// hook, or the sign-in program) when it is not on a fixed disk of this PC:
// a share or a drive taken out may be gone when Windows starts it, and a
// distribution's own files (\\wsl.localhost) have WSL start that
// distribution, even at sign-out as the hook stops WSL.
func startable(pc app.PC, exe, what string) error {
	if pc.OnFixedDisk(exe) {
		return nil
	}
	return hint(fmt.Errorf("the %s must start a program on a fixed disk of this PC, and %s is not on one", what, exe), "install wsl-spawner on Windows with its install script, which puts it on this PC's own disk, and run it from there")
}

// unsureHint adds what to do to an error about a policy file wsl-spawner
// cannot read, or change, for certain as Windows does; other errors stay as
// they are.
func unsureHint(err error) error {
	var u *gpo.Unsure
	if !errors.As(err, &u) {
		return err
	}
	return hint(err, "wsl-spawner leaves such a file as it is: put it right by hand, or in gpedit.msc (User Configuration, Windows Settings, Scripts), then try again")
}

// use is what --yes does, for the question no one can answer.
func use(add bool) string { return hookVerb(add) + " it" }

// hookVerb is the hook command that makes the change: install or remove.
func hookVerb(add bool) string {
	if add {
		return "install"
	}
	return "remove"
}

// elevatedProgram is the copy of this program Windows starts as an
// administrator for a change of h: the quiet build, which opens no window.
// For a removal without it, as when a virus scanner took it away, this
// program itself, started hidden: the hook must always come out, and it
// needs no quiet build to be taken out.
func elevatedProgram(pc app.PC, h hookState, add bool) (string, error) {
	if add || !h.NoQuiet {
		return h.Exe, nil
	}
	// A made-up PC may not know its own program.
	if pc.Executable == nil {
		return "", errors.New("this program's own file is not known")
	}
	return pc.Executable()
}

// notStarted is a copy of this program that Windows could not start as an
// administrator: the answer was not no, the start itself failed.
type notStarted struct{ error }

// Unwrap gives why it could not start.
func (n notStarted) Unwrap() error { return n.error }

// change installs or removes the hook of exe, and takes out the hooks of
// the programs drop names, as an administrator, and has Windows read it for
// the user this process runs as: a change Windows did not read is a notRead.
func change(pc app.PC, add bool, exe string, drop []string) error {
	if err := changeFiles(pc, add, exe, drop); err != nil {
		return err
	}
	if err := pc.Refresh(); err != nil {
		return notRead{err}
	}
	return nil
}

// changeFiles installs or removes the hook of exe in the policy's files, and
// takes out the hooks of the programs drop names, as an administrator, and
// no more: what the copy Windows started as one does. Windows reads the
// change for the one that asked, who signs out.
func changeFiles(pc app.PC, add bool, exe string, drop []string) error {
	if add {
		return pc.InstallHook(exe, hookParams, drop...)
	}
	return pc.UninstallHook(exe, drop...)
}

// changeAsAdmin has the copy of this program at run (elevatedProgram)
// change the hook as an administrator, Windows asking for the permission,
// and reads how it went; then it has Windows read the change, as this
// process: Windows takes in a sign-out script as it reads the policy of the
// user who signs out, and an administrator whose name and password the
// person typed is another user. A change Windows did not read is a notRead;
// a copy Windows could not start, a notStarted.
func changeAsAdmin(pc app.PC, add bool, run string) error {
	// The empty file of its own the copy writes its answer into: the only
	// kind it writes into (reportName, winapi.OpenReport).
	f, err := os.CreateTemp("", "wsl-spawner-hook-*.txt")
	if err != nil {
		return err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	verb := hookVerb(add)
	code, err := pc.RunElevated(run, []string{"hook", verb, "--yes", "--report", path})
	if errors.Is(err, winapi.ErrDeclined) {
		return hint(errors.New("nothing changed: the administrator's permission was not given"), "wsl-spawner hook "+verb+" asks again")
	}
	if err != nil {
		return notStarted{err}
	}
	b, _ := os.ReadFile(path)
	said := strings.TrimSpace(string(b))
	if code != 0 || said != "ok" {
		if said == "" {
			said = "exit code " + strconv.Itoa(code)
		}
		return fmt.Errorf("the hook was not changed: %s", said)
	}
	if err := pc.Refresh(); err != nil {
		return notRead{err}
	}
	return nil
}

// reportName is the name of the file changeAsAdmin makes for the answer,
// as os.CreateTemp names it.
var reportName = regexp.MustCompile(`^wsl-spawner-hook-[0-9]+\.txt$`)

// elevatedChange is the administrator's copy: it changes the hook in the
// policy's files and writes "ok" or what went wrong into report. Windows'
// reading of the change is the one that asked's, as the user who signs out.
// It writes only into an empty file of its own named as changeAsAdmin names
// it, checked before anything changes: any program may start this copy
// with a path of its choosing, and Windows would run it as an administrator
// if the person said yes.
func elevatedChange(pc app.PC, add bool, report string) error {
	if !reportName.MatchString(filepath.Base(report)) {
		return fmt.Errorf("%w: --report takes the file wsl-spawner makes for the answer, not %s", errUsage, report)
	}
	w, err := winapi.OpenReport(filepath.Clean(report))
	if err != nil {
		return fmt.Errorf("%w: --report takes an empty file of its own: %s: %w", errUsage, report, err)
	}
	defer w.Close()
	// The hook as this copy reads it: which program is its own, and which
	// copies are gone, worked out here again, not taken from whoever
	// started it.
	h, err := readHook(pc)
	// The quiet build is what an installed hook starts; a removal needs
	// none: it takes out the hook of the path beside this copy, whether
	// that file is there or not.
	if err == nil && h.NoQuiet && add {
		err = app.ErrNoQuietExe
	}
	// The same refusal as the one that asked, whoever started this copy.
	if err == nil && add {
		err = startable(pc, h.Exe, "sign-out hook")
	}
	if err == nil {
		err = changeFiles(pc, add, h.Exe, h.gone())
	}
	said := "ok"
	if err != nil {
		said = err.Error()
	}
	if _, werr := w.WriteString(said + "\n"); werr != nil && err == nil {
		err = werr
	}
	return err
}

// logoffCmd is what the sign-out script runs: every running distribution of
// wsl-spawner's stopped the slow way, then WSL. It is started with no
// console, so what it did goes to the history.
func logoffCmd(p *apps) *cobra.Command {
	return &cobra.Command{
		Use:    "logoff",
		Short:  "Stop every distribution, then WSL, as Windows signs out",
		Args:   noArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A settings file that does not load must not leave WSL to cut
			// every distribution short: the defaults stand in, and the
			// history says why.
			a, err := p.anyway()
			if err != nil {
				return err
			}
			return runStop(cmd, a, stopAsk{all: true, shutdown: true, yes: true, background: true, by: "signout", settingsErr: p.settingsErr()})
		},
	}
}
