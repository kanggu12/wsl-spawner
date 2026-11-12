// Package demo is a made-up PC for the screen's demo, its pictures and its
// tests: four distributions, containers, a last stop and a history, all in
// memory and a folder of its own. Nothing in it reaches the PC it runs on.
package demo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/gpo"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// Now is the made-up PC's clock: every time in the demo is around it, so
// its pictures are the same each time they are made.
var Now = time.Date(2026, 11, 12, 9, 30, 0, 0, time.Local)

// errNotHere is what the made-up PC answers for what it does not have.
var errNotHere = errors.New("the demo's PC has none of this")

// quiet is where the demo's build with no console would be.
const quiet = `C:\Users\you\AppData\Local\Programs\wsl-spawner\wsl-spawnerw.exe`

// box is one made-up distribution as it stands.
type box struct {
	setUp, kept bool
	// containers are its containers' probe lines, without the engine.
	docker, podman []string
	// failed are its failed units; systemd its state, "" for none.
	failed  []string
	systemd string
	// hooks are the programs in its /etc/wsl-spawner/pre-stop.d.
	hooks []string
	// runs are the starts noted in it, oldest first, the last the one that
	// runs: where it was set up before the demo began, its earlier starts
	// too; one set up in the demo has only its start now.
	runs []run
	// last is wsl-spawner's last stop of it as noted in it, nil for none.
	last *stopNote
	// resumed are what its start service brought back at its newest start,
	// as the probe says them, and when.
	resumed   []string
	resumedAt time.Time
	// again is set when a stop in the demo ended it: the start after it is
	// a new one, which brings back what that stop noted.
	again bool
}

// run is one start noted in a made-up distribution: when, and its number,
// which with the boot's id names it as the scripts do.
type run struct {
	at time.Time
	n  int
}

// stopNote is a stop of wsl-spawner's as a made-up distribution notes it:
// the start it ended, when it ended, how long it took and who asked.
type stopNote struct {
	run  int
	at   time.Time
	took int
	by   string
}

// bootID is every made-up distribution's boot, as its probe says it.
const bootID = "0d3e0b1a-demo"

// World is the made-up PC. Its fields change as the demo stops, starts and
// sets distributions.
type World struct {
	mu    sync.Mutex
	boxes map[string]*box
	// Fast plays a stop's steps quickly, 5 ms a second they say, for
	// pictures and tests.
	Fast bool
	// scripts is the sign-out scripts' text.
	scripts string
	// entry is the sign-in program's command line, "" for none.
	entry string
	// began is when this made-up PC was made, on this PC's clock: its own
	// runs on from Now since then.
	began time.Time
	// clockMu guards ahead, apart from mu, which the probe holds as it
	// reads the clock.
	clockMu sync.Mutex
	// ahead is how far the made-up PC's clock runs ahead of the time since
	// it was made: a stop played faster than its steps say moves it on to
	// where they say the stop ended.
	ahead time.Duration
}

// now is the made-up PC's clock: Now as it was made, moving on as time
// does, and as far on as its stops' steps say they took.
func (w *World) now() time.Time {
	w.clockMu.Lock()
	defer w.clockMu.Unlock()
	return Now.Add(time.Since(w.began) + w.ahead)
}

// reach moves the made-up PC's clock on to at, where it is not there yet:
// a stop's end, as its steps tell it.
func (w *World) reach(at time.Time) {
	w.clockMu.Lock()
	defer w.clockMu.Unlock()
	if d := at.Sub(Now.Add(time.Since(w.began) + w.ahead)); d > 0 {
		w.ahead += d
	}
}

