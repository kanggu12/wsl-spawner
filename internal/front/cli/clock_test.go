package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/app/demo"
	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// demoApp is the demo's made-up PC, its steps played at once, gone when
// the test ends.
func demoApp(t *testing.T) *app.App {
	t.Helper()
	a, _, done, err := demo.New(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	return a
}

// clockFindings are the check's findings about a distribution's clock.
func clockFindings(t *testing.T, a *app.App) []api.Finding {
	t.Helper()
	out, _, _ := cli(t, a, false, "check", "--json")
	var got api.Check
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var clocks []api.Finding
	for _, g := range got.Groups {
		for _, it := range g.Items {
			// The finding the check makes of a clock off Windows'.
			if it.Key == "d.clock" {
				clocks = append(clocks, it)
			}
		}
	}
	return clocks
}

func TestTheDemosCheckReadsItsOwnClock(t *testing.T) {
	// The demo's distributions read the demo's clock: set beside this
	// PC's, every one would be hours off.
	if got := clockFindings(t, demoApp(t)); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

// stepClock is a clock that moves only when told to: a test stands it in
// for the time a probe takes.
type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

// now is the clock's time.
func (c *stepClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

// add moves the clock on.
func (c *stepClock) add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

func TestCheckJudgesADistributionsClockAroundItsOwnProbe(t *testing.T) {
	// Three distributions read at once. Slow's probe takes a minute, its
	// engines slow to answer; Fast's takes none; Off's clock is an hour
	// ahead. Only Off's clock is off Windows': time a probe takes, its own
	// or another's, is no clock off.
	clk := &stepClock{t: time.Unix(1800000000, 0)}
	f := &wsl.Fake{List: []wsl.Distro{
		{Name: "Fast", Version: 2, Running: true},
		{Name: "Slow", Version: 2, Running: true},
		{Name: "Off", Version: 2, Running: true},
	}}
	f.Script = func(distro, _, script string) (string, error) {
		if script != inside.ProbeScript() {
			return "", errors.New("not the probe")
		}
		// The probe reads the distribution's clock as it starts.
		at := clk.now()
		switch distro {
		case "Slow":
			clk.add(time.Minute)
		case "Off":
			at = at.Add(time.Hour)
		}
		return fmt.Sprintf("probe=1\nnow=%d\nstart_script=%s1\nend=1\n", at.Unix(), inside.StartScriptMark), nil
	}
	a := &app.App{Config: config.Default(), WSL: f, Dir: t.TempDir(), PC: testPC(), Clock: clk.now}
	got := clockFindings(t, a)
	if len(got) != 1 || len(got[0].DoArgs) != 1 || got[0].DoArgs[0] != "Off" {
		t.Fatalf("%+v", got)
	}
}

func TestADemoStopIsKeptAtTheDemosTime(t *testing.T) {
	// A stop made in the demo goes into its history at the demo's time,
	// after the made-up entries, not at this PC's.
	a := demoApp(t)
	if out, errOut, code := cli(t, a, false, "stop", "--yes", "Ubuntu-24.04"); code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	all, err := history.Open(a.Dir).All()
	if err != nil || len(all) == 0 {
		t.Fatalf("%+v %v", all, err)
	}
	last := all[len(all)-1]
	if last.What != "stop" || last.At.Sub(demo.Now).Abs() > time.Minute {
		t.Fatalf("kept at %s, the demo's time is %s", last.At, demo.Now)
	}
}
