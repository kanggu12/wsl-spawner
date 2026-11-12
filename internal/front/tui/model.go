// Package tui is wsl-spawner's terminal screen: every distribution with what
// wsl-spawner does with it, how it last stopped and what runs in it, and
// the stops, starts and choices made from it. It stands on the commands'
// JSON alone (api.Client), so it does nothing a command cannot, and runs
// the same in the Windows program and in a WSL terminal.
package tui

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/engine/setup"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/front/ui"
)

// mode is what the screen shows.
type mode int

const (
	modeMain    mode = iota // the distributions
	modeRun                 // a stop or start under way, then how it went
	modeCheck               // the check's findings
	modeHistory             // past stops and starts
	modeHelp                // the keys and how it works
)

// Messages the model gets from its commands.
type (
	// loadMsg is everything the main screen shows, read at once.
	loadMsg struct {
		st   api.Status
		hook *api.Hook
		hist []history.Entry
		err  error
	}
	// checkMsg is the check's findings.
	checkMsg struct {
		ch  api.Check
		err error
	}
	// eventMsg is one step of a stop; stopMsg how the stop ended.
	eventMsg api.StopEvent
	stopMsg  struct {
		res api.StopResult
		err error
	}
	// startMsg is how a start ended; setMsg how a change of choices did.
	startMsg struct {
		res api.StartResult
		err error
	}
	setMsg struct {
		res api.SetResult
		err error
	}
	// tickMsg moves the spinner and the clock while something runs.
	tickMsg time.Time
	// bugMsg is a bug in a goroutine of the screen's, past every recover:
	// the screen ends and the program says it was a bug.
	bugMsg BugError
)

// BugError is a bug the screen stopped on: the value and where it was.
type BugError struct {
	Value any
	Stack []byte
}

func (b *BugError) Error() string { return fmt.Sprintf("the screen stopped on a bug: %v", b.Value) }

// action is what the screen asks about before doing it.
type action struct {
	// kind is "stop", "start" or "set".
	kind string
	// names are the distributions it is for; shutdown stops WSL after.
	names    []string
	shutdown bool
	// ask is a change of choices, for "set"; cur and want the choices
	// before and after.
	ask       setup.Ask
	cur, want setup.State
}

// step is one step of a distribution's stop, as the screen shows it.
type step struct {
	// key names it: "hook:NAME", "units", "docker", "podman", "sync",
	// "terminate".
	key string
	// count is how many it stops; took how long it took once done.
	count int
	took  time.Duration
	// state is "wait", "run", "done", "late" (out of time) or "failed".
	state string
	// since is when it began running.
	since time.Time
}

// runState is a stop or a start under way, then its end.
type runState struct {
	act action
	// steps are each distribution's steps, in order.
	steps map[string][]step
	// started is when it began; ended when it ended.
	started, ended time.Time
	// results are how each distribution went, by name: "clean", "limit",
	// "failed", "error", "started", "running".
	results map[string]string
	errs    map[string]string
	// back are the containers that came back at a start, by distribution.
	back map[string][]api.CameBack
	// took is how long each distribution's stop took, as it said at its end.
	took map[string]time.Duration
	// wsl is WSL's own stop after them, when asked: "", "run", "done" or
	// "failed".
	wsl string
	// err is what went wrong around them; done is set at the end.
	err  error
	done bool
	// events carries the stop's steps into the model.
	events chan tea.Msg
}

// model is the screen's state.
type model struct {
	ctx context.Context
	c   api.Client
	th  ui.Theme
	// width and height are the terminal's.
	width, height int
	mode          mode
	// What the main screen shows, and how reading it went.
	st api.Status
	// seen is each distribution as last read running in this session: a
	// distribution stopped since says nothing, and the screen still knows
	// what was set up in it.
	seen    map[string]api.Probe
	hook    *api.Hook
	hist    []history.Entry
	err     error
	loaded  bool
	loading bool
	// sel is the selected distribution's index in st.List.
	sel int
	// ask is the action the box asks about; nil when there is none.
	ask *action
	// run is the stop or start under way, or the last one.
	run *runState
	// check is the check's findings, read when its screen opens.
	check    *api.Check
	checkErr error
	checking bool
	// scroll is how far the check, history and help screens are scrolled.
	scroll int
	// note is a line for the status line: what just happened.
	note    string
	noteCat ui.Category
	// now and spin move the spinner and the elapsed times.
	now  time.Time
	spin int
	// bug is a bug the screen stopped on, for Run to say.
	bug *BugError
}

