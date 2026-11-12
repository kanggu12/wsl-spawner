package check

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/engine/distros"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// keys lists a group's findings as MARK:KEY words.
func keys(g Group) []string {
	var out []string
	for _, it := range g.Items {
		out = append(out, []string{"ok", "info", "warn", "bad"}[it.Mark]+":"+strings.TrimPrefix(it.Key, "check."))
	}
	return out
}

// find is the group named key (and arg, for a distribution).
func find(t *testing.T, gs []Group, key, arg string) Group {
	t.Helper()
	for _, g := range gs {
		if g.Key == key && g.Arg == arg {
			return g
		}
	}
	t.Fatalf("no group %s %s", key, arg)
	return Group{}
}

// now is the made-up PC's clock.
var now = time.Unix(1800000000, 0)

// setUp is a probe of a distribution wsl-spawner is set up in, whose last
// start ended in its stop.
func setUp() *inside.Probe {
	return &inside.Probe{BootID: "b", InitStart: "300", Systemd: "running", Interop: true, Now: now,
		StartScript: inside.StartScriptMark + inside.StartScriptVersion,
		Units:       map[string]inside.Unit{inside.KeepAliveUnit: {Active: "active", Enabled: "enabled"}},
		Runs:        []inside.Run{{ID: "b@100", At: now.Add(-time.Hour)}, {ID: "b@300", At: now}},
		LastStop:    &inside.StopRecord{Run: "b@100", Result: "clean", At: now.Add(-time.Minute)},
		Engines:     map[string]bool{"docker": true}, Complete: true}
}

// base is a PC where all is as it should be: the hook last, Windows waiting
// long enough, one distribution set up, kept running and started at
// sign-in, its program there.
func base() Inputs {
	c := config.Default()
	c.Logon = []string{"Dev"}
	// Its probe asked and answered within the second, on Windows' clock.
	return Inputs{Config: c, WSLVersion: "3.0.5.0",
		Distros: []distros.Distro{{Distro: wsl.Distro{Name: "Dev", Version: 2, Running: true}, AtLogon: true, Probe: setUp(),
			Asked: now, Answered: now}},
		Hook:  Hook{Installed: true, Last: true, Count: 2, MaxWait: 600 * time.Second},
		Logon: Logon{Present: true, Command: `"C:\x\wsl-spawnerw.exe" logon`, Want: `"C:\x\wsl-spawnerw.exe" logon`}}
}

func TestAPCWhereAllIsWellHasNothingToDo(t *testing.T) {
	gs := Run(base())
	if Worst(gs) != OK {
		for _, g := range gs {
			t.Logf("%s %s: %q", g.Key, g.Arg, keys(g))
		}
		t.Fatalf("worst %v", Worst(gs))
	}
	if got := keys(find(t, gs, "check.g.distro", "Dev")); !slices.Equal(got, []string{"ok:d.systemd", "ok:d.end.clean", "ok:d.kept"}) {
		t.Fatalf("%q", got)
	}
	if got := keys(find(t, gs, "check.g.stopping", "")); !slices.Equal(got, []string{"ok:hook.last", "ok:wait.enough"}) {
		t.Fatalf("%q", got)
	}
}

