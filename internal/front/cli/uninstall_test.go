package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/gpo"
	"github.com/kanggu12/wsl-spawner/internal/machine/winapi"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// fakeInstall is a PC with wsl-spawner installed in a folder of the test's
// own, as the install script leaves it, with settings and a history in
// folders of the test's own too, a sign-out hook and a sign-in program, and
// one running distribution it is set up in, one stopped; and what uninstall
// asked of it. No test reaches the person's PATH, policy or program: every
// call that would is this one's.
type fakeInstall struct {
	a                         *app.App
	f                         *wsl.Fake
	dir, exe, settings, state string
	// path is the user's PATH as stored, expand whether it expands.
	path   string
	expand bool
	// wrote is each PATH value written; pathErr fails the write.
	wrote   []string
	pathErr error
	// deleted is each (program, folder) the removal after the end got.
	deleted [][2]string
	// held is the files another program holds; others how many other
	// copies of the program run.
	held   map[string]bool
	others int
	// scripts is the sign-out scripts' text; entry the sign-in program's
	// command line; declined makes the administrator's question a no.
	scripts, entry string
	declined       bool
	// takenOut are the distributions the removal script ran in.
	takenOut []string
	// settingsErr is a settings file that does not load, which the App
	// comes with, as app.New gives it; nil for one that loads.
	settingsErr error
	// refreshed counts Windows' readings of the policy asked for;
	// refreshErr fails them.
	refreshed  int
	refreshErr error
}

// installed makes the PC, with the program in root/Programs/folder.
func installed(t *testing.T, folder string) *fakeInstall {
	t.Helper()
	root := t.TempDir()
	// The settings and the history in folders of their own, on Windows and
	// off it: off it XDG's stand in.
	roaming, local := filepath.Join(root, "Roaming"), filepath.Join(root, "Local")
	for v, d := range map[string]string{"APPDATA": roaming, "XDG_CONFIG_HOME": roaming, "LOCALAPPDATA": local, "XDG_CACHE_HOME": local} {
		t.Setenv(v, d)
	}
	fi := &fakeInstall{dir: filepath.Join(root, "Programs", folder), settings: filepath.Join(roaming, "wsl-spawner"),
		state: filepath.Join(local, "wsl-spawner"), expand: true, held: map[string]bool{}}
	fi.exe = filepath.Join(fi.dir, "wsl-spawner.exe")
	quiet := filepath.Join(fi.dir, "wsl-spawnerw.exe")
	// The program, its build with no console, its launcher, a copy an
	// install cut short, the settings and the history.
	for _, p := range []string{fi.exe, quiet, filepath.Join(fi.dir, "wsl-spawner"), filepath.Join(fi.dir, "wsl-spawner.exe.new"),
		filepath.Join(fi.settings, "config.toml"), filepath.Join(fi.state, "history.jsonl")} {
		write(t, p, "x")
	}
	// The PATH as an install leaves it: the folder among the person's own
	// entries, one written with a variable and one empty.
	fi.path = `C:\Users\ann\bin;` + fi.dir + `\;;%NO_SUCH_VARIABLE%\tools`
	fi.scripts = gpo.Add("[Logoff]\r\n0CmdLine=C:\\Tools\\notes.cmd\r\n0Parameters=\r\n", quiet, "logoff")
	fi.entry = `"` + quiet + `" logon`
	fi.f = &wsl.Fake{List: []wsl.Distro{{Name: "Dev", Version: 2, Running: true}, {Name: "Off", Version: 2}}}
	fi.f.Script = func(distro, _, script string) (string, error) {
		switch {
		case script == inside.ProbeScript():
			return "probe=1\nstart_script=" + inside.StartScriptMark + "1\nend=1\n", nil
		case strings.Contains(script, `rm -rf "$R/usr/local/lib/wsl-spawner"`):
			fi.takenOut = append(fi.takenOut, distro)
			return "ok=1\n", nil
		}
		return "", errors.New("unexpected script")
	}
	pc := testPC()
	pc.OnWindows = true
	pc.Executable = func() (string, error) { return fi.exe, nil }
	pc.UserPath = func() (string, bool, error) { return fi.path, fi.expand, nil }
	pc.SetUserPath = func(v string, expand bool) error {
		if fi.pathErr != nil {
			return fi.pathErr
		}
		fi.wrote = append(fi.wrote, v)
		fi.path, fi.expand = v, expand
		return nil
	}
	pc.InUse = func(p string) bool { return fi.held[filepath.Base(p)] }
	pc.OthersRunning = func(string) (int, error) { return fi.others, nil }
	// As the real one does once the program has ended: its file, then the
	// folder if nothing else is in it.
	pc.DeleteAfterExit = func(exe, dir string) error {
		fi.deleted = append(fi.deleted, [2]string{exe, dir})
		_ = os.Remove(exe)
		if dir != "" {
			_ = os.Remove(dir)
		}
		return nil
	}
	pc.QuietExe = func() (string, error) { return quiet, nil }
	pc.ReadScripts = func() (string, error) { return fi.scripts, nil }
	pc.UninstallHook = func(exe string, drop ...string) error { fi.scripts = gpo.Remove(fi.scripts, exe, drop...); return nil }
	pc.RefreshPolicy = func() error { fi.refreshed++; return fi.refreshErr }
	// Not an administrator: the hook goes through the copy Windows starts
	// as one, which does as asked unless the answer is no.
	pc.RunElevated = func(exe string, args []string) (int, error) {
		if fi.declined {
			return 0, winapi.ErrDeclined
		}
		fi.scripts = gpo.Remove(fi.scripts, exe)
		return 0, os.WriteFile(args[len(args)-1], []byte("ok\n"), 0o644)
	}
	pc.LogonEntry = func(string) (string, bool, bool, error) { return fi.entry, fi.entry != "", false, nil }
	pc.DeleteLogonEntry = func(string) error { fi.entry = ""; return nil }
	fi.a = &app.App{Config: config.Default(), ConfigPath: filepath.Join(fi.settings, "config.toml"), WSL: fi.f, Dir: fi.state, PC: pc}
	return fi
}

