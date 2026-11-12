package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
)

// longNames are made-up container names as long as real ones run, 15 to 30
// cells: compose names, a service and a number.
var longNames = []string{"billing-api-worker-1", "billing-api-worker-2", "reports-nightly-export", "search-indexer-cache-warmup", "postgres-primary"}

// withLongNames is the main screen at w by h, on a made-up PC with one
// distribution, set up or not, running containers of those names.
func withLongNames(w, h int, setUp bool) string {
	p := &api.Probe{SetUp: setUp, KeptRunning: setUp, Systemd: "running", Engines: map[string]bool{"docker": true}, LastEnd: api.End{How: "unknown"}}
	for i, n := range longNames {
		// A policy on every other one: words of both kinds in the column.
		policy := []string{"no", "unless-stopped"}[i%2]
		p.Containers = append(p.Containers, api.Container{Engine: "docker", Name: n, State: "running", Policy: policy, StopSeconds: 10, Image: "registry.example.com/team/" + n + ":1.4.2"})
	}
	m := newModel(context.Background(), api.Client{}, true)
	m.width, m.height, m.loaded = w, h, true
	m.st = api.Status{WSL: "3.0.5.0", StopLimit: 300, Resume: "running", List: []api.Distro{{Name: "Ubuntu-24.04", Running: true, Kind: "set-up", Version: 2, Probe: p}}}
	return ansi.Strip(m.View().Content)
}

func TestLongContainerNamesKeepTheirStateAndWordsWhole(t *testing.T) {
	for _, setUp := range []bool{false, true} {
		// The words on coming back each row ends with.
		words := map[bool][]string{false: {"won't be back", "back by its policy"}, true: {"back by wsl-spawner", "back by its policy"}}[setUp]
		for _, w := range []int{100, 120, 140, 200} {
			f := withLongNames(w, 34, setUp)
			for i, n := range longNames {
				// The row is the one with the name's end: a name cut short
				// keeps its start and its end, which tells -1 from -2.
				row := lineWith(f, n[len(n)-8:]+"  ")
				if row == "" {
					t.Errorf("set up %v, %d: no row ends %q in\n%s", setUp, w, n[len(n)-8:], f)
					continue
				}
				if !strings.Contains(row, "running  "+words[i%2]) {
					t.Errorf("set up %v, %d: the state or its words cut in %q", setUp, w, row)
				}
				// From 120 columns, the names stand whole.
				if w >= 120 && !strings.Contains(row, n) {
					t.Errorf("set up %v, %d: %s cut in %q", setUp, w, n, row)
				}
			}
		}
	}
}

func TestNarrowSectionsStillHaveTheContainersBeside(t *testing.T) {
	// In Korean, with no last stop noted, the sections left of the
	// containers are narrower than minLeft by nature: the containers stand
	// beside them all the same, where there is room.
	i18n.Set(i18n.Korean)
	defer i18n.Set(i18n.English)
	p := &api.Probe{Systemd: "running", Engines: map[string]bool{"docker": true}, LastEnd: api.End{How: "unknown"},
		Containers: []api.Container{{Engine: "docker", Name: "web", State: "running", Policy: "no", StopSeconds: 10, Image: "nginx:1.27"}}}
	m := newModel(context.Background(), api.Client{}, true)
	m.width, m.height, m.loaded = 100, 30, true
	m.st = api.Status{StopLimit: 300, Resume: "running", List: []api.Distro{{Name: "Arch", Running: true, Kind: "stop-only", Version: 2, Probe: p}}}
	f := ansi.Strip(m.View().Content)
	// The choices' heading, with the containers' beside it: the table's
	// own heading says 컨테이너 too.
	if row := lineWith(f, "선택"); !strings.Contains(row, "컨테이너") {
		t.Errorf("the containers are not beside the choices:\n%s", f)
	}
}