// newModel is the screen before anything is read.
func newModel(ctx context.Context, c api.Client, dark bool) model {
	return model{ctx: ctx, c: c, th: ui.NewTheme(dark), width: 100, height: 30, now: time.Now(), seen: map[string]api.Probe{}}
}

// Init reads the distributions.
func (m model) Init() tea.Cmd { return m.load() }

// guard runs f, and turns a bug in it into a bugMsg for the screen to end
// on, rather than a bare Go panic from a goroutine.
func guard(f func() tea.Msg) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = bugMsg{Value: r, Stack: debug.Stack()}
			}
		}()
		return f()
	}
}

// load reads everything the main screen shows.
func (m model) load() tea.Cmd {
	c, ctx := m.c, m.ctx
	return guard(func() tea.Msg {
		st, err := c.Status(ctx)
		if err != nil {
			return loadMsg{err: err}
		}
		var hook *api.Hook
		if h, err := c.Hook(ctx); err == nil {
			hook = &h
		}
		hist, _ := c.History(ctx)
		return loadMsg{st: st, hook: hook, hist: hist}
	})
}

// tick moves the spinner while something runs.
func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// selected is the selected distribution.
func (m model) selected() (api.Distro, bool) {
	if m.sel < 0 || m.sel >= len(m.st.List) {
		return api.Distro{}, false
	}
	return m.st.List[m.sel], true
}

// ours reports whether wsl-spawner stops and starts a distribution.
func ours(d api.Distro) bool {
	return d.Kind == "set-up" || d.Kind == "stop-only" || d.Kind == "not-read"
}

// probeOf is what runs in a distribution: read now, or, for one stopped
// since it was read in this session, as it was then.
func (m model) probeOf(d api.Distro) *api.Probe {
	if d.Probe != nil {
		return d.Probe
	}
	if p, ok := m.seen[d.Name]; ok && !d.Running {
		return &p
	}
	return nil
}

// level is how far wsl-spawner goes with a distribution, as setup counts:
// 0 stopped with its services' time, 1 set up, 2 kept running, 3 started at
// sign-in. known is false for one stopped and never read: what is set up in
// it shows once it runs, unless the settings start it at sign-in, which
// holds the other two.
func (m model) level(d api.Distro) (lv int, known bool) { return levelIn(m.probeOf(d), d.AtLogon) }

// levelIn is the level what runs in a distribution says, with whether the
// settings start it at sign-in; p is nil when it was not read.
func levelIn(p *api.Probe, atLogon bool) (lv int, known bool) {
	switch {
	case p != nil:
		if p.SetUp {
			lv = 1
			if p.KeptRunning {
				lv = 2
				if atLogon {
					lv = 3
				}
			}
		}
		return lv, true
	case atLogon:
		return 3, true
	}
	return 0, false
}

// stateOf is a distribution's choices as the screen knows them, and
// whether it knows them.
func (m model) stateOf(d api.Distro) (setup.State, bool) {
	lv, known := m.level(d)
	return setup.State{Managed: lv >= 1, Keep: lv >= 2, Logon: d.AtLogon, Skip: d.Kind == "skipped"}, known
}