// New makes the made-up PC and an App on it: its settings and history in a
// new folder of their own, which the caller removes with the returned func.
func New(fast bool) (*app.App, *World, func(), error) {
	dir, err := os.MkdirTemp("", "wsl-spawner-demo-*")
	if err != nil {
		return nil, nil, nil, err
	}
	done := func() { os.RemoveAll(dir) }
	// This morning's sign-in and last night's sign-out, which the history
	// keeps too.
	morning := time.Date(Now.Year(), Now.Month(), Now.Day(), 8, 58, 0, 0, time.Local)
	night := morning.Add(-10*time.Hour - 22*time.Minute)
	w := &World{Fast: fast, began: time.Now(), boxes: map[string]*box{
		"Ubuntu-24.04": {setUp: true, kept: true, systemd: "running",
			docker: []string{
				"c0ffee01\t/web\trunning\tunless-stopped\t\tnginx:1.27\tsite\t8080:80/tcp,",
				"c0ffee02\t/db\trunning\tno\t60\tpostgres:17\tsite\t",
				"c0ffee03\t/cache\trunning\talways\t\tredis:7\t\t",
				"c0ffee04\t/job\texited\tno\t\tbusybox:1.36\t\t",
			},
			hooks: []string{"10-backup-db"},
			// Set up before the demo began: yesterday's start, its stop as
			// Windows signed out, and this morning's start, which brought
			// db back and found web and cache back by their policies: the
			// three the containers' list says come back.
			runs:      []run{{night.Add(-14 * time.Hour), 100}, {morning, 300}},
			last:      &stopNote{run: 100, at: night.Add(41 * time.Second), took: 41, by: "signout"},
			resumed:   []string{"docker c0ffee02 db started", "docker c0ffee01 web running", "docker c0ffee03 cache running"},
			resumedAt: morning},
		"Debian": {systemd: "running"},
		"Work": {systemd: "degraded", failed: []string{"getty@tty1.service"},
			podman: []string{"bead0001\tnotes\trunning\tno\t10\tdocker.io/library/redis:7\t\t"}},
		"docker-desktop": {},
	}}
	// Another tool's sign-out script, and wsl-spawner's hook after it.
	w.scripts = gpo.Add("[Logoff]\r\n0CmdLine="+`C:\Tools\backup-notes.cmd`+"\r\n0Parameters=\r\n", quiet, "logoff")
	w.entry = `"` + quiet + `" logon`
	c := config.Default()
	c.Logon = []string{"Ubuntu-24.04"}
	// The commands read the made-up PC's clock, as its distributions do.
	a := &app.App{Config: c, ConfigPath: filepath.Join(dir, "config.toml"), Dir: dir, WSL: w.wsl(), PC: w.pc(), Clock: w.now}
	if err := seedHistory(a.Dir); err != nil {
		done()
		return nil, nil, nil, err
	}
	return a, w, done, nil
}

// seedHistory writes a made-up history: a week of starts at sign-in in the
// morning and stops as Windows signed out at night, one with a hook that
// failed, then last night's sign-out and this morning's sign-in, which the
// made-up distributions note too.
func seedHistory(dir string) error {
	s := history.Open(dir)
	night := time.Date(Now.Year(), Now.Month(), Now.Day()-1, 22, 36, 0, 0, time.Local)
	day := func(back int, h, m int) time.Time {
		return time.Date(Now.Year(), Now.Month(), Now.Day()-back, h, m, 0, 0, time.Local)
	}
	start := func(at time.Time, took float64) history.Entry {
		return history.Entry{At: at, Took: took, What: "start", By: "logon",
			Distros: []history.Distro{{Name: "Ubuntu-24.04", Result: "started", Took: took}}}
	}
	// steps are Ubuntu-24.04's stop as its setup makes it: its hook, Docker
	// with its three running containers, the sync; a stop with a step that
	// failed has its hook end with exit code 1.
	steps := func(took float64, result string) []history.Step {
		hook := history.Step{Step: "hook", Hook: "10-backup-db", Took: 2}
		if result == "failed" {
			hook.Code, hook.Took = 1, 1
		}
		return []history.Step{hook, {Step: "docker", Count: 3, Took: took - hook.Took - 1}, {Step: "sync", Took: 1}}
	}
	stop := func(at time.Time, took float64, result string) history.Entry {
		return history.Entry{At: at, Took: took + 1, What: "stop", By: "signout", Shutdown: true,
			Distros: []history.Distro{{Name: "Ubuntu-24.04", Result: result, Took: took, Steps: steps(took, result)}}}
	}
	for _, e := range []history.Entry{
		start(day(5, 9, 2), 8), stop(day(5, 23, 10), 39, "clean"),
		start(day(4, 8, 55), 9), stop(day(4, 22, 41), 36, "failed"),
		start(day(3, 9, 5), 8), stop(day(3, 21, 50), 40, "clean"),
		start(day(2, 8, 59), 9), stop(night.Add(-26*time.Hour), 37, "clean"),
		start(day(1, 8, 57), 8),
		{At: night, Took: 43, What: "stop", By: "signout", Shutdown: true,
			Distros: []history.Distro{{Name: "Ubuntu-24.04", Result: "clean", Took: 41, Steps: steps(41, "clean")},
				// Work runs one container under Podman, as its next stop says.
				{Name: "Work", Result: "clean", Took: 3, Steps: []history.Step{{Step: "podman", Count: 1, Took: 2}, {Step: "sync", Took: 1}}}}},
		start(night.Add(10*time.Hour+22*time.Minute), 9),
	} {
		if err := s.Add(e); err != nil {
			return err
		}
	}
	return nil
}