// lineWith is the first line of f that holds s.
func lineWith(f, s string) string {
	for _, l := range strings.Split(f, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

func TestARunThatWentWrongIsNotCalledDone(t *testing.T) {
	// A run that ended with an error, or with a distribution that did not
	// stop or start, says what it was: the lines under it say what went
	// wrong, and "Stopped" over them would say the opposite.
	for _, c := range []struct {
		r    runState
		want string
	}{
		// Under way.
		{runState{act: action{kind: "stop", names: []string{"Ubuntu"}}}, "Stopping Ubuntu"},
		// Ended well.
		{runState{act: action{kind: "stop", names: []string{"Ubuntu"}}, done: true, results: map[string]string{"Ubuntu": "clean"}}, "Stopped Ubuntu"},
		// A step out of time still stopped it.
		{runState{act: action{kind: "stop", names: []string{"Ubuntu"}}, done: true, results: map[string]string{"Ubuntu": "limit"}}, "Stopped Ubuntu"},
		// One that did not stop as it should.
		{runState{act: action{kind: "stop", names: []string{"Ubuntu", "Work"}}, done: true, results: map[string]string{"Ubuntu": "clean", "Work": "error"}}, "Stop of Ubuntu, Work"},
		// The command itself refused.
		{runState{act: action{kind: "stop", shutdown: true}, done: true, err: errors.New("refused")}, "Stop of WSL"},
		// A start that did not start it.
		{runState{act: action{kind: "start", names: []string{"Debian"}}, done: true, results: map[string]string{"Debian": "error"}}, "Start of Debian"},
		{runState{act: action{kind: "start", names: []string{"Debian"}}, done: true, results: map[string]string{"Debian": "started"}}, "Started Debian"},
	} {
		if got := runTitle(&c.r); got != c.want {
			t.Errorf("%+v: %q, want %q", c.r, got, c.want)
		}
	}
}

func TestAContainerRemovedAsItStopsIsNeverSaidToComeBack(t *testing.T) {
	// Set up, every running container brought back: but one run with --rm
	// is removed as it stops, and nothing can start it again.
	p := &api.Probe{SetUp: true, KeptRunning: true, Systemd: "running", Engines: map[string]bool{"docker": true}, LastEnd: api.End{How: "unknown"},
		Containers: []api.Container{
			{Engine: "docker", Name: "web", State: "running", Policy: "no", StopSeconds: 10, Image: "nginx:1.27"},
			{Engine: "docker", Name: "once", State: "running", Policy: "no", StopSeconds: 10, Image: "busybox:1.36", AutoRemove: true},
		}}
	m := newModel(context.Background(), api.Client{}, true)
	m.width, m.height, m.loaded = 120, 34, true
	m.st = api.Status{WSL: "3.0.5.0", StopLimit: 300, Resume: "running", List: []api.Distro{{Name: "Ubuntu-24.04", Running: true, Kind: "set-up", Version: 2, Probe: p}}}
	f := ansi.Strip(m.View().Content)
	if !strings.Contains(lineWith(f, "▪ once "), "won't be back") || !strings.Contains(lineWith(f, "▪ web "), "back by wsl-spawner") {
		t.Fatalf("%s", f)
	}
	// The verdict counts only the one that comes back.
	if !strings.Contains(f, "1 container comes back") {
		t.Fatalf("the count:\n%s", f)
	}
}

func TestTheDetailsSayAProbeReadInPart(t *testing.T) {
	// What was read stands, under a line, in the color of what is worth a
	// look, that says the look ran out of time.
	p := &api.Probe{Systemd: "running", Engines: map[string]bool{"docker": true}, LastEnd: api.End{How: "unknown"}}
	m := newModel(context.Background(), api.Client{}, true)
	m.width, m.height, m.loaded = 120, 30, true
	m.st = api.Status{WSL: "3.0.5.0", StopLimit: 300, Resume: "running", List: []api.Distro{{Name: "Ubuntu-24.04", Running: true, Kind: "stop-only", Version: 2, Probe: p,
		ProbeError: "read in part, as its look ran out of time: WSL did not answer in time"}}}
	if f := ansi.Strip(m.View().Content); !strings.Contains(f, "read in part, as its look ran out of time") {
		t.Fatalf("%s", f)
	}
	// Read to its end, no such line.
	p.Complete = true
	m.st.List[0].ProbeError = ""
	if f := ansi.Strip(m.View().Content); strings.Contains(f, "read in part") {
		t.Fatalf("%s", f)
	}
}

// mainWith is the main screen at w by h on a made-up PC with ds, the row
// sel selected, as a model to press keys on.
func mainWith(w, h, sel int, ds ...api.Distro) model {
	m := newModel(context.Background(), api.Client{}, true)
	m.width, m.height, m.loaded, m.sel = w, h, true, sel
	m.st = api.Status{WSL: "3.0.5.0", StopLimit: 300, Resume: "running", List: ds}
	return m
}

func TestManyDistributionsKeepTheSelectedRowAndItsDetailsInSight(t *testing.T) {
	// 24 distributions at 80x24, the last selected: its row is shown and
	// lit, its details are under the table, and a line says how many rows
	// are out of sight.
	var ds []api.Distro
	for i := range 24 {
		ds = append(ds, api.Distro{Name: fmt.Sprintf("Work-%02d", i), Running: true, Kind: "stop-only", Version: 2,
			Probe: &api.Probe{Systemd: "running", LastEnd: api.End{How: "unknown"}}})
	}
	for _, size := range [][2]int{{80, 24}, {60, 18}, {120, 30}} {
		for _, sel := range []int{0, 12, 23} {
			f := ansi.Strip(mainWith(size[0], size[1], sel, ds...).View().Content)
			name := fmt.Sprintf("Work-%02d", sel)
			if strings.Count(f, name) < 2 {
				t.Errorf("%dx%d, %s selected: its row or its details are out of sight:\n%s", size[0], size[1], name, f)
			}
			if !strings.Contains(f, "more below") && !strings.Contains(f, "more above") {
				t.Errorf("%dx%d, %s selected: nothing says rows are out of sight:\n%s", size[0], size[1], name, f)
			}
		}
	}
}

func TestADistributionWithoutSystemdIsToldSoAndOfferedNoSetUp(t *testing.T) {
	m := mainWith(100, 30, 0, api.Distro{Name: "Work", Running: true, Kind: "stop-only", Version: 2,
		Probe: &api.Probe{Systemd: "", LastEnd: api.End{How: "unknown"}}})
	f := ansi.Strip(m.View().Content)
	if !strings.Contains(f, "systemd does not run in it") || strings.Contains(f, "Each service gets its time to stop") {
		t.Errorf("the verdict:\n%s", f)
	}
	keys := lineWith(f, "select")
	for _, k := range []string{"set up", "keep", "sign-in"} {
		if strings.Contains(keys, k) {
			t.Errorf("%q offered for a distribution without systemd: %q", k, keys)
		}
	}
}

func TestTheKeysOfferStopForOneRunningAndStartForOneStopped(t *testing.T) {
	run := api.Distro{Name: "On", Running: true, Kind: "set-up", Version: 2, Probe: &api.Probe{Systemd: "running", SetUp: true, LastEnd: api.End{How: "unknown"}}}
	off := api.Distro{Name: "Off", Kind: "set-up", Version: 2}
	for sel, want := range map[int][2]string{0: {"stop", "start"}, 1: {"start", "stop"}} {
		keys := lineWith(ansi.Strip(mainWith(120, 30, sel, run, off).View().Content), "select")
		if !strings.Contains(keys, " "+want[0]) || strings.Contains(keys, " "+want[1]+" ") {
			t.Errorf("row %d: %q", sel, keys)
		}
	}
}

func TestTheErrorScreenSaysTheWholeErrorAndHowToReadAgain(t *testing.T) {
	err := errors.New(`asking WSL which distributions run: exec: "wsl.exe": executable file not found in %PATH%, and nothing else could be asked either`)
	for _, w := range []int{60, 80, 120} {
		m := mainWith(w, 24, 0)
		m.err = err
		f := ansi.Strip(m.View().Content)
		// The words, wherever the lines break.
		if !strings.Contains(strings.Join(strings.Fields(f), " "), strings.Join(strings.Fields(err.Error()), " ")) {
			t.Errorf("%d wide: the error is cut:\n%s", w, f)
		}
		keys := lineWith(f, "quit")
		if !strings.Contains(keys, "read again") || strings.Contains(keys, "select") {
			t.Errorf("%d wide: the keys %q", w, keys)
		}
	}
}

func TestEscOnTheMainScreenDoesNotQuit(t *testing.T) {
	m := mainWith(100, 30, 0, api.Distro{Name: "Work", Running: true, Kind: "set-up", Version: 2, Probe: &api.Probe{Systemd: "running", LastEnd: api.End{How: "unknown"}}})
	if _, cmd := m.Update(snapshotKey("esc")); cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("esc quit the program from the main screen")
		}
	}
	if _, cmd := m.Update(snapshotKey("q")); cmd == nil {
		t.Fatal("q did not quit")
	} else if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("q did not quit")
	}
}