// Update handles a message.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case bugMsg:
		b := BugError(msg)
		m.bug = &b
		return m, tea.Quit
	case loadMsg:
		m.loading = false
		m.loaded, m.err = true, msg.err
		if msg.err == nil {
			// The same distribution stays selected, wherever it moved.
			name := ""
			if d, ok := m.selected(); ok {
				name = d.Name
			}
			m.st, m.hook, m.hist = msg.st, msg.hook, msg.hist
			m.sel = max(0, slices.IndexFunc(m.st.List, func(d api.Distro) bool { return d.Name == name }))
			// What each running distribution holds, kept for when it stops.
			for _, d := range m.st.List {
				if d.Probe != nil {
					m.seen[d.Name] = *d.Probe
				}
			}
		}
		return m, nil
	case checkMsg:
		m.checking = false
		if msg.err != nil {
			m.checkErr = msg.err
		} else {
			ch := msg.ch
			m.check, m.checkErr = &ch, nil
		}
		return m, nil
	case tickMsg:
		m.now = time.Time(msg)
		m.spin++
		if (m.run != nil && !m.run.done) || m.checking || m.loading {
			return m, tick()
		}
		return m, nil
	case eventMsg:
		if m.run != nil {
			m.run.event(api.StopEvent(msg), m.now)
			return m, m.run.next()
		}
		return m, nil
	case stopMsg:
		return m.endStop(msg), m.load()
	case startMsg:
		return m.endStart(msg), m.load()
	case setMsg:
		m.loading = true
		if msg.err != nil {
			m.note, m.noteCat = errWords(msg.err), ui.CatCut
		} else {
			m.note, m.noteCat = i18n.T("note.set", msg.res.Name, choiceWords(setup.State{Managed: msg.res.Managed, Keep: msg.res.Keep, Logon: msg.res.Logon, Skip: msg.res.Skip})), ui.CatClean
		}
		return m, tea.Batch(m.load(), tick())
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// key handles a key on the screen open.
func (m model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	// The box asks first: enter does it, esc does not.
	if m.ask != nil {
		switch key {
		case "enter", "y":
			return m.do(*m.ask)
		case "esc", "n", "q":
			m.ask = nil
		}
		return m, nil
	}
	switch m.mode {
	case modeRun:
		// While it runs, only quitting waits; once done, any key goes back.
		if m.run != nil && m.run.done && (key == "enter" || key == "esc" || key == "q" || key == "backspace") {
			m.mode = modeMain
		}
		return m, nil
	case modeCheck, modeHistory, modeHelp:
		switch key {
		case "esc", "q", "backspace", "enter":
			m.mode, m.scroll = modeMain, 0
		case "down", "j":
			m.scroll++
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "pgdown", "space":
			m.scroll += max(1, m.height-6)
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.height-6))
		case "home":
			m.scroll = 0
		case "r":
			if m.mode == modeCheck && !m.checking {
				m.checking = true
				return m, tea.Batch(m.readCheck(), tick())
			}
		}
		return m, nil
	}
	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		// Back from every other screen; here there is nothing to go back
		// to, and a second esc from the check must not quit: it clears
		// what the status line says, and nothing more.
		m.note = ""
		return m, nil
	case "down":
		if m.sel < len(m.st.List)-1 {
			m.sel++
		}
	case "up":
		if m.sel > 0 {
			m.sel--
		}
	case "home":
		m.sel = 0
	case "end":
		m.sel = max(0, len(m.st.List)-1)
	case "r":
		if !m.loading {
			m.loading, m.note = true, ""
			return m, tea.Batch(m.load(), tick())
		}
	case "c":
		m.mode, m.scroll, m.checking = modeCheck, 0, true
		return m, tea.Batch(m.readCheck(), tick())
	case "h":
		m.mode, m.scroll = modeHistory, 0
	case "?":
		m.mode, m.scroll = modeHelp, 0
	case "s":
		return m.askStop(false)
	case "S":
		return m.askStop(true)
	case "t":
		return m.askStart()
	case "m", "k", "l", "x":
		return m.askSet(key)
	}
	return m, nil
}

// readCheck reads the check's findings.
func (m model) readCheck() tea.Cmd {
	c, ctx := m.c, m.ctx
	return guard(func() tea.Msg {
		ch, err := c.Check(ctx)
		return checkMsg{ch: ch, err: err}
	})
}

