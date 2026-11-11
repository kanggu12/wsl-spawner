package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
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

// pc is a made-up PC: a distribution set up with Docker, one stopped, one
// of Docker Desktop's and one left to WSL; every stop script prints lines.
func pc(t *testing.T, stopLines ...string) (*app.App, *wsl.Fake) {
	t.Helper()
	f := &wsl.Fake{List: []wsl.Distro{
		{Name: "Ubuntu-24.04", Version: 2, Running: true, Default: true},
		{Name: "Debian", Version: 2},
		{Name: "docker-desktop", Version: 2, Running: true},
		{Name: "Work", Version: 2, Running: true},
	}}
	if len(stopLines) == 0 {
		stopLines = []string{"plan=docker\t2", "step=docker\tstart\t2", "step=docker\tdone\t0\t21", "step=sync\tstart", "step=sync\tdone\t0\t0", "end=clean"}
	}
	f.Script = func(distro, user, script string) (string, error) {
		if script == inside.ProbeScript() {
			return "probe=1\nstart_script=" + inside.StartScriptMark + "1\ndocker=up\n" +
				"container=docker\taaa\t/web\trunning\tunless-stopped\t60\tnginx\tsite\n" +
				"container=docker\tbbb\t/job\texited\tno\t\tbusybox\t\nend=1\n", nil
		}
		if strings.HasPrefix(script, "LIMIT=") {
			return strings.Join(stopLines, "\n") + "\n", nil
		}
		return "", errors.New("unexpected script")
	}
	c := config.Default()
	c.Skip = []string{"Work"}
	// The settings file in the test's own folder: a change a test makes is
	// written there, never beside the tests.
	return &app.App{Config: c, ConfigPath: filepath.Join(t.TempDir(), "config.toml"), WSL: f, Dir: t.TempDir(), PC: testPC()}, f
}

// errNotHere is what a made-up PC answers for what a test did not give it.
var errNotHere = errors.New("this test's PC has none of this")

// testPC is a made-up PC that has nothing a test did not give it: a call
// a test did not expect fails, and never reaches this PC.
func testPC() app.PC {
	return app.PC{
		ReadScripts:           func() (string, error) { return "", errNotHere },
		ReadPowerShellScripts: func() (string, error) { return "", errNotHere },
		InstallHook:           func(string, string, ...string) error { return errNotHere },
		UninstallHook:         func(string, ...string) error { return errNotHere },
		RefreshPolicy:         func() error { return errNotHere },
		Elevated:              func() bool { return false },
		RunElevated:           func(string, []string) (int, error) { return 0, errNotHere },
		MaxWait:               func() time.Duration { return 600 * time.Second },
		LogonEntry:            func(string) (string, bool, bool, error) { return "", false, false, errNotHere },
		SetLogonEntry:         func(string, string) error { return errNotHere },
		DeleteLogonEntry:      func(string) error { return errNotHere },
		QuietExe:              func() (string, error) { return "", errNotHere },
		FixedDisk:             func(string) bool { return false },
		ReadFile:              func(string) ([]byte, error) { return nil, errNotHere },
		HomeDir:               func() (string, error) { return "", errNotHere },
		ExcludedPorts:         func(string) (string, error) { return "", errNotHere },
		StartDetached:         func([]string) error { return errNotHere },
		TerminalDistro:        func() string { return "" },
	}
}

// cli runs one command line on the made-up PC, with no terminal unless
// terminal says, and returns its output, its errors and its exit code.
func cli(t *testing.T, a *app.App, terminal bool, args ...string) (string, string, int) {
	t.Helper()
	in, outTerm := stdinIsTerminal, stdoutIsTerminal
	t.Cleanup(func() { stdinIsTerminal, stdoutIsTerminal = in, outTerm })
	stdinIsTerminal = func() bool { return terminal }
	stdoutIsTerminal = func() bool { return terminal }
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("WSL_SPAWNER_TTY", "")
	var out, errOut bytes.Buffer
	code := runWith(args, &out, &errOut, func() (*app.App, error) { return a, nil })
	return out.String(), errOut.String(), code
}

