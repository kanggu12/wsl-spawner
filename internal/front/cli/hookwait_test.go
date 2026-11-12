package cli

import (
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/machine/gpo"
)

// waitStore is the made-up PC's wait for the sign-out scripts and
// wsl-spawner's note of it, as the registry holds them.
type waitStore struct {
	wait gpo.Wait
	note *gpo.Raised
	// failRaise fails the raise, as a policy no administrator may change.
	failRaise error
}

func (w *waitStore) Wait() (gpo.Wait, error)         { return w.wait, nil }
func (w *waitStore) SetWait(v gpo.Wait) error        { w.wait = v; return nil }
func (w *waitStore) Note() (gpo.Raised, bool, error) { return derefNote(w.note) }
func (w *waitStore) SetNote(r *gpo.Raised) error {
	if r == nil {
		w.note = nil
		return nil
	}
	n := *r
	w.note = &n
	return nil
}

// derefNote is a note as Note gives it.
func derefNote(r *gpo.Raised) (gpo.Raised, bool, error) {
	if r == nil {
		return gpo.Raised{}, false, nil
	}
	return *r, true, nil
}

// withWait gives a's made-up PC the wait in w, which only an administrator
// may change, as the policy's: p says who this process is.
func withWait(a *app.App, p *policy, w *waitStore) {
	a.PC.MaxWait = func() time.Duration { return time.Duration(w.wait.Effective()) * time.Second }
	a.PC.RaiseWait = func(seconds int) (gpo.Raised, error) {
		if !p.admin {
			return gpo.Raised{}, errors.New("access is denied")
		}
		if w.failRaise != nil {
			return gpo.Raised{}, w.failRaise
		}
		return gpo.Raise(w, seconds)
	}
	a.PC.PutBackWait = func() (gpo.Wait, bool, error) {
		if !p.admin {
			return gpo.Wait{}, false, errors.New("access is denied")
		}
		return gpo.PutBack(w)
	}
	a.PC.RaisedWait = func() (gpo.Raised, bool) { return gpo.StillRaised(w) }
}

// slowStops is the made-up PC with stop_limit 900 s and Windows' own wait of
// 600 s, too short for it; another tool's sign-out script runs too.
func slowStops(t *testing.T) (*app.App, *policy, *waitStore) {
	t.Helper()
	a, _ := pc(t)
	a.Config.StopLimit = 900
	p := newPolicy(t, a, another, false)
	w := &waitStore{}
	withWait(a, p, w)
	return a, p, w
}