// askStop asks before stopping the selected distribution, or every running
// one of wsl-spawner's and WSL after (all).
func (m model) askStop(all bool) (tea.Model, tea.Cmd) {
	if all {
		var names []string
		for _, d := range m.st.List {
			if d.Running && ours(d) {
				names = append(names, d.Name)
			}
		}
		m.ask = &action{kind: "stop", names: names, shutdown: true}
		return m, nil
	}
	d, ok := m.selected()
	switch {
	case !ok:
		return m, nil
	case !ours(d):
		m.note, m.noteCat = i18n.T("note.notours", d.Name), ui.CatWarn
		return m, nil
	case !d.Running:
		m.note, m.noteCat = i18n.T("note.notrunning", d.Name), ui.CatStopped
		return m, nil
	}
	m.ask = &action{kind: "stop", names: []string{d.Name}}
	return m, nil
}

// askStart asks before starting the selected distribution.
func (m model) askStart() (tea.Model, tea.Cmd) {
	d, ok := m.selected()
	switch {
	case !ok:
		return m, nil
	case !ours(d):
		m.note, m.noteCat = i18n.T("note.notours", d.Name), ui.CatWarn
		return m, nil
	case d.Running:
		m.note, m.noteCat = i18n.T("note.running", d.Name), ui.CatRunning
		return m, nil
	}
	m.ask = &action{kind: "start", names: []string{d.Name}}
	return m, nil
}

// askSet asks before turning one of the selected distribution's choices:
// m set up, k kept running, l started at sign-in, x left to WSL.
func (m model) askSet(key string) (tea.Model, tea.Cmd) {
	d, ok := m.selected()
	if !ok {
		return m, nil
	}
	if d.Owner != "" || d.Version == 1 {
		m.note, m.noteCat = i18n.T("note.notours", d.Name), ui.CatWarn
		return m, nil
	}
	cur, known := m.stateOf(d)
	// A choice inside a distribution stopped and never read is not guessed:
	// what is set up in it shows once it runs. Leaving it to WSL is the
	// settings' alone.
	if !known && key != "x" {
		m.note, m.noteCat = i18n.T("note.unknown", d.Name), ui.CatStopped
		return m, nil
	}
	flip := func(v bool) *bool { b := !v; return &b }
	var a setup.Ask
	switch key {
	case "m":
		a.Managed = flip(cur.Managed)
	case "k":
		a.Keep = flip(cur.Keep)
	case "l":
		a.Logon = flip(cur.Logon)
	case "x":
		a.Skip = flip(cur.Skip)
	}
	want, err := setup.Want(cur, a)
	if err != nil {
		m.note, m.noteCat = err.Error(), ui.CatWarn
		return m, nil
	}
	m.ask = &action{kind: "set", names: []string{d.Name}, ask: a, cur: cur, want: want}
	return m, nil
}

// do starts what the box asked about.
func (m model) do(a action) (tea.Model, tea.Cmd) {
	m.ask, m.note = nil, ""
	c, ctx := m.c, m.ctx
	switch a.kind {
	case "set":
		return m, guard(func() tea.Msg {
			res, err := c.Set(ctx, a.names[0], a.ask.Managed, a.ask.Keep, a.ask.Logon, a.ask.Skip)
			return setMsg{res: res, err: err}
		})
	case "start":
		m.run = newRun(a, m.now)
		m.mode = modeRun
		return m, tea.Batch(guard(func() tea.Msg {
			res, err := c.Start(ctx, a.names)
			return startMsg{res: res, err: err}
		}), tick())
	}
	// A stop: its steps come one by one, through the run's channel.
	r := newRun(a, m.now)
	m.run, m.mode = r, modeRun
	events := r.events
	go func() {
		defer func() {
			if v := recover(); v != nil {
				events <- bugMsg{Value: v, Stack: debug.Stack()}
			}
		}()
		res, err := c.Stop(ctx, a.names, a.shutdown, func(e api.StopEvent) { events <- eventMsg(e) })
		events <- stopMsg{res: res, err: err}
	}()
	return m, tea.Batch(r.next(), tick())
}