func TestStopNeedsNamesOrAll(t *testing.T) {
	a, f := pc(t)
	_, errOut, code := cli(t, a, false, "stop")
	// The running ones it could stop are named in the hint.
	if code != exitUsage || !strings.Contains(errOut, "name the distributions to stop") || !strings.Contains(errOut, "running: Ubuntu-24.04") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if slices.ContainsFunc(f.CallList(), func(c string) bool { return strings.HasPrefix(c, "terminate") }) {
		t.Fatal("something stopped")
	}
}

func TestStopRefusesWhatIsNotItsToStop(t *testing.T) {
	a, f := pc(t)
	for name, words := range map[string]string{
		"docker-desktop": "Docker Desktop's, which stops it itself",
		"work":           "left to WSL in the settings",
		"nosuch":         "no such distribution",
	} {
		_, errOut, code := cli(t, a, false, "stop", "--yes", name)
		if code != exitUsage || !strings.Contains(errOut, words) {
			t.Errorf("%s: exit %d: %s", name, code, errOut)
		}
	}
	if slices.ContainsFunc(f.CallList(), func(c string) bool { return strings.HasPrefix(c, "terminate") }) {
		t.Fatalf("stopped: %q", f.CallList())
	}
}

func TestStopOfOneThatDoesNotRunIsNothing(t *testing.T) {
	a, _ := pc(t)
	out, _, code := cli(t, a, false, "stop", "--yes", "Debian")
	if code != exitOK || !strings.Contains(out, "Debian is not running") || !strings.Contains(out, "nothing to stop") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestStopAsksAndAScriptGetsNoQuestion(t *testing.T) {
	a, f := pc(t)
	// No terminal, no --yes: no one to ask, so nothing happens.
	_, errOut, code := cli(t, a, false, "stop", "Ubuntu-24.04")
	if code != exitUsage || !strings.Contains(errOut, "add --yes") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// At a terminal, a no stops nothing.
	saved := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = saved })
	_, errOut, code = cliWithInput(t, a, "n\n", "stop", "Ubuntu-24.04")
	if code != exitFail || !strings.Contains(errOut, "nothing stopped") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if slices.Contains(f.CallList(), "terminate:Ubuntu-24.04") {
		t.Fatal("stopped after a no")
	}
}

// cliWithInput is cli at a terminal, with input typed.
func cliWithInput(t *testing.T, a *app.App, input string, args ...string) (string, string, int) {
	t.Helper()
	in, outTerm := stdinIsTerminal, stdoutIsTerminal
	t.Cleanup(func() { stdinIsTerminal, stdoutIsTerminal = in, outTerm })
	stdinIsTerminal = func() bool { return true }
	stdoutIsTerminal = func() bool { return false }
	t.Setenv("WSL_DISTRO_NAME", "")
	var out, errOut bytes.Buffer
	root := newRoot(&apps{make: func() (*app.App, error) { return a, nil }})
	root.SetContext(context.Background())
	root.SetArgs(args)
	root.SetIn(strings.NewReader(input))
	root.SetOut(&out)
	root.SetErr(&errOut)
	err := root.Execute()
	if err != nil {
		printErr(&errOut, withHint(usageHint(root, args, err)))
	}
	return out.String(), errOut.String(), exitFor(err)
}