// write makes the file at p, and its folder, with body.
func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// there reports whether p exists.
func there(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// run runs a command line on the made-up install, with no
// terminal unless input is given, which is then typed at one.
func (fi *fakeInstall) run(t *testing.T, input string, args ...string) (int, string, string) {
	t.Helper()
	if input != "" {
		o, e, code := cliWithInput(t, fi.a, input, args...)
		return code, o, e
	}
	var o, e bytes.Buffer
	in, outTerm := stdinIsTerminal, stdoutIsTerminal
	t.Cleanup(func() { stdinIsTerminal, stdoutIsTerminal = in, outTerm })
	stdinIsTerminal, stdoutIsTerminal = func() bool { return false }, func() bool { return false }
	t.Setenv("WSL_DISTRO_NAME", "")
	code := runWith(args, &o, &e, func() (*app.App, error) { return fi.a, fi.settingsErr })
	return code, o.String(), e.String()
}

// untouched fails the test unless the program, its folder, the settings,
// the history, the PATH, the hook, the sign-in program and the
// distributions are as installed made them.
func (fi *fakeInstall) untouched(t *testing.T) {
	t.Helper()
	for _, p := range []string{fi.exe, filepath.Join(fi.dir, "wsl-spawnerw.exe"), filepath.Join(fi.dir, "wsl-spawner"),
		filepath.Join(fi.settings, "config.toml"), filepath.Join(fi.state, "history.jsonl")} {
		if !there(p) {
			t.Errorf("%s was removed", p)
		}
	}
	if len(fi.wrote) != 0 || len(fi.deleted) != 0 || len(fi.takenOut) != 0 || fi.entry == "" || len(gpo.Logoff(fi.scripts)) != 2 {
		t.Errorf("the PATH was written %q, removals asked %q, taken out of %q, entry %q, scripts %q", fi.wrote, fi.deleted, fi.takenOut, fi.entry, fi.scripts)
	}
}

func TestUninstallTakesOutWhatItPutAndRemovesTheProgram(t *testing.T) {
	fi := installed(t, "wsl-spawner")
	code, out, errOut := fi.run(t, "y\n", "uninstall")
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	// The plan said each thing first.
	for _, want := range []string{"removes  the sign-out hook", "removes  the sign-in program", "removes  what wsl-spawner set up in Dev",
		"keeps    what wsl-spawner may have set up in Off", "and its entry in your PATH", "keeps    the settings in"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("plan: no %q in\n%s", want, errOut)
		}
	}
	// The hook and the sign-in program are gone; the other tool's script
	// stays.
	if s := gpo.Logoff(fi.scripts); len(s) != 1 || s[0].CmdLine != `C:\Tools\notes.cmd` || fi.entry != "" {
		t.Fatalf("scripts %+v, entry %q", s, fi.entry)
	}
	if !slices.Equal(fi.takenOut, []string{"Dev"}) {
		t.Fatalf("taken out of %q", fi.takenOut)
	}
	// The PATH lost the folder's entry and nothing else; the files went.
	if fi.path != `C:\Users\ann\bin;;%NO_SUCH_VARIABLE%\tools` {
		t.Fatalf("PATH %q", fi.path)
	}
	for _, f := range []string{"wsl-spawnerw.exe", "wsl-spawner", "wsl-spawner.exe.new"} {
		if there(filepath.Join(fi.dir, f)) {
			t.Errorf("%s stayed", f)
		}
	}
	if len(fi.deleted) != 1 || fi.deleted[0] != [2]string{fi.exe, fi.dir} || there(fi.dir) {
		t.Fatalf("deleted %q, folder there %v", fi.deleted, there(fi.dir))
	}
	// The settings and the history stay without --purge, and it says so.
	if !there(filepath.Join(fi.settings, "config.toml")) || !strings.Contains(out, "note: kept the settings in") {
		t.Fatalf("%s", out)
	}
}

