package cli

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/front/api"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// box is a made-up PC whose distributions answer the scripts as a real
// one would: a set-up script sets the distribution up, a removal takes it
// out, and the probe says so after.
type box struct {
	f *wsl.Fake
	a *app.App
	// setUp and kept say, by name, what is in each distribution now; old
	// that the start script in it is from an older version, newer that it
	// is from a newer one.
	setUp, kept, old, newer map[string]bool
	// scripts notes each script run, as "NAME:setup-keep", "NAME:setup",
	// "NAME:remove" or "NAME:probe".
	scripts []string
	// resumed is what the start service did at the last start, by name.
	resumed map[string]string
	// entry is the sign-in program's command line, "" for none.
	entry string
}

// newBox is a made-up PC with Ubuntu-24.04 running, Debian stopped, both
// not set up, and Docker Desktop's own.
func newBox(t *testing.T) *box {
	t.Helper()
	b := &box{setUp: map[string]bool{}, kept: map[string]bool{}, old: map[string]bool{}, newer: map[string]bool{}, resumed: map[string]string{}}
	b.f = &wsl.Fake{List: []wsl.Distro{
		{Name: "Ubuntu-24.04", Version: 2, Running: true},
		{Name: "Debian", Version: 2},
		{Name: "docker-desktop", Version: 2, Running: true},
	}}
	b.f.Script = func(distro, user, script string) (string, error) {
		switch {
		case script == inside.ProbeScript():
			b.scripts = append(b.scripts, distro+":probe")
			out := "probe=1\n"
			if b.setUp[distro] {
				version := inside.StartScriptVersion
				if b.old[distro] {
					version = "1"
				}
				// One a later wsl-spawner put, after which this one came back.
				if b.newer[distro] {
					version = "99"
				}
				out += "start_script=" + inside.StartScriptMark + version + "\nunit=" + inside.StartUnit + "\tinactive\tenabled\n" + b.resumed[distro]
			}
			if b.kept[distro] {
				out += "unit=" + inside.KeepAliveUnit + "\tactive\tenabled\n"
			}
			return out + "end=1\n", nil
		case strings.Contains(script, "rm -rf \"$R/usr/local/lib/wsl-spawner\""):
			b.scripts = append(b.scripts, distro+":remove")
			b.setUp[distro], b.kept[distro] = false, false
			return "ok=1\n", nil
		case strings.Contains(script, "systemctl enable --quiet "+inside.StartUnit):
			keep := strings.Contains(script, "enable --quiet --now "+inside.KeepAliveUnit)
			// The signal is this distribution's letters and digits and a secret of
			// 32 hex digits, new at each set-up.
			if keep && !regexp.MustCompile(inside.Signal(distro, "")+`[0-9a-f]{32}\b`).MatchString(script) {
				return "", errors.New("the keep-running service waits for another name")
			}
			w := distro + ":setup"
			if keep {
				w += "-keep"
			}
			b.scripts = append(b.scripts, w)
			b.setUp[distro], b.kept[distro], b.old[distro] = true, keep, false
			return "ok=1\n", nil
		}
		return "", errors.New("unexpected script")
	}
	c := config.Default()
	b.a = &app.App{Config: c, ConfigPath: filepath.Join(t.TempDir(), "config.toml"), WSL: b.f, Dir: t.TempDir()}
	// The sign-in program, held here, and the quiet build beside this one.
	b.a.PC = testPC()
	b.a.PC.LogonEntry = func(name string) (string, bool, bool, error) { return b.entry, b.entry != "", false, nil }
	b.a.PC.SetLogonEntry = func(name, cmd string) error { b.entry = cmd; return nil }
	b.a.PC.DeleteLogonEntry = func(name string) error { b.entry = ""; return nil }
	b.a.PC.QuietExe = func() (string, error) { return `C:\Tools\wsl-spawnerw.exe`, nil }
	b.a.PC.FixedDisk = onFixedDisks
	return b
}