func TestTheLogoStandsInTheMiddleOfAnEmptyScreen(t *testing.T) {
	// The block is its own width: the mark starts well in from the left,
	// and the line under the name is whole, wrapped where it must be.
	for _, w := range []int{80, 120} {
		f := ansi.Strip(mainWith(w, 30, 0).View().Content)
		mark := lineWith(f, "#")
		if mark == "" {
			mark = lineWith(f, "▄")
		}
		if lead := len(mark) - len(strings.TrimLeft(mark, " ")); lead < 8 {
			t.Errorf("%d wide: the logo starts at column %d:\n%s", w, lead, f)
		}
		// The words in order, the mark's blocks beside them left out.
		words := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
			if strings.ContainsRune("▀▄█#", r) {
				return ' '
			}
			return r
		}, f)), " ")
		if !strings.Contains(words, "WSL's services and containers get their time to stop, and come back") {
			t.Errorf("%d wide: the tagline is cut:\n%s", w, f)
		}
	}
}

func TestAnEngineAsleepOrAnothersIsSaidQuietlyNotAsDown(t *testing.T) {
	// A Docker stopped by hand behind its socket, or a docker whose daemon
	// is another's, is nothing wrong: said by its own word, where "down"
	// is for a daemon that does not answer.
	for state, want := range map[string]string{"asleep": "docker asleep", "foreign": "docker not its own", "down": "docker down"} {
		engines := map[string]bool{}
		if state != "foreign" {
			engines["docker"] = false
		}
		m := mainWith(120, 30, 0, api.Distro{Name: "Work", Running: true, Kind: "set-up", Version: 2,
			Probe: &api.Probe{Systemd: "running", SetUp: true, Engines: engines, EngineStates: map[string]string{"docker": state}, LastEnd: api.End{How: "unknown"}}})
		row := lineWith(ansi.Strip(m.View().Content), "Work ")
		if !strings.Contains(row, want) {
			t.Errorf("%s: %q", state, row)
		}
	}
}

func TestAStopAnotherStopIsStoppingIsSaidQuietly(t *testing.T) {
	// Its result carries an error for scripts; on the screen nothing went
	// wrong, and no ✗ says so.
	m := mainWith(100, 30, 0)
	r := &runState{errs: map[string]string{"Work": "another stop of Work is running"}}
	if got := ansi.Strip(m.resultWords("Work", "busy", r)); got != "another stop is stopping it" {
		t.Errorf("%q", got)
	}
}

func TestTheHistoryNamesANoteThatCouldNotBeWritten(t *testing.T) {
	if got := stepName(history.Step{Step: "record", Code: 1}); got != "notes in /var/lib/wsl-spawner, exit 1" {
		t.Errorf("%q", got)
	}
	if got := stepWords(step{key: "record"}); got != "notes in /var/lib/wsl-spawner" {
		t.Errorf("%q", got)
	}
}