func TestTheHookAndWindowsWait(t *testing.T) {
	in := base()
	in.Hook = Hook{Count: 3, MaxWait: 300 * time.Second, StopsWSL: []string{`C:\x\old-hook.cmd`}}
	got := keys(find(t, Run(in), "check.g.stopping", ""))
	// No hook, another script that stops WSL first, and a wait shorter
	// than the stop limit and a minute.
	if !slices.Equal(got, []string{"warn:hook.none", "warn:hook.otherwsl", "warn:wait.short"}) {
		t.Fatalf("%q", got)
	}
	in.Hook = Hook{Installed: true, MaxWait: 0}
	if got := keys(find(t, Run(in), "check.g.stopping", "")); !slices.Equal(got, []string{"warn:hook.notlast", "ok:wait.unbounded"}) {
		t.Fatalf("%q", got)
	}
	in.Hook = Hook{Err: errors.New("access denied")}
	if got := keys(find(t, Run(in), "check.g.stopping", "")); !slices.Equal(got, []string{"warn:hook.unread"}) {
		t.Fatalf("%q", got)
	}
	// Last of the sign-out scripts, but PowerShell ones run after it: said,
	// and what to do about one that uses WSL.
	in.Hook = Hook{Installed: true, Last: true, Count: 2, PowerShell: 1, MaxWait: 0}
	g := find(t, Run(in), "check.g.stopping", "")
	if got := keys(g); !slices.Equal(got, []string{"warn:hook.powershell", "ok:wait.unbounded"}) || g.Items[0].Do != "check.hook.do.powershell" {
		t.Fatalf("%q %+v", got, g.Items[0])
	}
	// The policy itself has them run last: said as its choice, with no
	// "unless".
	in.Hook = Hook{Installed: true, Last: true, Count: 2, PowerShell: 1, PowerShellLast: true, MaxWait: 0}
	if got := keys(find(t, Run(in), "check.g.stopping", "")); !slices.Equal(got, []string{"warn:hook.pslast", "ok:wait.unbounded"}) {
		t.Fatalf("%q", got)
	}
	// No hook of this copy's, but another copy's runs: said for what it is,
	// not as no hook at all.
	in.Hook = Hook{Count: 1, Copies: []string{`E:\second\wsl-spawnerw.exe`}, MaxWait: 0}
	g = find(t, Run(in), "check.g.stopping", "")
	if got := keys(g); !slices.Equal(got, []string{"info:hook.copy", "ok:wait.unbounded"}) || g.Items[0].Args[0] != `E:\second\wsl-spawnerw.exe` {
		t.Fatalf("%q %+v", got, g.Items[0])
	}
}

func TestADistributionsFindings(t *testing.T) {
	in := base()
	p := setUp()
	// Ended without wsl-spawner's stop, systemd degraded, kept running with
	// no interop, a container that waits longer than the limit, one with no
	// policy under resume = policy, one that did not come back, and a
	// clock a minute off.
	p.LastStop = nil
	p.Systemd, p.Failed = "degraded", []string{"x.service"}
	p.Interop = false
	p.Containers = []inside.Container{
		{Name: "db", State: "running", Policy: "always", StopTimeout: 400 * time.Second},
		{Name: "job", State: "running", Policy: "no"},
		// Run with --rm: nothing brings it back, so no setting is said.
		{Name: "once", State: "running", Policy: "no", AutoRemove: true},
	}
	p.Resumed = []inside.Resumed{{Bring: inside.Bring{Name: "web"}, What: "exited"}}
	p.Now = now.Add(time.Minute)
	in.Config.Resume = "policy"
	in.Distros[0].Probe = p
	got := keys(find(t, Run(in), "check.g.distro", "Dev"))
	want := []string{"warn:d.degraded", "bad:d.end.cut", "bad:d.keepnointerop", "warn:d.longstop", "warn:d.nopolicy", "warn:d.notback", "warn:d.clock"}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	for _, it := range find(t, Run(in), "check.g.distro", "Dev").Items {
		if it.Key == "check.d.nopolicy" && (it.Args[0] != 1 || it.Args[1] != "job") {
			t.Fatalf("%+v", it)
		}
	}
}

func TestADockerAsleepBehindItsSocketIsSaidForTheLook(t *testing.T) {
	// Its daemon stopped by hand, its socket listening, as Docker's
	// packages leave it: not asked, as asking starts it, and nothing of it
	// runs to stop. That is no daemon that does not answer.
	p, err := inside.Parse("probe=1\nsystemd=running\ndocker=asleep\nend=1\n")
	if err != nil {
		t.Fatal(err)
	}
	in := base()
	in.Distros[0].Probe = &p
	got := keys(find(t, Run(in), "check.g.distro", "Dev"))
	if !slices.Contains(got, "info:d.engineasleep") || slices.Contains(got, "warn:d.enginedown") {
		t.Fatalf("%q", got)
	}
	// One that does not answer is said as before.
	p, _ = inside.Parse("probe=1\nsystemd=running\ndocker=down\nend=1\n")
	in.Distros[0].Probe = &p
	if got := keys(find(t, Run(in), "check.g.distro", "Dev")); !slices.Contains(got, "warn:d.enginedown") {
		t.Fatalf("%q", got)
	}
}