// changes are the scripts that changed something, without the probes.
func (b *box) changes() []string {
	return slices.DeleteFunc(slices.Clone(b.scripts), func(s string) bool { return strings.HasSuffix(s, ":probe") })
}

func TestSetNeedsAChoiceAndRefusesTwoWays(t *testing.T) {
	b := newBox(t)
	if _, errOut, code := cli(t, b.a, false, "set", "Ubuntu-24.04"); code != exitUsage || !strings.Contains(errOut, "set needs a choice") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if _, errOut, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--logon", "--keep=false", "--yes"); code != exitUsage || !strings.Contains(errOut, "--keep=false and --logon do not go together: starting at sign-in keeps it running") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if _, errOut, code := cli(t, b.a, false, "set", "docker-desktop", "--keep", "--yes"); code != exitUsage || !strings.Contains(errOut, "Docker Desktop's") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if len(b.changes()) != 0 {
		t.Fatalf("changed %q", b.changes())
	}
}

func TestSetKeepSetsItUpInside(t *testing.T) {
	b := newBox(t)
	out, errOut, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--keep", "--yes")
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	for _, want := range []string{"set wsl-spawner up in it", "keep it running", "Ubuntu-24.04: set up, kept running"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	if !slices.Equal(b.changes(), []string{"Ubuntu-24.04:setup-keep"}) || b.entry != "" {
		t.Fatalf("changes %q, entry %q", b.changes(), b.entry)
	}
	// Nothing to change the second time.
	if out, _, _ := cli(t, b.a, false, "set", "Ubuntu-24.04", "--keep", "--yes"); !strings.Contains(out, "nothing to change") {
		t.Fatalf("%s", out)
	}
}

func TestSetLogonAddsTheSignInProgramAndTheSetting(t *testing.T) {
	b := newBox(t)
	if _, errOut, code := cli(t, b.a, false, "set", "ubuntu-24.04", "--logon", "--yes"); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if b.entry != `"C:\Tools\wsl-spawnerw.exe" logon` {
		t.Fatalf("entry %q", b.entry)
	}
	// The setting names it as WSL spells it, and the file loads.
	c, err := config.Load(b.a.ConfigPath)
	if err != nil || !slices.Equal(c.Logon, []string{"Ubuntu-24.04"}) {
		t.Fatalf("%+v %v", c.Logon, err)
	}
	// The last one off at sign-in takes the program away; it stays set up
	// and kept running.
	if _, errOut, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--logon=false", "--yes"); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if c, _ := config.Load(b.a.ConfigPath); len(c.Logon) != 0 || b.entry != "" || !b.kept["Ubuntu-24.04"] {
		t.Fatalf("logon %q, entry %q, kept %v", c.Logon, b.entry, b.kept)
	}
}