// newRun is a run of a, begun at now.
func newRun(a action, now time.Time) *runState {
	r := &runState{act: a, steps: map[string][]step{}, started: now, results: map[string]string{}, errs: map[string]string{},
		back: map[string][]api.CameBack{}, took: map[string]time.Duration{}, events: make(chan tea.Msg, 64)}
	for _, n := range a.names {
		r.steps[n] = nil
	}
	return r
}

// next waits for the run's next message.
func (r *runState) next() tea.Cmd {
	events := r.events
	return func() tea.Msg { return <-events }
}

// event takes one step of a stop into the run.
func (r *runState) event(e api.StopEvent, now time.Time) {
	steps := r.steps[e.Distro]
	find := func(key string) int { return slices.IndexFunc(steps, func(s step) bool { return s.key == key }) }
	key := e.Step
	if e.Hook != "" {
		key = "hook:" + e.Hook
	}
	switch e.Event {
	case "line":
		switch e.Kind {
		case "plan":
			if find(key) < 0 {
				steps = append(steps, step{key: key, count: e.Count, state: "wait"})
			}
		case "start":
			if i := find(key); i >= 0 {
				steps[i].state, steps[i].since = "run", now
				if e.Count > 0 {
					steps[i].count = e.Count
				}
			} else {
				steps = append(steps, step{key: key, count: e.Count, state: "run", since: now})
			}
		case "done":
			i := find(key)
			if i < 0 {
				steps = append(steps, step{key: key})
				i = len(steps) - 1
			}
			steps[i].took = time.Duration(e.Took * float64(time.Second))
			switch e.Code {
			case 0:
				steps[i].state = "done"
			case 124, 137:
				steps[i].state = "late"
			default:
				steps[i].state = "failed"
			}
		}
	case "terminate":
		i := find("terminate")
		if i < 0 {
			steps = append(steps, step{key: "terminate", state: "run", since: now})
		} else if e.Done {
			steps[i].state, steps[i].took = "done", now.Sub(steps[i].since)
			if e.Err != "" {
				steps[i].state = "failed"
			}
		}
	case "finished":
		r.results[e.Distro] = e.Result
		r.took[e.Distro] = time.Duration(e.Took * float64(time.Second))
		if e.Err != "" {
			r.errs[e.Distro] = e.Err
		}
	case "shutdown":
		// WSL's own stop, after every distribution: running, then done.
		switch {
		case !e.Done:
			r.wsl = "run"
		case e.Err != "":
			r.wsl = "failed"
		default:
			r.wsl = "done"
		}
		return
	}
	r.steps[e.Distro] = steps
}

// endStop takes a stop's end into the screen.
func (m model) endStop(msg stopMsg) model {
	if m.run == nil {
		return m
	}
	r := m.run
	r.done, r.ended, r.err = true, m.now, msg.err
	for _, d := range msg.res.Results {
		r.results[d.Name] = d.Result
		if d.Err != "" {
			r.errs[d.Name] = d.Err
		}
	}
	if msg.res.Err != "" && r.err == nil {
		r.err = errors.New(msg.res.Err)
	}
	m.loading = true
	return m
}

// endStart takes a start's end into the screen.
func (m model) endStart(msg startMsg) model {
	if m.run == nil {
		return m
	}
	r := m.run
	r.done, r.ended, r.err = true, m.now, msg.err
	for _, d := range msg.res.Results {
		r.results[d.Name] = d.Result
		r.back[d.Name] = d.Back
		if d.Err != "" {
			r.errs[d.Name] = d.Err
		}
	}
	m.loading = true
	return m
}

// errWords is an error as the status line says it: its first line.
func errWords(err error) string {
	s := strings.TrimSpace(err.Error())
	first, _, _ := strings.Cut(s, "\n")
	return first
}