func TestADockerOfAnotherDaemonIsSaidForTheLook(t *testing.T) {
	// A docker command here answers for a daemon this distribution does
	// not run, Docker Desktop's or another engine's: it is left alone,
	// which is nothing to do, and no daemon that does not answer.
	p, err := inside.Parse("probe=1\nsystemd=running\ndocker=foreign\nend=1\n")
	if err != nil {
		t.Fatal(err)
	}
	in := base()
	in.Distros[0].Probe = &p
	got := keys(find(t, Run(in), "check.g.distro", "Dev"))
	if !slices.Contains(got, "info:d.engineforeign") || slices.Contains(got, "warn:d.enginedown") {
		t.Fatalf("%q", got)
	}
}

func TestTheStopUnitsThatAStopPassesByAreSaid(t *testing.T) {
	// A stop stops only the listed units that run: one systemd does not
	// have (a typo) is said as worth a look, those that do not run now
	// for the look, each with its state. One whose state was not read is
	// not said either way.
	in := base()
	p := setUp()
	p.StopUnits = []string{"appworker.service", "appwroker.service", "postgresql.service", "redis.service", "slow.service"}
	p.StopUnitStates = map[string]inside.StopUnitState{
		"appworker.service":  {Load: "loaded", Active: "active"},
		"appwroker.service":  {Load: "not-found", Active: "inactive"},
		"postgresql.service": {Load: "loaded", Active: "activating"},
		"redis.service":      {Load: "loaded", Active: "inactive"},
	}
	in.Distros[0].Probe = p
	g := find(t, Run(in), "check.g.distro", "Dev")
	said := map[string]Item{}
	for _, it := range g.Items {
		said[it.Key] = it
	}
	if it, ok := said["check.d.unitsmissing"]; !ok || it.Mark != Warn || it.Args[0] != "appwroker.service" {
		t.Errorf("missing: %+v\n%q", it, keys(g))
	}
	if it, ok := said["check.d.unitsidle"]; !ok || it.Mark != Info || it.Args[0] != "postgresql.service (activating), redis.service (inactive)" {
		t.Errorf("idle: %+v\n%q", it, keys(g))
	}
}

func TestASystemdThatDoesNotAnswerIsNotNoSystemd(t *testing.T) {
	// systemd runs in it, and did not answer the probe in its time: said
	// for what it is, not as a distribution where systemd does not run,
	// which points to turning it on.
	in := base()
	in.Distros[0].Probe.Systemd = "noanswer"
	got := keys(find(t, Run(in), "check.g.distro", "Dev"))
	if !slices.Contains(got, "warn:d.systemdsilent") || slices.Contains(got, "warn:d.nosystemd") {
		t.Fatalf("%q", got)
	}
}

func TestAStartScriptIsJudgedByItsVersionNumber(t *testing.T) {
	// Older is a smaller number, or a line that names none; a larger one is
	// from a newer wsl-spawner, which this one leaves as it is, said for
	// the look.
	for version, want := range map[string]string{
		"1":                       "warn:d.oldsetup",
		"x":                       "warn:d.oldsetup",
		"99":                      "info:d.newsetup",
		inside.StartScriptVersion: "",
	} {
		in := base()
		in.Distros[0].Probe.StartScript = inside.StartScriptMark + version
		got := keys(find(t, Run(in), "check.g.distro", "Dev"))
		// Only the finding for this version, if any, about the script.
		var said []string
		for _, k := range got {
			if strings.HasSuffix(k, "setup") {
				said = append(said, k)
			}
		}
		if want == "" && len(said) != 0 || want != "" && !slices.Equal(said, []string{want}) {
			t.Errorf("version %s: %q", version, got)
		}
	}
}