func TestSetLogonPutsTheSignInProgramRightWhenTheChoiceIsMadeAlready(t *testing.T) {
	b := newBox(t)
	// The settings are written first; then adding the program fails.
	b.a.PC.SetLogonEntry = func(string, string) error { return errors.New("Access is denied") }
	if _, errOut, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--logon", "--yes"); code != exitFail || !strings.Contains(errOut, "adding the sign-in program") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if c, _ := config.Load(b.a.ConfigPath); !slices.Equal(c.Logon, []string{"Ubuntu-24.04"}) || b.entry != "" {
		t.Fatalf("logon %q, entry %q", c.Logon, b.entry)
	}
	// Asked again, as check says to, the plan says it and it is put back.
	b.a.PC.SetLogonEntry = func(_, cmd string) error { b.entry = cmd; return nil }
	out, errOut, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--logon", "--yes")
	if code != exitOK || b.entry != `"C:\Tools\wsl-spawnerw.exe" logon` || !strings.Contains(out, "put the sign-in program back") {
		t.Fatalf("exit %d, entry %q: %s%s", code, b.entry, out, errOut)
	}
	// One that starts another copy is made to start this one, and the
	// JSON says something changed.
	b.entry = `"D:\old\wsl-spawnerw.exe" logon`
	out, _, code = cli(t, b.a, false, "set", "Ubuntu-24.04", "--logon", "--yes", "--json")
	var got api.SetResult
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != exitOK || !got.Changed || b.entry != `"C:\Tools\wsl-spawnerw.exe" logon` {
		t.Fatalf("exit %d, entry %q, %+v %v: %s", code, b.entry, got, err, out)
	}
	// As the settings say: nothing to change.
	if out, _, _ := cli(t, b.a, false, "set", "Ubuntu-24.04", "--logon", "--yes"); !strings.Contains(out, "nothing to change") {
		t.Fatalf("%s", out)
	}
	// One left over, nothing starting at sign-in: --logon=false for any
	// distribution takes it out, as check says.
	if _, errOut, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--logon=false", "--yes"); code != exitOK || b.entry != "" {
		t.Fatalf("exit %d, entry %q: %s", code, b.entry, errOut)
	}
	b.entry = `"C:\Tools\wsl-spawnerw.exe" logon`
	out, errOut, code = cli(t, b.a, false, "set", "Debian", "--logon=false", "--yes")
	if code != exitOK || b.entry != "" || !strings.Contains(out, "take out the sign-in program left over") || slices.Contains(b.f.CallList(), "start:Debian") {
		t.Fatalf("exit %d, entry %q, calls %q: %s%s", code, b.entry, b.f.CallList(), out, errOut)
	}
	// Another choice says nothing of the sign-in program, and leaves it.
	b.entry = `"C:\Tools\wsl-spawnerw.exe" logon`
	if out, _, _ := cli(t, b.a, false, "set", "Ubuntu-24.04", "--keep", "--yes"); !strings.Contains(out, "nothing to change") || b.entry == "" {
		t.Fatalf("entry %q: %s", b.entry, out)
	}
}

func TestSetSkipTakesItOutAndLeavesItToWSL(t *testing.T) {
	b := newBox(t)
	if _, _, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--logon", "--yes"); code != exitOK {
		t.Fatal(code)
	}
	b.scripts = nil
	out, errOut, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--skip", "--yes")
	if code != exitOK || !strings.Contains(out, "take wsl-spawner out of it") || !strings.Contains(out, "leave it to WSL") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	c, _ := config.Load(b.a.ConfigPath)
	if !slices.Equal(b.changes(), []string{"Ubuntu-24.04:remove"}) || !slices.Equal(c.Skip, []string{"Ubuntu-24.04"}) || len(c.Logon) != 0 || b.entry != "" {
		t.Fatalf("changes %q, skip %q, logon %q, entry %q", b.changes(), c.Skip, c.Logon, b.entry)
	}
	// Left to WSL, it is not wsl-spawner's to stop.
	if _, errOut, code := cli(t, b.a, false, "stop", "--yes", "Ubuntu-24.04"); code != exitUsage || !strings.Contains(errOut, "left to WSL") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestSetStartsAStoppedDistributionToChangeItInside(t *testing.T) {
	b := newBox(t)
	out, errOut, code := cli(t, b.a, false, "set", "Debian", "--managed", "--yes")
	if code != exitOK || !strings.Contains(out, "It is not running: it starts first") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if i, j := slices.Index(b.f.CallList(), "start:Debian"), slices.Index(b.scripts, "Debian:setup"); i < 0 || j < 0 {
		t.Fatalf("calls %q, scripts %q", b.f.CallList(), b.scripts)
	}
	// A change of the settings alone does not start it.
	b2 := newBox(t)
	b2.a.Config.Skip = []string{"Debian"}
	if _, _, code := cli(t, b2.a, false, "set", "Debian", "--skip=false", "--yes"); code != exitOK || slices.Contains(b2.f.CallList(), "start:Debian") {
		t.Fatalf("exit %d, calls %q", code, b2.f.CallList())
	}
}