// wsl is the made-up WSL.
func (w *World) wsl() *wsl.Fake {
	f := &wsl.Fake{Version: "3.0.5.0", List: []wsl.Distro{
		{Name: "Ubuntu-24.04", Version: 2, Running: true, Default: true},
		{Name: "Debian", Version: 2},
		{Name: "Work", Version: 2, Running: true},
		{Name: "docker-desktop", Version: 2, Running: true},
	}}
	f.Script = func(distro, _, script string) (string, error) { return w.answer(distro, script) }
	f.Streamed = func(ctx context.Context, distro, _, script string, line func(string)) error {
		return w.stop(ctx, distro, script, line)
	}
	return f
}

// answer is what a made-up distribution says to a script.
func (w *World) answer(distro, script string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	b := w.boxes[distro]
	if b == nil {
		return "", errNotHere
	}
	switch {
	case script == inside.ProbeScript():
		return w.probe(distro, b), nil
	case strings.Contains(script, `rm -rf "$R/usr/local/lib/wsl-spawner"`):
		b.setUp, b.kept = false, false
		return "ok=1\n", nil
	case strings.Contains(script, "systemctl enable --quiet "+inside.StartUnit):
		b.setUp, b.kept = true, strings.Contains(script, "enable --quiet --now "+inside.KeepAliveUnit)
		return "ok=1\n", nil
	}
	return "", errNotHere
}

