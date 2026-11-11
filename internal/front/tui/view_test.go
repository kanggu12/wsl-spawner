package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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