func TestATurnOffOnAStoppedDistributionReadsItFirst(t *testing.T) {
	// Debian is stopped, so what is set up in it is not known. Taking
	// wsl-spawner out, no longer keeping it running and leaving it to WSL
	// each start it, read it, and take out what is there; where nothing
	// is, nothing changes inside.
	// The plan says each as a change from the other way.
	said := map[string][]string{
		"--managed=false": {"take wsl-spawner out of it"},
		"--keep=false":    {"no longer keep it running"},
		"--skip":          {"take wsl-spawner out of it", "leave it to WSL"},
	}
	for _, flags := range [][]string{{"--managed=false"}, {"--keep=false"}, {"--skip"}} {
		b := newBox(t)
		b.setUp["Debian"], b.kept["Debian"] = true, true
		args := append([]string{"set", "Debian"}, append(flags, "--yes")...)
		out, errOut, code := cli(t, b.a, false, args...)
		if code != exitOK || !strings.Contains(out, "it starts first, and only what is not so already changes") {
			t.Fatalf("%v: exit %d: %s%s", flags, code, out, errOut)
		}
		for _, w := range said[flags[0]] {
			if !strings.Contains(out, w) {
				t.Fatalf("%v: the plan does not say %q:\n%s", flags, w, out)
			}
		}
		if !slices.Contains(b.f.CallList(), "start:Debian") {
			t.Fatalf("%v: not started: %q", flags, b.f.CallList())
		}
		switch flags[0] {
		case "--keep=false":
			// Still set up, no longer kept: set up again without the
			// keep-running service.
			if !slices.Contains(b.changes(), "Debian:setup") || b.kept["Debian"] {
				t.Fatalf("%v: %q kept=%v", flags, b.changes(), b.kept["Debian"])
			}
		default:
			if !slices.Contains(b.changes(), "Debian:remove") {
				t.Fatalf("%v: nothing taken out: %q", flags, b.changes())
			}
		}
	}
	// Nothing set up in it: started and read, and nothing to change.
	b := newBox(t)
	out, errOut, code := cli(t, b.a, false, "set", "Debian", "--managed=false", "--yes")
	if code != exitOK || !strings.Contains(out, "Debian: nothing to change") || len(b.changes()) != 0 {
		t.Fatalf("exit %d: %s%s %q", code, out, errOut, b.changes())
	}
}