func TestUninstallAnsweredNoChangesNothing(t *testing.T) {
	fi := installed(t, "wsl-spawner")
	if code, _, errOut := fi.run(t, "n\n", "uninstall"); code != exitFail || !strings.Contains(errOut, "not uninstalled") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	fi.untouched(t)
}

func TestUninstallWithoutATerminalWantsYes(t *testing.T) {
	fi := installed(t, "wsl-spawner")
	if code, _, errOut := fi.run(t, "", "uninstall"); code != exitUsage || !strings.Contains(errOut, "add --yes") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	fi.untouched(t)
}

func TestAHookThatStaysStopsTheUninstallBeforeAnythingGoes(t *testing.T) {
	// Left behind, every sign-out would start a program that is gone.
	fi := installed(t, "wsl-spawner")
	fi.declined = true
	code, _, errOut := fi.run(t, "", "uninstall", "--yes")
	if code != exitFail || !strings.Contains(errOut, "the sign-out hook stays") || !strings.Contains(errOut, "nothing was removed") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	fi.untouched(t)
}

func TestAHookThatCannotComeOutStopsTheUninstallBeforeWindowsAsks(t *testing.T) {
	// Another tool's key named twice: renumbered, Windows could take the
	// other one, so the hook cannot come out for certain. Nothing is asked
	// of Windows, nothing goes, and the words say how to put the file right:
	// trying again could never work.
	for _, input := range []string{"", "y\n"} {
		fi := installed(t, "wsl-spawner")
		fi.scripts += "5Parameters=a\r\n5Parameters=b\r\n"
		asked := 0
		fi.a.PC.RunElevated = func(string, []string) (int, error) { asked++; return 1, errors.New("asked") }
		args := []string{"uninstall"}
		if input == "" {
			args = append(args, "--yes")
		}
		code, _, errOut := fi.run(t, input, args...)
		if code != exitFail || asked != 0 || !strings.Contains(errOut, "nothing was removed: the sign-out hook cannot be taken out of the sign-out scripts as they are: scripts.ini: it names 5Parameters twice in [Logoff]") ||
			!strings.Contains(errOut, "put it right by hand, or in gpedit.msc") || strings.Contains(errOut, "tries again") || strings.Contains(errOut, "Uninstall? [y/N]") {
			t.Fatalf("%q: exit %d, asked %d: %s", input, code, asked, errOut)
		}
		fi.untouched(t)
	}
}