func TestStopSaysThePlanThenEachStepAndKeepsTheHistory(t *testing.T) {
	a, f := pc(t)
	out, errOut, code := cliWithInput(t, a, "y\n", "stop", "ubuntu-24.04")
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	for _, want := range []string{
		"Each service and container gets the time it asks for, 300 s at most together:",
		"Ubuntu-24.04  docker: 1 container, the longest stop 60 s",
		"Ubuntu-24.04  docker: 2 containers …",
		"Ubuntu-24.04  docker: 2 containers  done, 21 s",
		"Ubuntu-24.04  WSL ends the rest …",
		"Ubuntu-24.04 stopped, every step in its time",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	// The question named the distribution as WSL has it.
	if !strings.Contains(errOut, "Stop Ubuntu-24.04? [y/N]") {
		t.Errorf("question: %q", errOut)
	}
	if !slices.Contains(f.CallList(), "terminate:Ubuntu-24.04") || slices.Contains(f.CallList(), "shutdown:") {
		t.Fatalf("calls %q", f.CallList())
	}
	all, err := history.Open(a.Dir).All()
	if err != nil || len(all) != 1 || all[0].By != "hand" || all[0].Distros[0].Result != "clean" || all[0].Distros[0].Steps[0].Count != 2 {
		t.Fatalf("history %+v %v", all, err)
	}
}

func TestAStopThatRanOutOfTimeExitsThree(t *testing.T) {
	a, _ := pc(t, "step=docker\tstart\t1", "step=docker\tdone\t124\t300", "end=limit")
	out, errOut, code := cli(t, a, false, "stop", "--yes", "--all")
	if code != exitAttention || !strings.Contains(out, "out of time") || !strings.Contains(errOut, "did not get every step in its time") {
		t.Fatalf("exit %d:\n%s\n%s", code, out, errOut)
	}
}

func TestStopJSON(t *testing.T) {
	a, _ := pc(t)
	out, _, code := cli(t, a, false, "stop", "--yes", "--json", "--all", "--shutdown")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, out)
	}
	var got struct {
		Schema   int  `json:"schema"`
		Shutdown bool `json:"shutdown"`
		Results  []struct {
			Name   string `json:"name"`
			Result string `json:"result"`
			Clean  bool   `json:"clean"`
			Steps  []struct {
				Step  string `json:"step"`
				Count int    `json:"count"`
			} `json:"steps"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got.Schema != 1 || !got.Shutdown || len(got.Results) != 1 || got.Results[0].Name != "Ubuntu-24.04" || !got.Results[0].Clean || got.Results[0].Steps[0].Count != 2 {
		t.Fatalf("%+v", got)
	}
	// Nothing but the JSON: no plan, no steps.
	if !strings.HasPrefix(out, "{") {
		t.Fatalf("%s", out)
	}
}

func TestAStopOfTheTerminalsOwnDistributionGoesOnWithoutIt(t *testing.T) {
	a, f := pc(t)
	var started []string
	a.PC.StartDetached = func(args []string) error { started = args; return nil }
	in, outTerm := stdinIsTerminal, stdoutIsTerminal
	t.Cleanup(func() { stdinIsTerminal, stdoutIsTerminal = in, outTerm })
	stdinIsTerminal, stdoutIsTerminal = func() bool { return false }, func() bool { return false }
	a.PC.TerminalDistro = func() string { return "Ubuntu-24.04" }
	var out, errOut bytes.Buffer
	code := runWith([]string{"stop", "--yes", "Ubuntu-24.04"}, &out, &errOut, func() (*app.App, error) { return a, nil })
	if code != exitOK || !strings.Contains(out.String(), "This terminal runs in Ubuntu-24.04") {
		t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
	}
	if !slices.Equal(started, []string{"stop", "--yes", "--background", "--by", "hand", "Ubuntu-24.04"}) {
		t.Fatalf("started %q", started)
	}
	// This run stopped nothing itself: the one started does.
	if slices.Contains(f.CallList(), "terminate:Ubuntu-24.04") {
		t.Fatal("stopped here too")
	}
}

func TestAShutdownWithNothingToStopFirstGoesOnAsAsked(t *testing.T) {
	// Nothing of wsl-spawner's runs, or the one named does not: the copy
	// that goes on without the terminal is asked for what was, and its
	// arguments run, stopping WSL alone.
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"stop", "--yes", "--all", "--shutdown"}, []string{"stop", "--yes", "--background", "--by", "hand", "--shutdown", "--all"}},
		{[]string{"stop", "--yes", "--shutdown", "Debian"}, []string{"stop", "--yes", "--background", "--by", "hand", "--shutdown", "Debian"}},
	} {
		a, f := pc(t)
		// Only Work runs, which is left to WSL.
		for i := range f.List {
			f.List[i].Running = f.List[i].Name == "Work"
		}
		var started []string
		a.PC.StartDetached = func(args []string) error { started = args; return nil }
		a.PC.TerminalDistro = func() string { return "Work" }
		if _, errOut, code := cli(t, a, false, tc.args...); code != exitOK || !slices.Equal(started, tc.want) {
			t.Fatalf("%q: exit %d: %s started %q", tc.args, code, errOut, started)
		}
		// The copy's own run: WSL stops, and the history says so.
		a.PC.TerminalDistro = func() string { return "" }
		if _, errOut, code := cli(t, a, false, started...); code != exitOK || !slices.Contains(f.CallList(), "shutdown:") {
			t.Fatalf("%q: the copy: exit %d: %s calls %q", started, code, errOut, f.CallList())
		}
		if all, _ := history.Open(a.Dir).All(); len(all) != 1 || !all[0].Shutdown {
			t.Fatalf("%q: %+v", started, all)
		}
	}
}

func TestStatusTableAndJSON(t *testing.T) {
	a, _ := pc(t)
	out, _, code := cli(t, a, false, "status")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"Ubuntu-24.04", "set up", "1/2", "docker-desktop", "left to Docker Desktop", "Work", "left to WSL", "Debian", "stopped"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	// Debian does not run: what is set up in it is not guessed.
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Debian") && (!strings.Contains(l, "–") || strings.Contains(l, "stop only")) {
			t.Errorf("Debian's row: %q", l)
		}
	}
	out, _, code = cli(t, a, false, "status", "--json")
	var got api.Status
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != exitOK {
		t.Fatalf("%v %d\n%s", err, code, out)
	}
	byName := map[string]api.Distro{}
	for _, d := range got.List {
		byName[d.Name] = d
	}
	if u := byName["Ubuntu-24.04"]; u.Kind != "set-up" || u.Probe == nil || len(u.Probe.Containers) != 2 || u.Probe.Containers[0].StopSeconds != 60 || u.Probe.Containers[1].StopSeconds != 10 {
		t.Fatalf("%+v", u)
	}
	if byName["Debian"].Probe != nil || byName["Debian"].Kind != "not-read" || byName["Work"].Kind != "skipped" || byName["docker-desktop"].Owner != "Docker Desktop" {
		t.Fatalf("%+v", byName)
	}
}

func TestStopEventsComeOneLineEach(t *testing.T) {
	a, _ := pc(t)
	out, _, code := cli(t, a, false, "stop", "--yes", "--json", "--events", "Ubuntu-24.04")
	if code != exitOK {
		t.Fatalf("exit %d\n%s", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var kinds []string
	for _, l := range lines[:len(lines)-1] {
		var e api.StopEvent
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("%v: %q", err, l)
		}
		kinds = append(kinds, e.Event+":"+e.Kind+":"+e.Step)
	}
	// The steps as they came, WSL ending the rest, then the result line.
	want := []string{"line:plan:docker", "line:start:docker", "line:done:docker", "line:start:sync", "line:done:sync", "line:end:",
		"terminate::", "terminate::", "finished::"}
	if !slices.Equal(kinds, want) {
		t.Fatalf("%q", kinds)
	}
	// A step done says its exit code and its time, 0 as well; the other
	// lines leave out what they have nothing to say for.
	if !strings.Contains(lines[4], `"code":0,"took_seconds":0`) || strings.Contains(lines[3], `"code"`) {
		t.Fatalf("%s\n%s", lines[3], lines[4])
	}
	var last api.StopResult
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil || len(last.Results) != 1 || !last.Results[0].Clean {
		t.Fatalf("%v %+v", err, last)
	}
	// Without --json, --events is a mistake.
	if _, errOut, code := cli(t, a, false, "stop", "--yes", "--events", "Ubuntu-24.04"); code != exitUsage || !strings.Contains(errOut, "--events goes with --json") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestStatusSaysAProbeReadInPart(t *testing.T) {
	// The probe of Ubuntu-24.04 runs out of time after systemd: the table
	// and the JSON keep what it read, and say it was read in part.
	a, f := pc(t)
	f.Script = func(distro, user, script string) (string, error) {
		if script == inside.ProbeScript() {
			return "probe=1\nsystemd=running\n", fmt.Errorf("%w: wsl ran for 1m30s", wsl.ErrNoAnswer)
		}
		return "", errors.New("unexpected script")
	}
	out, _, code := cli(t, a, false, "status")
	if code != exitOK || !strings.Contains(out, "! Ubuntu-24.04: read in part, as its look ran out of time") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	out, _, _ = cli(t, a, false, "status", "--json")
	var got api.Status
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	for _, d := range got.List {
		if d.Name == "Ubuntu-24.04" && (d.Probe == nil || d.Probe.Complete || d.Probe.Systemd != "running" || !strings.Contains(d.ProbeError, "read in part")) {
			t.Fatalf("%+v %+v", d, d.Probe)
		}
	}
}