func TestStartSaysWhatCameBack(t *testing.T) {
	b := newBox(t)
	b.setUp["Debian"] = true
	b.resumed["Debian"] = "resumed=at " + strconv.FormatInt(time.Now().Unix(), 10) + "\nresumed=docker bbb db started\nresumed=docker aaa web running\nresumed=docker ccc old gone\n"
	out, errOut, code := cli(t, b.a, false, "start", "debian")
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if !strings.Contains(out, "Debian started; brought back db; back by their restart policy: web; old gone") {
		t.Fatalf("%s", out)
	}
	all, _ := history.Open(b.a.Dir).All()
	if len(all) != 1 || all[0].What != "start" || all[0].By != "hand" || all[0].Distros[0].Result != "started" {
		t.Fatalf("%+v", all)
	}
	// Docker Desktop's is its own to start.
	if _, errOut, code := cli(t, b.a, false, "start", "docker-desktop"); code != exitUsage || !strings.Contains(errOut, "starts it itself") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestLogonStartsTheSettingsListInItsOrder(t *testing.T) {
	b := newBox(t)
	b.f.List = append(b.f.List, wsl.Distro{Name: "Work", Version: 2})
	b.a.Config.Logon = []string{"Work", "gone-since", "Debian", "docker-desktop"}
	if _, errOut, code := cli(t, b.a, false, "logon"); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var starts []string
	for _, c := range b.f.CallList() {
		if strings.HasPrefix(c, "start:") {
			starts = append(starts, c)
		}
	}
	// In the settings' order; one no longer there, and one of another
	// tool's, are passed by.
	if !slices.Equal(starts, []string{"start:Work", "start:Debian"}) {
		t.Fatalf("started %q", starts)
	}
	all, _ := history.Open(b.a.Dir).All()
	if len(all) != 1 || all[0].By != "logon" || len(all[0].Distros) != 2 {
		t.Fatalf("%+v", all)
	}
}

func TestASetOfAnotherChoiceKeepsTheSignInOrder(t *testing.T) {
	// The sign-in program starts the settings' list in its order: keeping
	// Ubuntu-24.04 running must not move it after Debian.
	b := newBox(t)
	b.a.Config.Logon = []string{"Ubuntu-24.04", "Debian"}
	if _, errOut, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--keep", "--yes"); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !slices.Equal(b.a.Config.Logon, []string{"Ubuntu-24.04", "Debian"}) || !b.kept["Ubuntu-24.04"] {
		t.Fatalf("logon %q, kept %v", b.a.Config.Logon, b.kept)
	}
	// A name written another way keeps its place, spelled as WSL spells it;
	// a new one goes after the others; one taken out leaves the rest.
	for _, c := range []struct {
		names []string
		name  string
		in    bool
		want  []string
	}{
		{[]string{"ubuntu-24.04", "Debian"}, "Ubuntu-24.04", true, []string{"Ubuntu-24.04", "Debian"}},
		{[]string{"Debian"}, "Ubuntu-24.04", true, []string{"Debian", "Ubuntu-24.04"}},
		{[]string{"Work", "Ubuntu-24.04", "Debian"}, "Ubuntu-24.04", false, []string{"Work", "Debian"}},
	} {
		if got := withName(c.names, c.name, c.in); !slices.Equal(got, c.want) {
			t.Errorf("%q with %s %v: %q, want %q", c.names, c.name, c.in, got, c.want)
		}
	}
}