func TestUninstallTakesTheHookOutWithoutTheQuietBuild(t *testing.T) {
	// The build with no console was taken away: the uninstall still takes
	// the hook out first, through this program started as an administrator.
	fi := installed(t, "wsl-spawner")
	quiet := filepath.Join(fi.dir, "wsl-spawnerw.exe")
	if err := os.Remove(quiet); err != nil {
		t.Fatal(err)
	}
	fi.a.PC.QuietExe = func() (string, error) { return quiet, app.ErrNoQuietExe }
	// The copy Windows starts as an administrator: this program again, in
	// this process, as the program it was given.
	var started string
	admin := false
	fi.a.PC.Elevated = func() bool { return admin }
	fi.a.PC.RunElevated = func(exe string, args []string) (int, error) {
		started, admin = exe, true
		defer func() { admin = false }()
		return runWith(args, io.Discard, io.Discard, func() (*app.App, error) { return fi.a, nil }), nil
	}
	code, out, errOut := fi.run(t, "", "uninstall", "--yes")
	if code != exitOK || started != fi.exe || len(gpo.Logoff(fi.scripts)) != 1 || len(fi.deleted) != 1 {
		t.Fatalf("exit %d, started %q, scripts %q: %s%s", code, started, fi.scripts, out, errOut)
	}
	// No copy can be started as an administrator: nothing goes, and the
	// words say what works.
	fi = installed(t, "wsl-spawner")
	fi.a.PC.RunElevated = func(string, []string) (int, error) {
		return 0, errors.New("starting the program as an administrator: The system cannot find the file specified.")
	}
	code, _, errOut = fi.run(t, "", "uninstall", "--yes")
	if code != exitFail || !strings.Contains(errOut, "the sign-out hook stays: starting the program as an administrator") ||
		!strings.Contains(errOut, "in a terminal run as administrator, wsl-spawner hook remove takes it out, then wsl-spawner uninstall") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	fi.untouched(t)
}

func TestUninstallGoesOnWhenWindowsHasNotReadTheHooksRemoval(t *testing.T) {
	// The hook is out of the policy's files: the program goes, and the end
	// says that Windows has not read that yet.
	fi := installed(t, "wsl-spawner")
	fi.refreshErr = errors.New("gpupdate: exit status 1")
	code, out, errOut := fi.run(t, "", "uninstall", "--yes")
	if code != exitOK || len(gpo.Logoff(fi.scripts)) != 1 || len(fi.deleted) != 1 || fi.refreshed != 1 {
		t.Fatalf("exit %d, refreshed %d: %s%s", code, fi.refreshed, out, errOut)
	}
	if !strings.Contains(out, "note: took the sign-out hook out of the local Group Policy, but Windows has not read that yet (gpupdate: exit status 1)") {
		t.Fatalf("%s", out)
	}
}