// probe is what the probe reads in a made-up distribution.
func (w *World) probe(distro string, b *box) string {
	var s strings.Builder
	at := func(t time.Time) string { return fmt.Sprint(t.Unix()) }
	if b.again {
		// Started again after a stop of the demo's: a new start, now, and
		// what that stop noted, brought back by the start service.
		n := 300
		if len(b.runs) > 0 {
			n = b.runs[len(b.runs)-1].n + 200
		}
		b.runs = append(b.runs, run{w.now(), n})
		b.resumedAt, b.again = w.now(), false
	}
	if b.setUp && len(b.runs) == 0 {
		// Set up in the demo: its start now is the first one noted.
		b.runs = []run{{time.Date(Now.Year(), Now.Month(), Now.Day(), 8, 58, 0, 0, time.Local), 300}}
	}
	// The start that runs, as the probe names it.
	initStart := 300
	if len(b.runs) > 0 {
		initStart = b.runs[len(b.runs)-1].n
	}
	fmt.Fprintf(&s, "probe=1\nnow=%d\nboot_id=%s\ninit_start=%d\ninit=systemd\nuptime=1920\nsystemd=%s\ninterop=1\nwaitfor=/mnt/c/Windows/System32/waitfor.exe\n", w.now().Unix(), bootID, initStart, b.systemd)
	for _, u := range b.failed {
		s.WriteString("failed=" + u + "\n")
	}
	if b.setUp {
		s.WriteString("unit=" + inside.StartUnit + "\tinactive\tenabled\n")
		s.WriteString("start_script=" + inside.StartScriptMark + inside.StartScriptVersion + "\n")
		for _, r := range b.runs {
			fmt.Fprintf(&s, "run=%s %s@%d\n", at(r.at), bootID, r.n)
		}
		if l := b.last; l != nil {
			fmt.Fprintf(&s, "stop=run=%s@%d\nstop=at=%s\nstop=took=%d\nstop=result=clean\nstop=by=%s\n", bootID, l.run, at(l.at), l.took, l.by)
		}
		if len(b.resumed) > 0 {
			s.WriteString("resumed=at " + at(b.resumedAt) + "\n")
			for _, r := range b.resumed {
				s.WriteString("resumed=" + r + "\n")
			}
		}
		for _, h := range b.hooks {
			s.WriteString("hook=" + h + "\n")
		}
	}
	if b.kept {
		s.WriteString("unit=" + inside.KeepAliveUnit + "\tactive\tenabled\n")
	}
	if len(b.docker) > 0 {
		s.WriteString("unit=docker.service\tactive\tenabled\ndocker=up\ndocker_live_restore=false\n")
		for _, c := range b.docker {
			s.WriteString("container=docker\t" + c + "\n")
		}
	}
	if len(b.podman) > 0 {
		s.WriteString("podman=up\n")
		for _, c := range b.podman {
			s.WriteString("container=podman\t" + c + "\n")
		}
	}
	s.WriteString("end=1\n")
	return s.String()
}

// stop plays a made-up distribution's stop, step by step.
func (w *World) stop(ctx context.Context, distro, script string, line func(string)) error {
	if !strings.HasPrefix(script, "LIMIT=") {
		return errNotHere
	}
	w.mu.Lock()
	b := w.boxes[distro]
	w.mu.Unlock()
	if b == nil {
		return errNotHere
	}
	// wait plays a step of secs seconds, faster than they are: a sixth as
	// long for the demo, 5 ms a second for pictures and tests. Played in
	// step with what they say, two stops at once end in the order they
	// would, each read at its own end before the other moves the clock on.
	wait := func(secs int) error {
		d := time.Duration(secs) * time.Second / 6
		if w.Fast {
			d = time.Duration(secs) * 5 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
			return nil
		}
	}
	running := func(cs []string) int {
		n := 0
		for _, c := range cs {
			if strings.Contains(c, "\trunning\t") {
				n++
			}
		}
		return n
	}
	var steps [][3]string
	// Its hooks run where it is set up, 2 s each.
	if b.setUp {
		for _, h := range b.hooks {
			steps = append(steps, [3]string{"hook=" + h, "", "2"})
		}
	}
	if n := running(b.docker); n > 0 {
		steps = append(steps, [3]string{"step=docker", fmt.Sprint(n), "21"})
	}
	if n := running(b.podman); n > 0 {
		steps = append(steps, [3]string{"step=podman", fmt.Sprint(n), "1"})
	}
	for _, s := range steps {
		k, v, _ := strings.Cut(s[0], "=")
		if k == "hook" {
			line("plan=hook\t" + v)
		} else {
			line("plan=" + v + "\t" + s[1])
		}
	}
	line("plan=sync\t")
	// at is where this stop's own time has got to: its start, then each
	// step's end as the step says.
	begun := w.now()
	at := begun
	for _, s := range steps {
		line(s[0] + "\tstart\t" + s[1])
		secs, _ := strconv.Atoi(s[2])
		if err := wait(secs); err != nil {
			return err
		}
		at = at.Add(time.Duration(secs) * time.Second)
		line(s[0] + "\tdone\t0\t" + s[2])
	}
	line("step=sync\tstart")
	line("step=sync\tdone\t0\t0")
	// The clock moves on once, at the stop's end: a step of another stop
	// under way at the same time does not move this one's.
	w.reach(at)
	w.noteStop(b, script, at, int(at.Sub(begun)/time.Second))
	line("end=clean")
	return nil
}