func TestAClockIsJudgedAgainstWindowsClockAroundItsProbe(t *testing.T) {
	// The probe was asked at now and answered a minute later: a clock that
	// read anything in between agrees with Windows', and one outside is
	// off by how far it is from the nearer end.
	for off, want := range map[time.Duration]int{
		0: 0, 30 * time.Second: 0, time.Minute: 0,
		// The probe's clock drops the part of a second.
		-time.Second: 0,
		// Past either end by more than 5 s.
		-10 * time.Second: 9, time.Minute + 10*time.Second: 10, time.Hour: 3540,
	} {
		in := base()
		p := setUp()
		p.Now = now.Add(off)
		in.Distros[0].Probe, in.Distros[0].Answered = p, now.Add(time.Minute)
		var got int
		for _, it := range find(t, Run(in), "check.g.distro", "Dev").Items {
			if it.Key == "check.d.clock" {
				got = it.Args[0].(int)
			}
		}
		if got != want {
			t.Errorf("a clock %s off: %d s, want %d", off, got, want)
		}
	}
	// Not probed with its moments: nothing to set it beside.
	in := base()
	in.Distros[0].Probe.Now = now.Add(time.Hour)
	in.Distros[0].Asked, in.Distros[0].Answered = time.Time{}, time.Time{}
	if got := keys(find(t, Run(in), "check.g.distro", "Dev")); slices.Contains(got, "warn:d.clock") {
		t.Fatalf("%q", got)
	}
}

func TestDistributionsThatAreNotReadSayWhy(t *testing.T) {
	in := base()
	in.Config.Logon = nil
	in.Logon = Logon{}
	in.Distros = []distros.Distro{
		{Distro: wsl.Distro{Name: "docker-desktop", Version: 2, Running: true}, Owner: "Docker Desktop"},
		{Distro: wsl.Distro{Name: "Old", Version: 1}},
		{Distro: wsl.Distro{Name: "Off", Version: 2}},
		{Distro: wsl.Distro{Name: "Mine", Version: 2}, Skipped: true},
		{Distro: wsl.Distro{Name: "Odd", Version: 2, Running: true}, ProbeErr: errors.New("boom\nmore")},
		{Distro: wsl.Distro{Name: "Bare", Version: 2, Running: true}, Probe: &inside.Probe{Engines: map[string]bool{"docker": false}, Complete: true}},
	}
	gs := Run(in)
	for name, want := range map[string][]string{
		"docker-desktop": {"info:d.owned"}, "Old": {"info:d.wsl1"}, "Off": {"info:d.stopped"}, "Mine": {"info:d.skipped"},
		"Odd": {"warn:d.unread"}, "Bare": {"warn:d.nosystemd", "info:d.notsetup", "warn:d.enginedown"},
	} {
		if got := keys(find(t, gs, "check.g.distro", name)); !slices.Equal(got, want) {
			t.Errorf("%s: %q", name, got)
		}
	}
	// The probe's error is said by its first line.
	if g := find(t, gs, "check.g.distro", "Odd"); g.Items[0].Args[0] != "boom" {
		t.Fatalf("%v", g.Items[0].Args)
	}
}

func TestSignIn(t *testing.T) {
	for name, c := range map[string]struct {
		logon []string
		entry Logon
		want  []string
	}{
		"nothing":          {nil, Logon{}, []string{"info:s.none"}},
		"left over":        {nil, Logon{Present: true}, []string{"warn:s.leftover"}},
		"no program":       {[]string{"Dev"}, Logon{}, []string{"bad:s.noentry", "info:s.starts"}},
		"turned off":       {[]string{"Dev"}, Logon{Present: true, Off: true}, []string{"warn:s.off", "info:s.starts"}},
		"another copy":     {[]string{"Dev"}, Logon{Present: true, Command: "a", Want: "b"}, []string{"warn:s.stale", "info:s.starts"}},
		"one that is gone": {[]string{"Dev", "Gone"}, Logon{Present: true}, []string{"ok:s.entry", "info:s.starts", "warn:s.missing"}},
	} {
		in := base()
		in.Config.Logon, in.Logon = c.logon, c.entry
		if got := keys(find(t, Run(in), "check.g.signin", "")); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q", name, got)
		}
	}
}

func TestPortsWindowsKeeps(t *testing.T) {
	in := base()
	in.Distros[0].Probe.Containers = []inside.Container{{Name: "web", State: "running", Policy: "always", Ports: []string{"8080/tcp", "5353/udp", "50010/tcp"}}}
	in.Excluded = ParseExcluded("\r\nProtocol tcp Port Exclusion Ranges\r\n\r\nStart Port    End Port\r\n----------    --------\r\n      8050        8149\r\n     50000       50059     *\r\n\r\n* - Administered port exclusions.\r\n", "tcp")
	g := find(t, Run(in), "check.g.network", "")
	if got := keys(g); !slices.Equal(got, []string{"bad:n.excluded", "bad:n.excluded"}) {
		t.Fatalf("%q", got)
	}
	if g.Items[0].Args[2] != "8080/tcp" || g.Items[0].Args[3] != "8050-8149" {
		t.Fatalf("%v", g.Items[0].Args)
	}
	// UDP's ranges are UDP's: a TCP range does not hold a UDP port.
	in.Excluded = []PortRange{{"tcp", 5300, 5400}}
	if got := keys(find(t, Run(in), "check.g.network", "")); !slices.Equal(got, []string{"ok:n.ok"}) {
		t.Fatalf("%q", got)
	}
}

