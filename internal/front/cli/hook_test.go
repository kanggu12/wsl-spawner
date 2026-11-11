package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/front/api"

	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/machine/gpo"
	"github.com/kanggu12/wsl-spawner/internal/machine/winapi"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// policy is this PC's sign-out scripts, held here, and what was asked of
// the administrator.
type policy struct {
	// text is scripts.ini's text; powerShell psscripts.ini's.
	text, powerShell string
	admin            bool
	// elevatedArgs is what a copy started as an administrator was given.
	elevatedArgs []string
	// answer is what that copy does: "ok", "declined", or an error.
	answer string
	// filesErr fails the change of the policy's files; refreshErr Windows'
	// reading of them.
	filesErr, refreshErr error
	// refreshedAs is whether each reading of the policy Windows was asked
	// for came from an administrator's copy (true) or the person's (false).
	refreshedAs []bool
}

// newPolicy puts made-up sign-out scripts in a's PC.
func newPolicy(t *testing.T, a *app.App, text string, admin bool) *policy {
	t.Helper()
	p := &policy{text: text, admin: admin, answer: "ok"}
	a.PC.ReadScripts = func() (string, error) { return p.text, nil }
	a.PC.ReadPowerShellScripts = func() (string, error) { return p.powerShell, nil }
	// As the real ones: a file that cannot be changed for certain stays.
	a.PC.InstallHook = func(exe, params string, drop ...string) error {
		if p.filesErr != nil {
			return p.filesErr
		}
		out, err := gpo.WithHook(p.text, exe, params, drop...)
		if err == nil {
			p.text = out
		}
		return err
	}
	a.PC.UninstallHook = func(exe string, drop ...string) error {
		if p.filesErr != nil {
			return p.filesErr
		}
		out, err := gpo.WithoutHook(p.text, exe, drop...)
		if err == nil {
			p.text = out
		}
		return err
	}
	a.PC.RefreshPolicy = func() error { p.refreshedAs = append(p.refreshedAs, p.admin); return p.refreshErr }
	a.PC.Elevated = func() bool { return p.admin }
	a.PC.MaxWait = func() time.Duration { return 600 * time.Second }
	a.PC.QuietExe = func() (string, error) { return `C:\Tools\wsl-spawnerw.exe`, nil }
	a.PC.FixedDisk = onFixedDisks
	a.PC.RunElevated = func(exe string, args []string) (int, error) {
		p.elevatedArgs = append([]string{exe}, args...)
		switch p.answer {
		case "declined":
			return 0, winapi.ErrDeclined
		case "ok":
			// The copy Windows starts as an administrator: this program
			// again, with those arguments and no terminal.
			p.admin = true
			defer func() { p.admin = false }()
			return runWith(args, io.Discard, io.Discard, func() (*app.App, error) { return a, nil }), nil
		}
		return 1, errors.New(p.answer)
	}
	return p
}

// onFixedDisks is a made-up PC's disks: every drive letter a fixed disk,
// every share (\\server\share, \\wsl.localhost\NAME) none.
func onFixedDisks(p string) bool { return !strings.HasPrefix(p, `\\`) }

// another is a sign-out script of another tool's.
const another = "[Logoff]\r\n0CmdLine=C:\\Tools\\save-notes.cmd\r\n0Parameters=\r\n"