func TestUninstallTakesOutTheHooksOfCopiesThatAreGone(t *testing.T) {
	// A moved copy's hook first, another tool's script, a second install's
	// hook, then this one's. The moved one's goes with this one's; the
	// second install's is that copy's to take out, and stays.
	fi := installed(t, "wsl-spawner")
	quiet := filepath.Join(fi.dir, "wsl-spawnerw.exe")
	fi.scripts = "[Logoff]\r\n" + hookOf(0, oldCopy) + "1CmdLine=C:\\Tools\\notes.cmd\r\n1Parameters=\r\n" + hookOf(2, secondCopy) + hookOf(3, quiet)
	fi.a.PC.LookAt, fi.a.PC.FixedDisk = lookAtCopies, onFixedDisks
	// The copy Windows starts as an administrator: this program again, in
	// this process, which works out the gone copies itself.
	admin := false
	fi.a.PC.Elevated = func() bool { return admin }
	fi.a.PC.RunElevated = func(exe string, args []string) (int, error) {
		admin = true
		defer func() { admin = false }()
		return runWith(args, io.Discard, io.Discard, func() (*app.App, error) { return fi.a, nil }), nil
	}
	code, out, errOut := fi.run(t, "y\n", "uninstall")
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	for _, want := range []string{
		"removes  the sign-out hook from the local Group Policy (Windows asks for an administrator)",
		`removes  the sign-out hook of a copy that is gone: "` + oldCopy + `" logoff` + "\n",
		`keeps    the sign-out hook of another copy: "` + secondCopy + `" logoff (that copy's own uninstall takes it out)`,
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("plan: no %q in\n%s", want, errOut)
		}
	}
	if s := gpo.Logoff(fi.scripts); len(s) != 2 || s[0].CmdLine != `C:\Tools\notes.cmd` || s[1].CmdLine != secondCopy {
		t.Fatalf("scripts %+v", s)
	}
	if !strings.Contains(out, `note: the sign-out hook of another copy stays: "`+secondCopy+`" logoff: that copy's own uninstall takes it out`) {
		t.Fatalf("%s", out)
	}
	// Only a copy's hook that is gone, with none of this one's: it goes, and
	// the uninstall says so.
	fi = installed(t, "wsl-spawner")
	fi.scripts = "[Logoff]\r\n" + hookOf(0, oldCopy)
	fi.a.PC.LookAt, fi.a.PC.FixedDisk, fi.a.PC.Elevated = lookAtCopies, onFixedDisks, func() bool { return true }
	code, out, errOut = fi.run(t, "", "uninstall", "--yes")
	if code != exitOK || len(gpo.Logoff(fi.scripts)) != 0 || !strings.Contains(out, "note: took the sign-out hook of a copy that is gone out of the local Group Policy") {
		t.Fatalf("exit %d, scripts %q: %s%s", code, fi.scripts, out, errOut)
	}
}

func TestUninstallSaysAHookItCannotReadStays(t *testing.T) {
	// Sign-out scripts that cannot be read are not taken for none: the
	// plan and the end say that a hook of wsl-spawner's among them stays.
	fi := installed(t, "wsl-spawner")
	fi.a.PC.ReadScripts = func() (string, error) { return "", errors.New("scripts.ini: it is UTF-16 BE") }
	code, out, errOut := fi.run(t, "y\n", "uninstall")
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "keeps    any sign-out hook of wsl-spawner's: reading the sign-out scripts: scripts.ini: it is UTF-16 BE") {
		t.Fatalf("plan:\n%s", errOut)
	}
	if !strings.Contains(out, "note: any sign-out hook of wsl-spawner's stays: reading the sign-out scripts: scripts.ini: it is UTF-16 BE") {
		t.Fatalf("end:\n%s", out)
	}
}

func TestUninstallPurgeRemovesOnlyTheProgramsFiles(t *testing.T) {
	fi := installed(t, "wsl-spawner")
	// A file of the person's own in the settings folder.
	mine := filepath.Join(fi.settings, "notes.txt")
	write(t, mine, "mine")
	write(t, filepath.Join(fi.state, "crash-12-34.txt"), "x")
	code, out, errOut := fi.run(t, "", "uninstall", "--purge", "--yes")
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if there(filepath.Join(fi.settings, "config.toml")) || there(fi.state) || !there(mine) {
		t.Fatalf("settings %v, state %v, mine %v", there(filepath.Join(fi.settings, "config.toml")), there(fi.state), there(mine))
	}
	if !strings.Contains(out, "kept "+fi.settings+": other files are in it") {
		t.Fatalf("%s", out)
	}
}