func TestHookInstallRaisesAWaitTooShortForTheStopLimit(t *testing.T) {
	a, p, w := slowStops(t)
	out, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	// stop_limit, a minute for WSL to end, and one for the other script.
	if !w.wait.Set || w.wait.Seconds != 1020 || w.note == nil || w.note.Before.Set {
		t.Fatalf("wait %+v, note %+v", w.wait, w.note)
	}
	// Said before the question, asked of the administrator's copy, and
	// said after.
	for _, want := range []string{"Raises how long Windows waits for the sign-out scripts, all of them, from 600 s to 1020 s:",
		"stop_limit 900 s, a minute for WSL to end, and a minute for each other sign-out script;",
		"sign-out hook installed", "Windows waits up to 1020 s for the sign-out scripts now, where it waited 600 s"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	if !slices.Contains(p.elevatedArgs, "--wait") {
		t.Fatalf("asked %q", p.elevatedArgs)
	}
	// hook status says it, and what remove puts back.
	out, _, _ = cli(t, a, false, "hook", "status")
	if !strings.Contains(out, "Windows waits up to 1020 s for the sign-out scripts, as wsl-spawner hook install raised it; hook remove puts back 600 s") {
		t.Fatalf("%s", out)
	}
	out, _, _ = cli(t, a, false, "hook", "status", "--json")
	var h api.Hook
	if err := json.Unmarshal([]byte(out), &h); err != nil || h.MaxWait != 1020 || h.MaxWaitBefore == nil || *h.MaxWaitBefore != 600 {
		t.Fatalf("%+v %v", h, err)
	}
}

func TestHookRemovePutsBackTheWaitInstallRaised(t *testing.T) {
	a, _, w := slowStops(t)
	w.wait = gpo.Wait{Seconds: 300, Set: true}
	cli(t, a, false, "hook", "install", "--yes")
	if w.wait.Seconds != 1020 {
		t.Fatalf("wait %+v", w.wait)
	}
	out, errOut, code := cli(t, a, false, "hook", "remove", "--yes")
	if code != exitOK || w.wait.Seconds != 300 || w.note != nil {
		t.Fatalf("exit %d, wait %+v, note %+v: %s%s", code, w.wait, w.note, out, errOut)
	}
	if !strings.Contains(out, "Puts back how long Windows waits for the sign-out scripts: 300 s") || !strings.Contains(out, "Windows waits up to 300 s for the sign-out scripts again, as before wsl-spawner hook install raised it") {
		t.Fatalf("%s", out)
	}
}

func TestAWaitChangedSinceInstallRaisedItStays(t *testing.T) {
	a, _, w := slowStops(t)
	cli(t, a, false, "hook", "install", "--yes")
	// Someone set another since: theirs.
	w.wait = gpo.Wait{Seconds: 1500, Set: true}
	out, _, code := cli(t, a, false, "hook", "remove", "--yes")
	if code != exitOK || w.wait.Seconds != 1500 || strings.Contains(out, "again") || strings.Contains(out, "Puts back") {
		t.Fatalf("exit %d, wait %+v: %s", code, w.wait, out)
	}
}

func TestHookInstallLeavesAWaitThatFits(t *testing.T) {
	a, p, w := slowStops(t)
	a.Config.StopLimit = 300
	out, _, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitOK || w.wait.Set || w.note != nil || strings.Contains(out, "Raises") || slices.Contains(p.elevatedArgs, "--wait") {
		t.Fatalf("exit %d, wait %+v, asked %q: %s", code, w.wait, p.elevatedArgs, out)
	}
}

func TestHookInstallRaisesTheWaitOfAHookAlreadyLast(t *testing.T) {
	a, p, w := slowStops(t)
	p.text = gpo.Add(another, `C:\Tools\wsl-spawnerw.exe`, "logoff")
	before := p.text
	out, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitOK || w.wait.Seconds != 1020 {
		t.Fatalf("exit %d, wait %+v: %s%s", code, w.wait, out, errOut)
	}
	// The files stay as they are, and Windows is not asked to read them.
	if p.text != before || len(p.refreshedAs) != 0 {
		t.Fatalf("scripts changed %v, read %v", p.text != before, p.refreshedAs)
	}
	for _, want := range []string{"The sign-out hook is installed, the last sign-out script.", "raised Windows' wait for the sign-out scripts\n", "Windows waits up to 1020 s for the sign-out scripts now"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	// Run again: nothing left to change.
	if out, _, _ := cli(t, a, false, "hook", "install", "--yes"); !strings.Contains(out, "nothing to change") {
		t.Fatalf("%s", out)
	}
}

func TestHookInstallAsAdministratorRaisesTheWaitItself(t *testing.T) {
	a, p, w := slowStops(t)
	p.admin = true
	out, _, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitOK || w.wait.Seconds != 1020 || len(p.elevatedArgs) != 0 {
		t.Fatalf("exit %d, wait %+v, asked %q: %s", code, w.wait, p.elevatedArgs, out)
	}
}

func TestAStandardUsersHookInstallLeavesTheWait(t *testing.T) {
	// A standard user's hook is an administrator's install: the wait is
	// the administrator's, and the administrator's copy is not asked.
	a, p, w := slowStops(t)
	a.PC.StandardUser = func() bool { return true }
	a.PC.WhoElseCanChange = func(string, bool) (string, string, error) { return "", "", nil }
	out, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitOK || w.wait.Set || strings.Contains(out, "Raises") || slices.Contains(p.elevatedArgs, "--wait") {
		t.Fatalf("exit %d, wait %+v, asked %q: %s%s", code, w.wait, p.elevatedArgs, out, errOut)
	}
}

func TestADeclinedInstallLeavesTheWait(t *testing.T) {
	a, p, w := slowStops(t)
	p.answer = "declined"
	if _, _, code := cli(t, a, false, "hook", "install", "--yes"); code != exitFail || w.wait.Set || w.note != nil {
		t.Fatalf("exit %d, wait %+v", code, w.wait)
	}
}

func TestAWaitThatCannotBeRaisedIsDoneButNotAsItShouldBe(t *testing.T) {
	a, _, w := slowStops(t)
	w.failRaise = errors.New("the policy is managed elsewhere")
	out, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	// The hook is in; the wait stays, said with what to do.
	if code != exitAttention || !strings.Contains(out, "sign-out hook installed") ||
		!strings.Contains(errOut, "the wait Windows gives the sign-out scripts stays as it was: the policy is managed elsewhere") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
}

func TestTheCheckNamesHookInstallForAShortWait(t *testing.T) {
	a, _, _ := slowStops(t)
	checkPC(t, a, "")
	withWait(a, &policy{}, &waitStore{})
	out, _, _ := cli(t, a, false, "check")
	if !strings.Contains(out, "! Windows waits up to 600 s for the sign-out scripts, less than stop_limit 900 s and a minute") ||
		!strings.Contains(out, "→ wsl-spawner hook install raises it to fit, or lower stop_limit in the settings") {
		t.Fatalf("%s", out)
	}
	// A standard user: the policy's own way, an administrator's.
	a.PC.StandardUser = func() bool { return true }
	if out, _, _ := cli(t, a, false, "check"); !strings.Contains(out, "→ lower stop_limit in the settings, or raise the policy") {
		t.Fatalf("%s", out)
	}
}

func TestUninstallPutsBackTheWaitHookInstallRaised(t *testing.T) {
	fi := installed(t, "wsl-spawner")
	p := &policy{}
	w := &waitStore{wait: gpo.Wait{Seconds: 1020, Set: true}, note: &gpo.Raised{To: 1020}}
	withWait(fi.a, p, w)
	// The copy Windows starts as an administrator: this program again.
	fi.a.PC.RunElevated = func(_ string, args []string) (int, error) {
		p.admin = true
		defer func() { p.admin = false }()
		return runWith(args, io.Discard, io.Discard, func() (*app.App, error) { return fi.a, nil }), nil
	}
	code, out, errOut := fi.run(t, "y\n", "uninstall")
	if code != exitOK || w.wait.Set || w.note != nil {
		t.Fatalf("exit %d, wait %+v: %s%s", code, w.wait, out, errOut)
	}
	if !strings.Contains(errOut, "removes  wsl-spawner's raise of Windows' wait for the sign-out scripts, back to 600 s") ||
		!strings.Contains(out, "put back Windows' wait for the sign-out scripts: 600 s") {
		t.Fatalf("%s%s", out, errOut)
	}
}

func TestHookRemovePutsBackARaisedWaitWithNoHookLeft(t *testing.T) {
	// The hook taken out by hand since: remove still puts the wait back.
	a, _, w := slowStops(t)
	w.wait, w.note = gpo.Wait{Seconds: 1020, Set: true}, &gpo.Raised{To: 1020, Before: gpo.Wait{Seconds: 300, Set: true}}
	out, errOut, code := cli(t, a, false, "hook", "remove", "--yes")
	if code != exitOK || w.wait.Seconds != 300 || w.note != nil || !strings.Contains(out, "put back Windows' wait for the sign-out scripts") {
		t.Fatalf("exit %d, wait %+v: %s%s", code, w.wait, out, errOut)
	}
	// Nothing raised, nothing to remove.
	if out, _, _ := cli(t, a, false, "hook", "remove", "--yes"); !strings.Contains(out, "nothing to remove") {
		t.Fatalf("%s", out)
	}
}

func TestAStandardUsersHookRemoveLeavesTheWait(t *testing.T) {
	// Raised by an administrator's install: a standard user's remove takes
	// the hook out, and the wait stays the administrator's.
	a, p, w := slowStops(t)
	p.text = gpo.Add(another, `C:\Tools\wsl-spawnerw.exe`, "logoff")
	w.wait, w.note = gpo.Wait{Seconds: 1020, Set: true}, &gpo.Raised{To: 1020}
	a.PC.StandardUser = func() bool { return true }
	a.PC.WhoElseCanChange = func(string, bool) (string, string, error) { return "", "", nil }
	out, errOut, code := cli(t, a, false, "hook", "remove", "--yes")
	if code != exitOK || w.wait.Seconds != 1020 || w.note == nil || slices.Contains(p.elevatedArgs, "--wait") || strings.Contains(out, "Puts back") {
		t.Fatalf("exit %d, wait %+v, asked %q: %s%s", code, w.wait, p.elevatedArgs, out, errOut)
	}
}

func TestAnotherCopysHookThatStaysKeepsTheRaise(t *testing.T) {
	// Another copy's hook stays, and the machine's one wait is raised for
	// it too: this copy's removal leaves the raise and its note.
	a, _, w := slowStops(t)
	p := newPolicy(t, a, gpo.Add(gpo.Add(another, `C:\Other\wsl-spawnerw.exe`, "logoff"), `C:\Tools\wsl-spawnerw.exe`, "logoff"), false)
	withWait(a, p, w)
	w.wait, w.note = gpo.Wait{Seconds: 1020, Set: true}, &gpo.Raised{To: 1020}
	out, errOut, code := cli(t, a, false, "hook", "remove", "--yes")
	if code != exitOK || w.wait.Seconds != 1020 || w.note == nil || strings.Contains(out, "Puts back") || slices.Contains(p.elevatedArgs, "--wait") {
		t.Fatalf("exit %d, wait %+v, asked %q: %s%s", code, w.wait, p.elevatedArgs, out, errOut)
	}
}

func TestAWaitNotRaisedIsSaidWhenWindowsHasNotReadTheHookEither(t *testing.T) {
	a, p, w := slowStops(t)
	w.failRaise = errors.New("the policy is managed elsewhere")
	p.refreshErr = errors.New("gpupdate failed")
	_, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitAttention || !strings.Contains(errOut, "has not read it yet") || !strings.Contains(errOut, "the policy is managed elsewhere") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestARaiseAloneThatFailsIsNotDone(t *testing.T) {
	// The hook already the last, and the raise refused: nothing was done.
	a, p, w := slowStops(t)
	p.text = gpo.Add(another, `C:\Tools\wsl-spawnerw.exe`, "logoff")
	w.failRaise = errors.New("the policy is managed elsewhere")
	out, errOut, code := cli(t, a, false, "hook", "install", "--yes")
	if code != exitFail || strings.Contains(out, "raised Windows' wait") || !strings.Contains(errOut, "the policy is managed elsewhere") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
}

func TestUninstallPutsBackTheWaitWhereTheScriptsCannotBeRead(t *testing.T) {
	// This user may not read the sign-out scripts; an administrator may:
	// the copy started as one puts the wait back, this program itself.
	fi := installed(t, "wsl-spawner")
	p := &policy{}
	w := &waitStore{wait: gpo.Wait{Seconds: 1020, Set: true}, note: &gpo.Raised{To: 1020}}
	withWait(fi.a, p, w)
	scripts := fi.a.PC.ReadScripts
	fi.a.PC.ReadScripts = func() (string, error) {
		if !p.admin {
			return "", errors.New("access is denied")
		}
		return scripts()
	}
	var started string
	fi.a.PC.RunElevated = func(exe string, args []string) (int, error) {
		started = exe
		p.admin = true
		defer func() { p.admin = false }()
		return runWith(args, io.Discard, io.Discard, func() (*app.App, error) { return fi.a, nil }), nil
	}
	code, out, errOut := fi.run(t, "y\n", "uninstall")
	if code != exitOK || started != fi.exe || w.wait.Set || w.note != nil {
		t.Fatalf("exit %d, started %q, wait %+v: %s%s", code, started, w.wait, out, errOut)
	}
	// It took this copy's hook out too, and says so, not that it stays.
	if len(gpo.Logoff(fi.scripts)) != 1 || strings.Contains(out, "any sign-out hook of wsl-spawner's stays") || !strings.Contains(out, "the copy started as an administrator took out wsl-spawner's hook") {
		t.Fatalf("scripts %q: %s", fi.scripts, out)
	}
}

func TestUninstallKeepsTheRaiseAnotherCopysHookNeeds(t *testing.T) {
	fi := installed(t, "wsl-spawner")
	p := &policy{}
	w := &waitStore{wait: gpo.Wait{Seconds: 1020, Set: true}, note: &gpo.Raised{To: 1020}}
	withWait(fi.a, p, w)
	fi.scripts = gpo.Add(fi.scripts, `C:\Other\wsl-spawnerw.exe`, "logoff")
	fi.a.PC.RunElevated = func(_ string, args []string) (int, error) {
		p.admin = true
		defer func() { p.admin = false }()
		return runWith(args, io.Discard, io.Discard, func() (*app.App, error) { return fi.a, nil }), nil
	}
	code, out, errOut := fi.run(t, "y\n", "uninstall")
	if code != exitOK || !w.wait.Set || w.note == nil || strings.Contains(errOut, "removes  wsl-spawner's raise") || !strings.Contains(errOut, "keeps    wsl-spawner's raise of Windows' wait for the sign-out scripts, for another copy's hook") {
		t.Fatalf("exit %d, wait %+v: %s%s", code, w.wait, out, errOut)
	}
}