func TestHookStatusSaysWhatRunsAtSignOut(t *testing.T) {
	a, _ := pc(t)
	newPolicy(t, a, another, false)
	out, _, code := cli(t, a, false, "hook", "status")
	if code != exitOK || !strings.Contains(out, "sign-out hook not installed") || !strings.Contains(out, "another sign-out script: C:\\Tools\\save-notes.cmd") || !strings.Contains(out, "up to 600 s") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	out, _, _ = cli(t, a, false, "hook", "status", "--json")
	var got api.Hook
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Installed || got.MaxWait != 600 || !slices.Equal(got.Others, []string{`C:\Tools\save-notes.cmd`}) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestHookStatusSaysPowerShellSignOutScriptsRunAfterIt(t *testing.T) {
	// Windows runs psscripts.ini's sign-out scripts after scripts.ini's,
	// unless set otherwise: the hook last among the others is not last.
	a, _ := pc(t)
	p := newPolicy(t, a, gpo.Add(another, `C:\Tools\wsl-spawnerw.exe`, "logoff"), false)
	p.powerShell = "[ScriptsConfig]\r\n[Logoff]\r\n0CmdLine=C:\\Tools\\backup.ps1\r\n0Parameters=-Quick\r\n"
	out, _, code := cli(t, a, false, "hook", "status")
	if code != exitOK || !strings.Contains(out, "! sign-out hook installed, the last of 2 sign-out scripts, but PowerShell ones run after it, when WSL has stopped, unless Windows is set to run them first") || !strings.Contains(out, "a PowerShell sign-out script: C:\\Tools\\backup.ps1 -Quick") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	out, _, _ = cli(t, a, false, "hook", "status", "--json")
	var got api.Hook
	if err := json.Unmarshal([]byte(out), &got); err != nil || !got.Last || !slices.Equal(got.PowerShell, []string{`C:\Tools\backup.ps1 -Quick`}) {
		t.Fatalf("%+v %v", got, err)
	}
	// None: the hook is the last, as before, and the JSON lists none.
	p.powerShell = ""
	if out, _, _ := cli(t, a, false, "hook", "status"); !strings.Contains(out, "sign-out hook installed, the last of 2 sign-out scripts\n") || strings.Contains(out, "PowerShell") {
		t.Fatalf("%s", out)
	}
	if out, _, _ := cli(t, a, false, "hook", "status", "--json"); !strings.Contains(out, `"powershell_scripts": []`) {
		t.Fatalf("%s", out)
	}
}

func TestAMistypedHookCommandIsAMistakeWithTheCommandMeant(t *testing.T) {
	// hook instal: a mistake in the command line, as a mistyped command is
	// at the root, with the command meant, not the help and 0.
	a, _ := pc(t)
	p := newPolicy(t, a, another, false)
	out, errOut, code := cli(t, a, false, "hook", "instal")
	if code != exitUsage || !strings.Contains(errOut, `unknown command "instal" for "wsl-spawner hook"`) || !strings.Contains(errOut, "did you mean: wsl-spawner hook install") || out != "" {
		t.Fatalf("exit %d: %q %q", code, out, errOut)
	}
	// Nothing was changed for it.
	if p.text != another || p.elevatedArgs != nil {
		t.Fatalf("scripts %q, elevated %q", p.text, p.elevatedArgs)
	}
	// A word like none of them: where to look.
	if _, errOut, code := cli(t, a, false, "hook", "xyzzy"); code != exitUsage || !strings.Contains(errOut, "wsl-spawner hook --help lists the commands") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// So at the root, with the same exit code.
	if _, errOut, code := cli(t, a, false, "uninstal"); code != exitUsage || !strings.Contains(errOut, "did you mean: wsl-spawner uninstall") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// hook alone still shows its commands, and ends with 0.
	if out, errOut, code := cli(t, a, false, "hook"); code != exitOK || !strings.Contains(out, "install") || !strings.Contains(out, "remove") || !strings.Contains(out, "status") || errOut != "" {
		t.Fatalf("exit %d: %q %q", code, out, errOut)
	}
}

func TestHookStatusReadsWhetherPowerShellSignOutScriptsRunFirst(t *testing.T) {
	// psscripts.ini's own choice, as the Group Policy editor writes it: run
	// first, the PowerShell ones come before the hook, which is the last.
	a, _ := pc(t)
	// What the check reads besides; then the policy held here, for the
	// test to change.
	checkPC(t, a, "")
	p := newPolicy(t, a, gpo.Add(another, `C:\Tools\wsl-spawnerw.exe`, "logoff"), false)
	p.powerShell = "[ScriptsConfig]\r\nEndExecutePSFirst=true\r\n[Logoff]\r\n0CmdLine=C:\\Tools\\backup.ps1\r\n0Parameters=-Quick\r\n"
	out, _, code := cli(t, a, false, "hook", "status")
	if code != exitOK || !strings.Contains(out, "sign-out hook installed, the last of 2 sign-out scripts\n") || strings.Contains(out, "!") ||
		!strings.Contains(out, "a PowerShell sign-out script, run before the others: C:\\Tools\\backup.ps1 -Quick") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	out, _, _ = cli(t, a, false, "hook", "status", "--json")
	var got api.Hook
	if err := json.Unmarshal([]byte(out), &got); err != nil || !got.Last || got.PowerShellFirst == nil || !*got.PowerShellFirst {
		t.Fatalf("%+v %v\n%s", got, err, out)
	}
	// The check counts none after the hook either.
	if out, _, _ := cli(t, a, false, "check"); !strings.Contains(out, "✓ sign-out hook installed, the last of 2 sign-out scripts") || strings.Contains(out, "PowerShell sign-out scripts run after it") {
		t.Fatalf("check:\n%s", out)
	}
	// Run last: said as the choice it is, not as a default Windows may be
	// set against.
	p.powerShell = strings.Replace(p.powerShell, "=true", "=false", 1)
	out, _, _ = cli(t, a, false, "hook", "status")
	if !strings.Contains(out, "! sign-out hook installed, the last of 2 sign-out scripts, but PowerShell ones run after it, when WSL has stopped, as this PC's policy has them run last") {
		t.Fatalf("%s", out)
	}
	if out, _, _ := cli(t, a, false, "hook", "status", "--json"); !strings.Contains(out, `"powershell_first": false`) {
		t.Fatalf("%s", out)
	}
	// No choice in the file: Windows' policy decides, and the JSON says null.
	p.powerShell = "[Logoff]\r\n0CmdLine=C:\\Tools\\backup.ps1\r\n0Parameters=-Quick\r\n"
	if out, _, _ := cli(t, a, false, "hook", "status", "--json"); !strings.Contains(out, `"powershell_first": null`) {
		t.Fatalf("%s", out)
	}
}

// The programs of other copies on a made-up PC: one moved away (gone), a
// second install (there), and another account's, which this user may not
// look at.
const (
	oldCopy    = `D:\old\wsl-spawnerw.exe`
	secondCopy = `E:\second\wsl-spawnerw.exe`
	bobsCopy   = `C:\Users\Bob\AppData\Local\Programs\wsl-spawner\wsl-spawnerw.exe`
)

// lookAtCopies is a made-up PC's look at a file: the copies above as they
// stand, every other file there.
func lookAtCopies(p string) error {
	switch p {
	case oldCopy:
		return fs.ErrNotExist
	case bobsCopy:
		return fs.ErrPermission
	}
	return nil
}

// hookOf is a hook's two lines at index i, as scripts.ini has them.
func hookOf(i int, exe string) string {
	return strconv.Itoa(i) + "CmdLine=" + exe + "\r\n" + strconv.Itoa(i) + "Parameters=logoff\r\n"
}

func TestAnotherCopysHookIsSaidAndOnlyOneThatIsGoneTakenOut(t *testing.T) {
	// The moved copy's hook first, another tool's script, then a second
	// install's, another account's; this copy's not yet installed.
	a, _ := pc(t)
	checkPC(t, a, "")
	start := "[Logoff]\r\n" + hookOf(0, oldCopy) + "1CmdLine=C:\\Tools\\save-notes.cmd\r\n1Parameters=\r\n" + hookOf(2, secondCopy) + hookOf(3, bobsCopy)
	p := newPolicy(t, a, start, false)
	a.PC.LookAt = lookAtCopies
	out, _, code := cli(t, a, false, "hook", "status")
	for _, want := range []string{
		"sign-out hook not installed\n",
		`the sign-out hook of a copy that is gone, which starts nothing: "` + oldCopy + `" logoff (wsl-spawner hook install or remove takes it out)`,
		`another copy's sign-out hook, which stops WSL as you sign out: "` + secondCopy + `" logoff (that copy's own wsl-spawner hook remove takes it out)`,
		`another copy's sign-out hook, whose program cannot be looked at from here: "` + bobsCopy + `" logoff`,
		"another sign-out script: C:\\Tools\\save-notes.cmd",
	} {
		if code != exitOK || !strings.Contains(out, want) {
			t.Errorf("exit %d: no %q in\n%s", code, want, out)
		}
	}
	// A copy's hook is said once, on its own line.
	if strings.Contains(out, "another sign-out script: "+oldCopy) {
		t.Fatalf("%s", out)
	}
	out, _, _ = cli(t, a, false, "hook", "status", "--json")
	var got api.Hook
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Copies) != 3 || got.Copies[0] != (api.HookCopy{Command: `"` + oldCopy + `" logoff`, Program: "gone", Before: true}) ||
		got.Copies[1].Program != "there" || got.Copies[2].Program != "unknown" || len(got.Others) != 4 {
		t.Fatalf("%+v %v\n%s", got, err, out)
	}
	// The check: with no hook of this copy's, the second install's runs at
	// sign-out instead, said as that; the gone one and the other account's
	// start nothing for this user.
	out, _, _ = cli(t, a, false, "check")
	if !strings.Contains(out, "no sign-out hook of this copy's, but another copy's runs at sign-out: "+secondCopy) || strings.Contains(out, "no sign-out hook: ") ||
		strings.Contains(out, oldCopy) || strings.Contains(out, bobsCopy) {
		t.Fatalf("check:\n%s", out)
	}
	// Install, through the copy Windows starts as an administrator: the gone
	// one's hook goes, said first; the others stay, said too; this one last.
	out, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitOK || !strings.Contains(out, `Takes out the sign-out hook of a copy that is gone: "`+oldCopy+`" logoff`) ||
		!strings.Contains(out, `The sign-out hook of another copy stays: "`+secondCopy+`" logoff; that copy's own wsl-spawner hook remove takes it out`) {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if s := gpo.Logoff(p.text); len(s) != 4 || s[0].CmdLine != `C:\Tools\save-notes.cmd` || s[1].CmdLine != secondCopy || s[2].CmdLine != bobsCopy || !gpo.Ours(s[3], `C:\Tools\wsl-spawnerw.exe`) {
		t.Fatalf("%+v", s)
	}
	if out, _, _ := cli(t, a, false, "hook", "status"); !strings.Contains(out, "sign-out hook installed, the last of 4 sign-out scripts\n") ||
		!strings.Contains(out, `another copy's sign-out hook, which stops WSL before this one: "`+secondCopy+`" logoff`) {
		t.Fatalf("%s", out)
	}
	// Remove: this copy's goes; the others' stay.
	if out, errOut, code := cli(t, a, false, "hook", "remove", "--yes"); code != exitOK || !strings.Contains(out, "sign-out hook removed") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if s := gpo.Logoff(p.text); len(s) != 3 || s[1].CmdLine != secondCopy || s[2].CmdLine != bobsCopy {
		t.Fatalf("%+v", s)
	}
}

func TestACopysHookAfterThisOneLeavesItLast(t *testing.T) {
	// Another account's hook after this one starts nothing for this user,
	// and a second install's finds nothing left to stop: this one is the
	// last that counts, and installing it again changes nothing. Two
	// accounts' installs would otherwise move each other's last in turn.
	a, _ := pc(t)
	p := newPolicy(t, a, "[Logoff]\r\n"+hookOf(0, `C:\Tools\wsl-spawnerw.exe`)+hookOf(1, secondCopy)+hookOf(2, bobsCopy), false)
	a.PC.LookAt = lookAtCopies
	out, _, _ := cli(t, a, false, "hook", "status")
	if !strings.Contains(out, "sign-out hook installed, the last of 3 sign-out scripts\n") || !strings.Contains(out, `another copy's sign-out hook, after this one, which finds nothing left to stop: "`+secondCopy+`" logoff`) {
		t.Fatalf("%s", out)
	}
	if out, _, _ := cli(t, a, false, "hook", "install", "--yes"); !strings.Contains(out, "nothing to change") || p.elevatedArgs != nil {
		t.Fatalf("%s, elevated %q", out, p.elevatedArgs)
	}
	// Only a copy's hook that is gone, with none of this one's: remove takes
	// it out.
	p.text = "[Logoff]\r\n" + hookOf(0, oldCopy) + "1CmdLine=C:\\Tools\\save-notes.cmd\r\n1Parameters=\r\n"
	out, errOut, code := cli(t, a, false, "hook", "remove", "--yes")
	if code != exitOK || !strings.Contains(out, "took out the sign-out hook of a copy that is gone") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if s := gpo.Logoff(p.text); len(s) != 1 || s[0].CmdLine != `C:\Tools\save-notes.cmd` {
		t.Fatalf("%+v", s)
	}
	// None left: nothing to remove.
	if out, _, _ := cli(t, a, false, "hook", "remove", "--yes"); !strings.Contains(out, "nothing to remove") {
		t.Fatalf("%s", out)
	}
}

func TestTheHookComesOutWithoutTheQuietBuild(t *testing.T) {
	// A virus scanner took the build with no console away: the hook that
	// names it must still come out. This program itself is started as an
	// administrator instead, hidden, and takes the hook out by its path.
	const quiet, self = `C:\Tools\wsl-spawnerw.exe`, `C:\Tools\wsl-spawner.exe`
	a, _ := pc(t)
	p := newPolicy(t, a, gpo.Add(another, quiet, "logoff"), false)
	a.PC.QuietExe = func() (string, error) { return quiet, app.ErrNoQuietExe }
	a.PC.Executable = func() (string, error) { return self, nil }
	out, errOut, code := cli(t, a, false, "hook", "remove", "--yes")
	if code != exitOK || !strings.Contains(out, "sign-out hook removed") || len(p.elevatedArgs) == 0 || p.elevatedArgs[0] != self {
		t.Fatalf("exit %d, elevated %q: %s%s", code, p.elevatedArgs, out, errOut)
	}
	if s := gpo.Logoff(p.text); len(s) != 1 || s[0].CmdLine != `C:\Tools\save-notes.cmd` {
		t.Fatalf("%+v", s)
	}
	// An install still refuses: its hook would start a program that is not
	// there.
	p.elevatedArgs = nil
	if _, errOut, code := cli(t, a, false, "hook", "install", "--yes"); code != exitFail || !strings.Contains(errOut, "wsl-spawnerw.exe is not beside wsl-spawner.exe") || p.elevatedArgs != nil {
		t.Fatalf("exit %d, elevated %q: %s", code, p.elevatedArgs, errOut)
	}
	// Windows cannot start any copy as an administrator: the words say what
	// works, not to try again.
	p.text, p.answer = gpo.Add(another, quiet, "logoff"), `starting C:\Tools\wsl-spawner.exe as an administrator: The system cannot find the file specified.`
	_, errOut, code = cli(t, a, false, "hook", "remove", "--yes")
	if code != exitFail || !strings.Contains(errOut, "nothing changed: starting") || !strings.Contains(errOut, "in a terminal run as administrator, wsl-spawner hook remove changes it itself") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// As it says: run as an administrator, it takes the hook out itself.
	p.admin, p.elevatedArgs = true, nil
	if _, errOut, code := cli(t, a, false, "hook", "remove", "--yes"); code != exitOK || p.elevatedArgs != nil || len(gpo.Logoff(p.text)) != 1 {
		t.Fatalf("exit %d, elevated %q: %s", code, p.elevatedArgs, errOut)
	}
}

func TestHookInstallAsksWindowsForAnAdministrator(t *testing.T) {
	a, _ := pc(t)
	p := newPolicy(t, a, another, false)
	out, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitOK || !strings.Contains(out, "Windows asks for an administrator's permission.") || !strings.Contains(out, "sign-out hook installed") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	// The quiet copy, with no window, did it as an administrator.
	if len(p.elevatedArgs) != 6 || p.elevatedArgs[0] != `C:\Tools\wsl-spawnerw.exe` || !slices.Equal(p.elevatedArgs[1:5], []string{"hook", "install", "--yes", "--report"}) {
		t.Fatalf("elevated %q", p.elevatedArgs)
	}
	// Last, after the other tool's.
	s := gpo.Logoff(p.text)
	if len(s) != 2 || s[1] != (gpo.Script{Index: 1, CmdLine: `C:\Tools\wsl-spawnerw.exe`, Parameters: "logoff"}) {
		t.Fatalf("%+v", s)
	}
	// Once there, nothing to change.
	if out, _, _ := cli(t, a, false, "hook", "install", "--yes"); !strings.Contains(out, "nothing to change") {
		t.Fatalf("%s", out)
	}
	if out, _, _ := cli(t, a, false, "hook", "status"); !strings.Contains(out, "installed, the last of 2 sign-out scripts") {
		t.Fatalf("%s", out)
	}
}

func TestAHookThatIsNotLastIsPutLast(t *testing.T) {
	a, _ := pc(t)
	first := "[Logoff]\r\n0CmdLine=C:\\Tools\\wsl-spawnerw.exe\r\n0Parameters=logoff\r\n1CmdLine=C:\\Tools\\save-notes.cmd\r\n1Parameters=\r\n"
	p := newPolicy(t, a, first, true)
	if out, _, _ := cli(t, a, false, "hook", "status"); !strings.Contains(out, "scripts after it run when WSL has stopped") {
		t.Fatalf("%s", out)
	}
	if _, errOut, code := cli(t, a, false, "hook", "install", "--yes"); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if s := gpo.Logoff(p.text); s[1].CmdLine != `C:\Tools\wsl-spawnerw.exe` || p.elevatedArgs != nil {
		t.Fatalf("%+v, elevated %q", s, p.elevatedArgs)
	}
}

func TestADeclinedPermissionChangesNothing(t *testing.T) {
	a, _ := pc(t)
	p := newPolicy(t, a, "", false)
	p.answer = "declined"
	_, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitFail || !strings.Contains(errOut, "the administrator's permission was not given") || p.text != "" {
		t.Fatalf("exit %d: %s / %q", code, errOut, p.text)
	}
}

func TestHookRemove(t *testing.T) {
	a, _ := pc(t)
	p := newPolicy(t, a, gpo.Add(another, `C:\Tools\wsl-spawnerw.exe`, "logoff"), true)
	if out, errOut, code := cli(t, a, false, "hook", "remove", "--yes"); code != exitOK || !strings.Contains(out, "sign-out hook removed") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if s := gpo.Logoff(p.text); len(s) != 1 || s[0].CmdLine != `C:\Tools\save-notes.cmd` {
		t.Fatalf("%+v", s)
	}
	if out, _, _ := cli(t, a, false, "hook", "remove", "--yes"); !strings.Contains(out, "nothing to remove") {
		t.Fatalf("%s", out)
	}
}

func TestWindowsReadsTheChangeAsTheOneWhoAsked(t *testing.T) {
	// The administrator Windows asks for may be another user, whose name
	// and password were typed: the copy started as them changes the files,
	// and the policy is read as the person who signs out.
	a, _ := pc(t)
	p := newPolicy(t, a, another, false)
	if _, errOut, code := cli(t, a, false, "hook", "install", "--yes"); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !slices.Equal(p.refreshedAs, []bool{false}) {
		t.Fatalf("read as an administrator's copy (true) or the person (false): %v", p.refreshedAs)
	}
	// Run as an administrator from the start, it reads its own.
	p.admin, p.refreshedAs = true, nil
	if _, errOut, code := cli(t, a, false, "hook", "remove", "--yes"); code != exitOK || !slices.Equal(p.refreshedAs, []bool{true}) || p.elevatedArgs == nil {
		t.Fatalf("exit %d: %s, read %v", code, errOut, p.refreshedAs)
	}
}

func TestAChangeWindowsHasNotReadIsDoneButSaid(t *testing.T) {
	// gpupdate fails, as on a laptop of a domain away from it: the hook
	// is in the policy's files, and the words and the exit code say that
	// Windows has not read it yet.
	a, _ := pc(t)
	p := newPolicy(t, a, another, false)
	p.refreshErr = errors.New("gpupdate: exit status 1: User policy could not be updated successfully")
	_, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitAttention || !strings.Contains(errOut, "sign-out hook installed, but Windows has not read it yet: gpupdate: exit status 1") || !strings.Contains(errOut, "gpupdate /target:user has Windows read it now") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// What to type reads only what changed: /force would apply every
	// policy again, a domain's too, for a change gpt.ini's version counts.
	if strings.Contains(errOut, "/force") {
		t.Fatalf("the words ask for /force: %s", errOut)
	}
	if s := gpo.Logoff(p.text); len(s) != 2 || !gpo.Ours(s[1], `C:\Tools\wsl-spawnerw.exe`) {
		t.Fatalf("%+v", s)
	}
	_, errOut, code = cli(t, a, false, "hook", "remove", "--yes")
	if code != exitAttention || !strings.Contains(errOut, "sign-out hook removed, but Windows has not read that yet") || len(gpo.Logoff(p.text)) != 1 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// Files that could not be changed are not changed, and say so.
	p.refreshErr, p.filesErr = nil, errors.New(`writing C:\Windows\System32\GroupPolicy\gpt.ini: Access is denied.; the policy is as it was`)
	_, errOut, code = cli(t, a, false, "hook", "install", "--yes")
	if code != exitFail || !strings.Contains(errOut, "the hook was not changed: writing") || !strings.Contains(errOut, "the policy is as it was") || len(gpo.Logoff(p.text)) != 1 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestTheHookAndTheSignInProgramStartOnlyFromAFixedDisk(t *testing.T) {
	// wsl-spawner run from a distribution's own folder, as a launcher on
	// the PATH inside WSL finds it: no hook, no sign-in program, and no
	// question, and nothing changes.
	inWSL := `\\wsl.localhost\Ubuntu-24.04\home\ann\bin\wsl-spawnerw.exe`
	a, _ := pc(t)
	p := newPolicy(t, a, another, false)
	a.PC.QuietExe = func() (string, error) { return inWSL, nil }
	_, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitFail || !strings.Contains(errOut, "the sign-out hook must start a program on a fixed disk of this PC, and "+inWSL+" is not on one") || p.elevatedArgs != nil || p.text != another {
		t.Fatalf("exit %d: %s\nelevated %q, scripts %q", code, errOut, p.elevatedArgs, p.text)
	}
	// The copy started as an administrator refuses it too.
	report := filepath.Join(t.TempDir(), "wsl-spawner-hook-1.txt")
	write(t, report, "")
	p.admin = true
	if _, _, code := cli(t, a, false, "hook", "install", "--yes", "--report", report); code != exitFail || p.text != another {
		t.Fatalf("exit %d, scripts %q", code, p.text)
	}
	if said, _ := os.ReadFile(report); !strings.Contains(string(said), "not on one") {
		t.Fatalf("report %q", said)
	}
	// Its removal stays possible: it takes nothing to start.
	p.text = gpo.Add(another, inWSL, "logoff")
	if _, errOut, code := cli(t, a, false, "hook", "remove", "--yes"); code != exitOK || len(gpo.Logoff(p.text)) != 1 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// The sign-in program: refused before the settings change.
	b := newBox(t)
	b.a.PC.QuietExe = func() (string, error) { return inWSL, nil }
	_, errOut, code = cli(t, b.a, false, "set", "Ubuntu-24.04", "--logon", "--yes")
	if code != exitFail || !strings.Contains(errOut, "the sign-in program must start a program on a fixed disk") || b.entry != "" || len(b.changes()) != 0 {
		t.Fatalf("exit %d, entry %q, changes %q: %s", code, b.entry, b.changes(), errOut)
	}
	if c, err := config.Load(b.a.ConfigPath); err != nil || len(c.Logon) != 0 {
		t.Fatalf("logon %q %v", c.Logon, err)
	}
	// Another distribution's choice, while one starts at sign-in, would
	// end with the sign-in program made to start this copy: refused too,
	// before anything changes.
	b.a.Config.Logon, b.entry = []string{"Ubuntu-24.04"}, `"C:\Tools\wsl-spawnerw.exe" logon`
	if _, errOut, code := cli(t, b.a, false, "set", "Debian", "--skip", "--yes"); code != exitFail || !strings.Contains(errOut, "fixed disk") || b.entry != `"C:\Tools\wsl-spawnerw.exe" logon` || len(b.a.Config.Skip) != 0 {
		t.Fatalf("exit %d, entry %q, skip %q: %s", code, b.entry, b.a.Config.Skip, errOut)
	}
}

func TestTheAdministratorsCopyWritesOnlyIntoItsOwnEmptyFile(t *testing.T) {
	// Any program can start that copy with a path of its choosing, and
	// Windows runs it as an administrator if the person says yes: it writes
	// into nothing but an empty file of its own named for its answer, and
	// changes nothing when given another.
	a, _ := pc(t)
	hooked := gpo.Add(another, `C:\Tools\wsl-spawnerw.exe`, "logoff")
	p := newPolicy(t, a, hooked, true)
	dir := t.TempDir()
	theirs, full, empty := filepath.Join(dir, "hosts"), filepath.Join(dir, "wsl-spawner-hook-2.txt"), filepath.Join(dir, "empty.txt")
	write(t, theirs, "127.0.0.1 localhost\n")
	write(t, full, "something\n")
	write(t, empty, "")
	missing := filepath.Join(dir, "wsl-spawner-hook-3.txt")
	cases := map[string]string{"another name": theirs, "a file with something in it": full, "no file": missing}
	// A second name for an empty file, and a link to one, where the system
	// lets a test make them.
	if second := filepath.Join(dir, "wsl-spawner-hook-4.txt"); os.Link(empty, second) == nil {
		cases["a second name for a file"] = second
	}
	if link := filepath.Join(dir, "wsl-spawner-hook-5.txt"); os.Symlink(empty, link) == nil {
		cases["a link"] = link
	}
	for name, report := range cases {
		if _, _, code := cli(t, a, false, "hook", "remove", "--yes", "--report", report); code == exitOK || p.text != hooked {
			t.Errorf("%s: exit %d, scripts %q", name, code, p.text)
		}
	}
	for path, want := range map[string]string{theirs: "127.0.0.1 localhost\n", full: "something\n", empty: ""} {
		if b, err := os.ReadFile(path); err != nil || string(b) != want {
			t.Errorf("%s now holds %q (%v)", path, b, err)
		}
	}
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a file was made: %v", err)
	}
	// Its own empty file takes the answer.
	own := filepath.Join(dir, "wsl-spawner-hook-6.txt")
	write(t, own, "")
	if _, errOut, code := cli(t, a, false, "hook", "remove", "--yes", "--report", own); code != exitOK || len(gpo.Logoff(p.text)) != 1 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if b, _ := os.ReadFile(own); string(b) != "ok\n" {
		t.Fatalf("%q", b)
	}
}

func TestAFileItCannotChangeForCertainIsSaidBeforeWindowsAsks(t *testing.T) {
	// Another tool's script named twice: renumbered, Windows could take
	// the other one. Nothing changes, and no administrator is asked for.
	twice := "[Logoff]\r\n0CmdLine=C:\\Tools\\save-notes.cmd\r\n0Parameters=\r\n0cmdline=C:\\Tools\\other.cmd\r\n"
	a, _ := pc(t)
	p := newPolicy(t, a, twice, false)
	_, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitFail || !strings.Contains(errOut, "left as they are") || !strings.Contains(errOut, "0CmdLine twice") || p.text != twice || p.elevatedArgs != nil {
		t.Fatalf("exit %d: %s\nscripts %q, elevated %q", code, errOut, p.text, p.elevatedArgs)
	}
	// So with a script after a missing index, which would start running.
	gap := "[Logoff]\r\n0CmdLine=C:\\Tools\\save-notes.cmd\r\n0Parameters=\r\n2CmdLine=C:\\Tools\\old.cmd\r\n2Parameters=\r\n"
	p.text = gap
	if _, errOut, code := cli(t, a, false, "hook", "install", "--yes"); code != exitFail || !strings.Contains(errOut, "after a missing index") || p.text != gap || p.elevatedArgs != nil {
		t.Fatalf("exit %d: %s\nscripts %q, elevated %q", code, errOut, p.text, p.elevatedArgs)
	}
}

func TestAFileItCannotReadForCertainSaysWhatToDo(t *testing.T) {
	// A scripts.ini in a code page: hook status says so, and what to do.
	a, _ := pc(t)
	newPolicy(t, a, "", false)
	a.PC.ReadScripts = func() (string, error) {
		return "", &gpo.Unsure{File: `C:\Windows\System32\GroupPolicy\User\Scripts\scripts.ini`, Why: "it is in a code page or in UTF-8, with characters outside ASCII, which wsl-spawner cannot read for certain as Windows does"}
	}
	_, errOut, code := cli(t, a, false, "hook", "status")
	if code != exitFail || !strings.Contains(errOut, "scripts.ini: it is in a code page") || !strings.Contains(errOut, "put it right by hand, or in gpedit.msc") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

// errBrokenSettings is a settings file that does not load, as config.Load
// says it.
var errBrokenSettings = errors.New(`C:\Users\Ann\AppData\Roaming\wsl-spawner\config.toml: line 3: expected a value`)

// cliBrokenSettings runs a command line as cli does, with a settings file
// that does not load: the App comes with the error, as app.New gives it.
func cliBrokenSettings(t *testing.T, a *app.App, args ...string) (string, string, int) {
	t.Helper()
	in, outTerm := stdinIsTerminal, stdoutIsTerminal
	t.Cleanup(func() { stdinIsTerminal, stdoutIsTerminal = in, outTerm })
	// No terminal: what a copy started as an administrator, or a script, has.
	stdinIsTerminal, stdoutIsTerminal = func() bool { return false }, func() bool { return false }
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_SPAWNER_TTY", "")
	var out, errOut bytes.Buffer
	code := runWith(args, &out, &errOut, func() (*app.App, error) { return a, errBrokenSettings })
	return out.String(), errOut.String(), code
}

func TestTheAdministratorsCopyReportsWithSettingsThatDoNotLoad(t *testing.T) {
	// uninstall takes the hook out first, through this copy, and a settings
	// file that does not load may be what it is about to remove: the copy
	// must still change the hook and write how it went.
	a, _ := pc(t)
	p := newPolicy(t, a, gpo.Add(another, `C:\Tools\wsl-spawnerw.exe`, "logoff"), true)
	// The file the one that asked made for the answer, empty.
	report := filepath.Join(t.TempDir(), "wsl-spawner-hook-1.txt")
	write(t, report, "")
	_, errOut, code := cliBrokenSettings(t, a, "hook", "remove", "--yes", "--report", report)
	said, _ := os.ReadFile(report)
	if code != exitOK || strings.TrimSpace(string(said)) != "ok" {
		t.Fatalf("exit %d, report %q: %s", code, said, errOut)
	}
	// The hook went; the other tool's script stayed.
	if s := gpo.Logoff(p.text); len(s) != 1 || s[0].CmdLine != `C:\Tools\save-notes.cmd` {
		t.Fatalf("%+v", s)
	}
}

func TestTheHookAndUninstallGoOnWithSettingsThatDoNotLoad(t *testing.T) {
	// hook status, install and remove read no settings.
	a, _ := pc(t)
	p := newPolicy(t, a, another, false)
	if out, errOut, code := cliBrokenSettings(t, a, "hook", "status"); code != exitOK || !strings.Contains(out, "sign-out hook not installed") {
		t.Fatalf("status: exit %d: %s%s", code, out, errOut)
	}
	if out, errOut, code := cliBrokenSettings(t, a, "hook", "install", "--yes"); code != exitOK || len(gpo.Logoff(p.text)) != 2 {
		t.Fatalf("install: exit %d: %s%s", code, out, errOut)
	}
	if out, errOut, code := cliBrokenSettings(t, a, "hook", "remove", "--yes"); code != exitOK || len(gpo.Logoff(p.text)) != 1 {
		t.Fatalf("remove: exit %d: %s%s", code, out, errOut)
	}
	// A command that reads the settings still refuses to go on with
	// defaults the person did not choose, and says what to do.
	if _, errOut, code := cliBrokenSettings(t, a, "status"); code != exitFail || !strings.Contains(errOut, "line 3: expected a value") || !strings.Contains(errOut, "fix the file") {
		t.Fatalf("status of the distributions: exit %d: %s", code, errOut)
	}
	// uninstall, the hook first, as the made-up install has it.
	fi := installed(t, "wsl-spawner")
	fi.settingsErr = errBrokenSettings
	code, out, errOut := fi.run(t, "", "uninstall", "--yes")
	if code != exitOK || len(gpo.Logoff(fi.scripts)) != 1 || fi.entry != "" || len(fi.deleted) != 1 {
		t.Fatalf("uninstall: exit %d: %s%s", code, out, errOut)
	}
}

func TestTheSignOutHookStopsWithTheDefaultsWhenTheSettingsDoNotLoad(t *testing.T) {
	// A typo in the settings must not leave WSL to cut every distribution
	// short at sign-out: the defaults stand in, and the history says why.
	a, f := pc(t)
	_, errOut, code := cliBrokenSettings(t, a, "logoff")
	calls := f.CallList()
	if code != exitOK || !slices.Contains(calls, "terminate:Ubuntu-24.04") || calls[len(calls)-1] != "shutdown:" {
		t.Fatalf("exit %d: %s calls %q", code, errOut, calls)
	}
	all, _ := history.Open(a.Dir).All()
	if len(all) != 1 || all[0].By != "signout" || !strings.Contains(all[0].Err, "the settings did not load, so the defaults stood in: ") || !strings.Contains(all[0].Err, "line 3: expected a value") {
		t.Fatalf("%+v", all)
	}
	// The sign-in program starts what the settings name: unread, it starts
	// nothing, and the history says why.
	a2, f2 := pc(t)
	if _, _, code := cliBrokenSettings(t, a2, "logon"); code != exitFail || slices.ContainsFunc(f2.CallList(), func(c string) bool { return strings.HasPrefix(c, "start:") }) {
		t.Fatalf("logon: exit %d, calls %q", code, f2.CallList())
	}
	all, _ = history.Open(a2.Dir).All()
	if len(all) != 1 || all[0].By != "logon" || !strings.Contains(all[0].Err, "the settings did not load, so nothing started at sign-in") {
		t.Fatalf("logon: %+v", all)
	}
}

func TestLogoffStopsEverythingThenWSL(t *testing.T) {
	a, f := pc(t)
	out, errOut, code := cli(t, a, false, "logoff")
	if code != exitOK || out != "" || errOut != "" {
		t.Fatalf("exit %d: %q %q", code, out, errOut)
	}
	calls := f.CallList()
	if !slices.Contains(calls, "terminate:Ubuntu-24.04") || calls[len(calls)-1] != "shutdown:" || slices.Contains(calls, "terminate:Work") {
		t.Fatalf("calls %q", calls)
	}
	all, _ := history.Open(a.Dir).All()
	if len(all) != 1 || all[0].By != "signout" || !all[0].Shutdown {
		t.Fatalf("%+v", all)
	}
}