func TestWSLConfigKeys(t *testing.T) {
	text := "[wsl2]\r\nmemory=8GB\r\nnetworkingMode = mirrored\r\n# vmIdleTimeout=1\r\n[general]\r\ninstanceIdleTimeout=-1\r\n"
	if got := WSLConfigKeys(text); !slices.Equal(got, []string{"networkingMode=mirrored", "instanceIdleTimeout=-1"}) {
		t.Fatalf("%q", got)
	}
}

func TestStopsWSL(t *testing.T) {
	for text, want := range map[string]bool{
		"@echo off\r\nwsl.exe --shutdown\r\n": true,
		"wsl -t Ubuntu --terminate":           true,
		"powershell -File save.ps1\r\n":       false,
		"rem shutdown the app first\n":        false,
	} {
		if got := StopsWSL(text); got != want {
			t.Errorf("%q: %v", text, got)
		}
	}
}

func TestTheHistorysLastStopIsSaidWhereNothingInsideNotesIt(t *testing.T) {
	// Each distribution has a stop of wsl-spawner's in the history. It is
	// said right after the finding that nothing inside notes how its last
	// start ended, once; where something inside does, or the distribution
	// is not wsl-spawner's, not at all.
	in := base()
	in.Config.Logon = nil
	in.Logon = Logon{}
	in.Distros = []distros.Distro{
		{Distro: wsl.Distro{Name: "Off", Version: 2}},
		{Distro: wsl.Distro{Name: "Bare", Version: 2, Running: true}, Probe: &inside.Probe{Systemd: "running", Engines: map[string]bool{}, Complete: true}},
		{Distro: wsl.Distro{Name: "Dev", Version: 2, Running: true}, Probe: setUp(), Asked: now, Answered: now},
		{Distro: wsl.Distro{Name: "Mine", Version: 2}, Skipped: true},
	}
	at := now.Add(-2 * time.Hour)
	in.LastStops = map[string]LastStop{"Off": {"error", at}, "Bare": {"limit", at}, "Dev": {"clean", at}, "Mine": {"clean", at}}
	gs := Run(in)
	for name, want := range map[string][]string{
		"Off":  {"info:d.stopped", "bad:d.history.error"},
		"Bare": {"ok:d.systemd", "info:d.notsetup", "warn:d.history.limit"},
		"Mine": {"info:d.skipped"},
	} {
		if got := keys(find(t, gs, "check.g.distro", name)); !slices.Equal(got, want) {
			t.Errorf("%s: %q", name, got)
		}
	}
	// Dev notes its own end inside: the history's line is not said.
	if got := keys(find(t, gs, "check.g.distro", "Dev")); slices.ContainsFunc(got, func(k string) bool { return strings.Contains(k, "d.history") }) {
		t.Fatalf("Dev: %q", got)
	}
	// When it is said, the stop's time goes with it.
	if it := find(t, gs, "check.g.distro", "Off").Items[1]; it.Args[0] != stamp(at) {
		t.Fatalf("%+v", it)
	}
}

func TestAProbeReadInPartIsSaidFirst(t *testing.T) {
	// Cut off before its end, the probe says less than it seems: the check
	// says so before what it read, and why.
	in := base()
	p := setUp()
	p.Complete = false
	in.Distros[0].Probe, in.Distros[0].ProbeErr = p, errors.New("read in part, as its look ran out of time: WSL did not answer in time")
	g := find(t, Run(in), "check.g.distro", "Dev")
	if got := keys(g); len(got) == 0 || got[0] != "warn:d.partial" || !strings.Contains(g.Items[0].Args[0].(string), "ran out of time") {
		t.Fatalf("%q %+v", got, g.Items)
	}
}