// noteStop notes a stop of the demo's in the made-up distribution, as the
// stop script does where wsl-spawner is set up: the start it ended, when it
// ended and took, who asked, and what its next start brings back, as the
// script's RESUME says. Its next start is a new one.
func (w *World) noteStop(b *box, script string, at time.Time, took int) {
	// Who asked and what comes back, as the script was told.
	vars := map[string]string{"BY": "hand", "RESUME": "running"}
	for _, l := range strings.Split(script, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok && (k == "BY" || k == "RESUME") {
			vars[k] = v
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	b.again = true
	if !b.setUp || len(b.runs) == 0 {
		// Nothing of wsl-spawner's in it notes a stop.
		return
	}
	b.last = &stopNote{run: b.runs[len(b.runs)-1].n, at: at, took: took, by: vars["BY"]}
	// What runs comes back: every one with resume = running, those with a
	// restart policy with policy, by wsl-spawner where none brings it back
	// by itself, and none with off.
	b.resumed = nil
	for _, e := range []struct {
		name  string
		lines []string
	}{{"docker", b.docker}, {"podman", b.podman}} {
		for _, c := range e.lines {
			// id, name, state, policy, … as the probe's lines give them.
			f := strings.Split(c, "\t")
			if len(f) < 4 || f[2] != "running" {
				continue
			}
			byPolicy := f[3] == "always" || f[3] == "unless-stopped"
			switch {
			case vars["RESUME"] == "off", vars["RESUME"] == "policy" && !byPolicy:
				continue
			case byPolicy:
				b.resumed = append(b.resumed, e.name+" "+f[0]+" "+strings.TrimPrefix(f[1], "/")+" running")
			default:
				b.resumed = append(b.resumed, e.name+" "+f[0]+" "+strings.TrimPrefix(f[1], "/")+" started")
			}
		}
	}
}

// pc is the made-up PC's Windows side.
func (w *World) pc() app.PC {
	return app.PC{
		ReadScripts: func() (string, error) { w.mu.Lock(); defer w.mu.Unlock(); return w.scripts, nil },
		InstallHook: func(exe, params string, drop ...string) error {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.scripts = gpo.Add(w.scripts, exe, params, drop...)
			return nil
		},
		UninstallHook: func(exe string, drop ...string) error {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.scripts = gpo.Remove(w.scripts, exe, drop...)
			return nil
		},
		// The demo's own copy is the administrator's: nothing to ask.
		Elevated:    func() bool { return true },
		RunElevated: func(string, []string) (int, error) { return 0, errNotHere },
		MaxWait:     func() time.Duration { return 600 * time.Second },
		LogonEntry: func(string) (string, bool, bool, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			return w.entry, w.entry != "", false, nil
		},
		SetLogonEntry:    func(_, cmd string) error { w.mu.Lock(); defer w.mu.Unlock(); w.entry = cmd; return nil },
		DeleteLogonEntry: func(string) error { w.mu.Lock(); defer w.mu.Unlock(); w.entry = ""; return nil },
		QuietExe:         func() (string, error) { return quiet, nil },
		ReadFile: func(p string) ([]byte, error) {
			if strings.HasSuffix(p, ".wslconfig") {
				return []byte("[wsl2]\nnetworkingMode=mirrored\n"), nil
			}
			return nil, errNotHere
		},
		HomeDir: func() (string, error) { return `C:\Users\you`, nil },
		ExcludedPorts: func(proto string) (string, error) {
			if proto == "tcp" {
				return "      1080        1179\n     50000       50059     *\n", nil
			}
			return "", nil
		},
		StartDetached: func([]string) error { return errNotHere },
		// The made-up PC's screen runs in none of its distributions, wherever
		// the demo is opened.
		TerminalDistro: func() string { return "" },
	}
}