func TestASetRefusedForTheSignInProgramSaysWhoStartsAtSignIn(t *testing.T) {
	// This copy is in a distribution's own folder, which Windows cannot
	// start a program from at sign-in, and Ubuntu-24.04 starts at sign-in:
	// every change would end with the sign-in program made to start this
	// copy. Debian's --keep has nothing to do with sign-in; the words say
	// what ties it to the sign-in program.
	inWSL := `\\wsl.localhost\Ubuntu-24.04\home\ann\bin\wsl-spawnerw.exe`
	b := newBox(t)
	b.a.PC.QuietExe = func() (string, error) { return inWSL, nil }
	b.a.Config.Logon, b.entry = []string{"Ubuntu-24.04"}, `"C:\Tools\wsl-spawnerw.exe" logon`
	_, errOut, code := cli(t, b.a, false, "set", "Debian", "--keep", "--yes")
	want := "while Ubuntu-24.04 starts at sign-in, a change makes the sign-in program start this copy: the sign-in program must start a program on a fixed disk of this PC, and " + inWSL + " is not on one"
	if code != exitFail || !strings.Contains(errOut, want) || !strings.Contains(errOut, "install wsl-spawner on Windows with its install script") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// Refused before anything changed: nothing started or set up, the
	// sign-in program as it was.
	if len(b.changes()) != 0 || b.entry != `"C:\Tools\wsl-spawnerw.exe" logon` || slices.Contains(b.f.CallList(), "start:Debian") {
		t.Fatalf("changes %q, entry %q, calls %q", b.changes(), b.entry, b.f.CallList())
	}
	// Two that start at sign-in are both named, in their order; so for a
	// running distribution, read before the question.
	b.a.Config.Logon = []string{"Work", "Debian"}
	if _, errOut, _ := cli(t, b.a, false, "set", "Ubuntu-24.04", "--keep", "--yes"); !strings.Contains(errOut, "while Work and Debian start at sign-in, a change makes the sign-in program start this copy: ") {
		t.Fatalf("%s", errOut)
	}
	// A change that starts the distribution at sign-in is about the sign-in
	// program already: the words are the refusal alone.
	b.a.Config.Logon, b.entry = nil, ""
	_, errOut, code = cli(t, b.a, false, "set", "Ubuntu-24.04", "--logon", "--yes")
	if code != exitFail || !strings.Contains(errOut, "the sign-in program must start a program on a fixed disk") || strings.Contains(errOut, "while ") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestSetAndStartJSON(t *testing.T) {
	b := newBox(t)
	out, _, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--keep", "--yes", "--json")
	var got api.SetResult
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != exitOK || !got.Changed || !got.Managed || !got.Keep || got.Logon {
		t.Fatalf("%v %d %+v\n%s", err, code, got, out)
	}
	b.setUp["Debian"] = true
	b.resumed["Debian"] = "resumed=at " + strconv.FormatInt(time.Now().Unix(), 10) + "\nresumed=docker bbb db started\n"
	out, _, code = cli(t, b.a, false, "start", "--json", "Debian")
	var st api.StartResult
	if err := json.Unmarshal([]byte(out), &st); err != nil || code != exitOK || len(st.Results) != 1 || st.Results[0].Result != "started" ||
		len(st.Results[0].Back) != 1 || st.Results[0].Back[0] != (api.CameBack{Engine: "docker", Name: "db", What: "started"}) {
		t.Fatalf("%v %d %+v\n%s", err, code, st, out)
	}
}

func TestManagedPutsTheNewerStartScriptWhereTheOneInItIsOlder(t *testing.T) {
	// The start script runs as root at each start of the distribution. One
	// from an older version is what check says to put again with --managed,
	// which said "nothing to change" while it was set up.
	b := newBox(t)
	b.setUp["Ubuntu-24.04"], b.old["Ubuntu-24.04"] = true, true
	out, errOut, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--managed", "--yes")
	if code != exitOK || !strings.Contains(out, "put wsl-spawner's newer start script in it") || strings.Contains(out, "nothing to change") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if !slices.Contains(b.changes(), "Ubuntu-24.04:setup") || b.old["Ubuntu-24.04"] {
		t.Fatalf("scripts %q, old %v", b.changes(), b.old)
	}
	// Put now: the same again changes nothing.
	before := len(b.changes())
	if out, _, code := cli(t, b.a, false, "set", "Ubuntu-24.04", "--managed", "--yes"); code != exitOK || !strings.Contains(out, "nothing to change") || len(b.changes()) != before {
		t.Fatalf("exit %d: %s", code, out)
	}
	// A script from a newer version, which a later wsl-spawner put before
	// this one came back, is not older: it stays as it is.
	b3 := newBox(t)
	b3.setUp["Ubuntu-24.04"], b3.newer["Ubuntu-24.04"] = true, true
	if out, errOut, code := cli(t, b3.a, false, "set", "Ubuntu-24.04", "--managed", "--yes"); code != exitOK || !strings.Contains(out, "nothing to change") || len(b3.changes()) != 0 {
		t.Fatalf("exit %d, scripts %q: %s%s", code, b3.changes(), out, errOut)
	}
	// A change that does not ask for --managed leaves an older script to
	// the change: --skip=false on a distribution that is not left to WSL
	// says nothing to change, as it did.
	b2 := newBox(t)
	b2.setUp["Ubuntu-24.04"], b2.old["Ubuntu-24.04"] = true, true
	if out, _, code := cli(t, b2.a, false, "set", "Ubuntu-24.04", "--skip=false", "--yes"); code != exitOK || !strings.Contains(out, "nothing to change") {
		t.Fatalf("exit %d: %s", code, out)
	}
}
