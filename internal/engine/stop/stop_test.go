package stop

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
)

// fixed is a clock that moves a second each time it is read, from any
// number of goroutines, as Run reads it.
func fixed() func() time.Time {
	var mu sync.Mutex
	t := time.Unix(1700000000, 0)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t = t.Add(time.Second)
		return t
	}
}

// opts are a stop's options as the settings give them by default.
var opts = Options{Limit: 300 * time.Second, Resume: "running", By: "hand"}

// printing is a made-up WSL whose stop script prints lines for each
// distribution.
func printing(lines map[string][]string) *wsl.Fake {
	f := &wsl.Fake{}
	for name := range lines {
		f.List = append(f.List, wsl.Distro{Name: name, Version: 2, Running: true})
	}
	slices.SortFunc(f.List, func(a, b wsl.Distro) int { return strings.Compare(a.Name, b.Name) })
	f.Streamed = func(_ context.Context, distro, user, script string, line func(string)) error {
		if user != "root" || !strings.HasPrefix(script, "LIMIT=300\nRESUME=running\nBY=hand\n") {
			return errors.New("not the stop script as root")
		}
		for _, l := range lines[distro] {
			line(l)
		}
		return nil
	}
	return f
}

func TestEveryDistributionStopsAndThenWSLEndsIt(t *testing.T) {
	f := printing(map[string][]string{
		"Dev":  {"plan=docker\t2", "step=docker\tstart\t2", "step=docker\tdone\t0\t21", "end=clean"},
		"Work": {"plan=sync\t", "hook=10-save\tstart", "hook=10-save\tdone\t3\t1", "end=failed"},
	})
	var events []string
	results, err := Run(context.Background(), f, []string{"Dev", "Work"}, opts, fixed(), func(e Event) {
		events = append(events, e.Distro+":"+e.Kind)
	})
	if err != nil {
		t.Fatal(err)
	}
	dev, work := results[0], results[1]
	if !dev.Clean() || len(dev.Steps) != 1 || dev.Steps[0].Count != 2 || dev.Steps[0].Took != 21 {
		t.Fatalf("Dev %+v", dev)
	}
	// A hook that failed makes the stop not clean, and says which.
	if work.Clean() || work.End != "failed" || work.Steps[0].Hook != "10-save" || work.Steps[0].Code != 3 {
		t.Fatalf("Work %+v", work)
	}
	// Each distribution: its lines, then WSL ends it, then its result.
	for _, d := range []string{"Dev", "Work"} {
		var mine []string
		for _, e := range events {
			if strings.HasPrefix(e, d+":") {
				mine = append(mine, strings.TrimPrefix(e, d+":"))
			}
		}
		if n := len(mine); n < 3 || mine[n-3] != "terminate" || mine[n-2] != "terminate" || mine[n-1] != "finished" {
			t.Errorf("%s: %q", d, mine)
		}
	}
	// Nothing left running, and WSL itself not stopped: it was not asked.
	calls := f.CallList()
	if slices.Contains(calls, "shutdown:") || !slices.Contains(calls, "terminate:Dev") || !slices.Contains(calls, "terminate:Work") {
		t.Fatalf("calls %q", calls)
	}
}

func TestAScriptThatDoesNotEndStillEndsTheDistribution(t *testing.T) {
	f := printing(map[string][]string{"Dev": {"plan=sync\t"}})
	// The script starts, says its plan, then WSL fails it.
	f.Streamed = func(_ context.Context, _, _, _ string, line func(string)) error {
		line("plan=sync\t")
		return errors.New("wsl: exit status 1")
	}
	results, err := Run(context.Background(), f, []string{"Dev"}, opts, fixed(), func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	r := results[0]
	if r.End != "error" || r.Err == nil || !strings.Contains(r.Err.Error(), "did not run to its end") || r.Clean() {
		t.Fatalf("%+v", r)
	}
	if !slices.Contains(f.CallList(), "terminate:Dev") {
		t.Fatal("not ended")
	}
}

func TestStoppedWaitingTheDistributionStillGoesDown(t *testing.T) {
	// Ctrl+C while the script runs: waiting for it stops, but the
	// distribution is not left half stopped.
	f := printing(map[string][]string{"Dev": nil})
	ctx, cancel := context.WithCancel(context.Background())
	f.Streamed = func(ctx context.Context, _, _, _ string, line func(string)) error {
		line("step=docker\tstart\t1")
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}
	results, _ := Run(ctx, f, []string{"Dev"}, opts, fixed(), func(Event) {})
	if !slices.Contains(f.CallList(), "terminate:Dev") || results[0].Clean() {
		t.Fatalf("%q %+v", f.CallList(), results[0])
	}
}

func TestShutdownComesAfterEveryDistribution(t *testing.T) {
	f := printing(map[string][]string{"A": {"end=clean"}, "B": {"end=clean"}})
	o := opts
	o.Shutdown = true
	if _, err := Run(context.Background(), f, []string{"A", "B"}, o, fixed(), func(Event) {}); err != nil {
		t.Fatal(err)
	}
	calls := f.CallList()
	if calls[len(calls)-1] != "shutdown:" || slices.Index(calls, "terminate:A") > len(calls)-2 || slices.Index(calls, "terminate:B") > len(calls)-2 {
		t.Fatalf("calls %q", calls)
	}
	// A shutdown that fails is the run's error.
	f = printing(map[string][]string{"A": {"end=clean"}})
	f.Fail = map[string]error{"shutdown:": errors.New("boom")}
	if _, err := Run(context.Background(), f, []string{"A"}, o, fixed(), func(Event) {}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
}

func TestAWSLThatDoesNotEndTheDistributionIsSaid(t *testing.T) {
	f := printing(map[string][]string{"Dev": {"end=clean"}})
	f.Fail = map[string]error{"terminate:Dev": errors.New("Access is denied")}
	results, _ := Run(context.Background(), f, []string{"Dev"}, opts, fixed(), func(Event) {})
	if r := results[0]; r.Clean() || r.End != "clean" || !strings.Contains(r.Err.Error(), "did not end Dev") {
		t.Fatalf("%+v", r)
	}
}

func TestOptionsThatCannotGoIntoTheScriptStopNothing(t *testing.T) {
	f := printing(map[string][]string{"Dev": {"end=clean"}})
	o := opts
	o.By = "by hand"
	if _, err := Run(context.Background(), f, []string{"Dev"}, o, fixed(), func(Event) {}); err == nil || len(f.CallList()) != 0 {
		t.Fatalf("%v %q", err, f.CallList())
	}
}

func TestEntry(t *testing.T) {
	results := []Result{{Distro: "Dev", End: "clean", Took: 3 * time.Second}, {Distro: "Work", End: "error", Err: errors.New("x")}}
	e := Entry(results, Options{By: "signout", Shutdown: true}, time.Unix(5, 0), 4*time.Second, nil, "0.1.0")
	if e.What != "stop" || e.By != "signout" || !e.Shutdown || e.Took != 4 || len(e.Distros) != 2 || e.Distros[1].Err != "x" || e.Version != "0.1.0" {
		t.Fatalf("%+v", e)
	}
}