func TestUninstallKeepsAFolderThatIsNotTheInstallsOwn(t *testing.T) {
	// A bin folder of the person's own, with other programs in it.
	fi := installed(t, "bin")
	write(t, filepath.Join(fi.dir, "other.exe"), "x")
	code, out, _ := fi.run(t, "", "uninstall", "--yes")
	if code != exitOK || !there(filepath.Join(fi.dir, "other.exe")) || len(fi.wrote) != 0 || fi.deleted[0][1] != "" {
		t.Fatalf("exit %d, wrote %q, deleted %q\n%s", code, fi.wrote, fi.deleted, out)
	}
	if !strings.Contains(out, "note: kept "+fi.dir+" and any PATH entry for it: other files are in it") {
		t.Fatalf("%s", out)
	}
}

func TestUninstallRefusesWhatWouldStopHalfWay(t *testing.T) {
	fi := installed(t, "wsl-spawner")
	fi.others = 1
	if code, _, errOut := fi.run(t, "", "uninstall", "--yes"); code != exitFail || !strings.Contains(errOut, "another wsl-spawner is running") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	fi.untouched(t)
	fi.others = 0
	fi.held["wsl-spawner"] = true
	if code, _, errOut := fi.run(t, "", "uninstall", "--yes"); code != exitFail || !strings.Contains(errOut, "close the wsl-spawner screens open in WSL") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	fi.untouched(t)
}

func TestUninstallKeepsTheProgramWhenThePathCannotBeWritten(t *testing.T) {
	fi := installed(t, "wsl-spawner")
	fi.pathErr = errors.New("Access is denied")
	code, _, errOut := fi.run(t, "", "uninstall", "--yes")
	if code != exitFail || !strings.Contains(errOut, "the program was not removed") || !there(fi.exe) || len(fi.deleted) != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestUninstallLeavesAScoopInstallToScoop(t *testing.T) {
	// Scoop's apps\wsl-spawner\current, with its manifest beside the
	// program.
	fi := installed(t, filepath.Join("scoop", "apps", "wsl-spawner", "current"))
	write(t, filepath.Join(fi.dir, "scoop-manifest.json"), "{}")
	if code, _, errOut := fi.run(t, "", "uninstall", "--yes"); code != exitFail || !strings.Contains(errOut, "installed with Scoop") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	fi.untouched(t)
	// With --purge, what wsl-spawner put on the PC goes, and the settings;
	// the program is Scoop's to remove.
	code, out, errOut := fi.run(t, "", "uninstall", "--purge", "--yes")
	if code != exitOK || fi.entry != "" || len(gpo.Logoff(fi.scripts)) != 1 || !there(fi.exe) || len(fi.deleted) != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if !strings.Contains(out, "scoop uninstall wsl-spawner removes the program") {
		t.Fatalf("%s", out)
	}
}

func TestUninstallIsForTheWindowsProgram(t *testing.T) {
	fi := installed(t, "wsl-spawner")
	fi.a.PC.OnWindows = false
	if code, _, errOut := fi.run(t, "", "uninstall", "--yes"); code != exitFail || !strings.Contains(errOut, "runs on Windows") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	fi.untouched(t)
}

func TestPathWithoutDropsTheFolderAsWindowsReadsIt(t *testing.T) {
	lookup := func(name string) (string, bool) {
		if name == "LOCALAPPDATA" {
			return `C:\Users\ann\AppData\Local`, true
		}
		return "", false
	}
	dir := `C:\Users\ann\AppData\Local\Programs\wsl-spawner`
	for value, want := range map[string]string{
		`C:\a;` + dir + `;C:\b`:                          `C:\a;C:\b`,
		`C:\a;%LOCALAPPDATA%\Programs\wsl-spawner\;C:\b`: `C:\a;C:\b`,
		`"` + dir + `";C:\a`:                             `C:\a`,
		`C:\a;` + strings.ToUpper(dir):                   `C:\a`,
		`C:\a;;C:\b`:                                     `C:\a;;C:\b`,
		`C:\a;%NOT_SET%\Programs\wsl-spawner;"C:\x;y"`:   `C:\a;%NOT_SET%\Programs\wsl-spawner;"C:\x;y"`,
	} {
		if got, _ := pathWithout(value, dir, lookup); got != want {
			t.Errorf("%q: got %q, want %q", value, got, want)
		}
	}
}
