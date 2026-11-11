package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kanggu12/wsl-spawner/internal/engine/setup"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/front/ui"
	"github.com/kanggu12/wsl-spawner/internal/version"
)

// The wizard's pages, in order.
const (
	pWelcome = iota
	pLanguage
	pColors
	pMode
	pDistros
	pHook
	pConfirm
	pApply
)

// pages are the pages the progress counts: welcome to confirm.
const pages = pConfirm + 1

// Settings is what the wizard starts from: the settings as they are, the
// shape the hidden settings command prints.
type Settings struct {
	Language      string   `json:"language"`
	Theme         string   `json:"theme"`
	Mode          string   `json:"mode"`
	Logon         []string `json:"logon"`
	Exists        bool     `json:"exists"`
	WindowsKorean bool     `json:"windows_korean"`
}

// levels are the steps a distribution's choice goes through in the wizard,
// each holding the one before: not set up, set up, kept running, started
// at sign-in.
var levels = []string{"wiz.level.none", "wiz.level.setup", "wiz.level.keep", "wiz.level.logon"}

// stateAt is the choices a level stands for.
func stateAt(level int) setup.State {
	return setup.State{Managed: level >= 1, Keep: level >= 2, Logon: level >= 3}
}

// wizDistro is one distribution the wizard offers.
type wizDistro struct {
	name    string
	running bool
	// cur is its level now; want what is chosen. known is false for one
	// stopped and never read: what is set up in it shows once it runs.
	cur, want int
	known     bool
}

// applyStep is one change the wizard makes, as its last page shows it.
type applyStep struct {
	what  string
	state string // "wait", "run", "done", "failed"
	err   string
	// note is what a step done says besides: the hook in, but not read by
	// Windows yet.
	note string
}

// wizApplyMsg is how one change went; wizDoneMsg ends them.
type (
	wizApplyMsg struct {
		i   int
		err error
		// note is the step's note, done as it is.
		note string
	}
	wizReadMsg struct {
		st   api.Status
		hook *api.Hook
		set  Settings
		err  error
	}
)

// wizard is the setup wizard's state.
type wizard struct {
	ctx           context.Context
	c             api.Client
	th            ui.Theme
	width, height int
	page          int
	// first is set on the first start: leaving writes the defaults.
	first bool
	// read is set once the settings and distributions are read.
	read bool
	err  error
	// The choices, as indexes into their lists.
	lang, theme, mode int
	distros           []wizDistro
	sel               int
	// hookIn is set when the hook is there; install when it is chosen.
	hookIn, install bool
	// steps are the changes being made, on the last page.
	steps []applyStep
	done  bool
	// finished is set when the person went through to the end.
	finished bool
	spin     int
	// systemKorean says Windows' language, for "auto".
	systemKorean bool
	// startDark is the theme the terminal asked for, for "auto".
	startDark bool
}

// The choices, as the settings name them.
var (
	wizLangs  = []string{"auto", "en", "ko"}
	wizThemes = []string{"auto", "dark", "light"}
	wizModes  = []string{"tui", "cli"}
)

// indexOf is v's index in vs, 0 when it is not there.
func indexOf(vs []string, v string) int {
	for i, x := range vs {
		if x == v {
			return i
		}
	}
	return 0
}

// newWizard is the wizard before anything is read.
func newWizard(ctx context.Context, c api.Client, dark, first bool) wizard {
	return wizard{ctx: ctx, c: c, th: ui.NewTheme(dark), startDark: dark, width: 100, height: 30, first: first, install: true}
}

// Init reads the settings and the distributions.
func (w wizard) Init() tea.Cmd { return w.readAll() }

// readAll reads what the wizard starts from.
func (w wizard) readAll() tea.Cmd {
	c, ctx := w.c, w.ctx
	return guard(func() tea.Msg {
		var set Settings
		if err := c.Settings(ctx, &set); err != nil {
			return wizReadMsg{err: err}
		}
		st, err := c.Status(ctx)
		if err != nil {
			return wizReadMsg{err: err}
		}
		var hook *api.Hook
		if h, err := c.Hook(ctx); err == nil {
			hook = &h
		}
		return wizReadMsg{st: st, hook: hook, set: set}
	})
}

// Update handles a message.
func (w wizard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w.width, w.height = msg.Width, msg.Height
	case wizReadMsg:
		w.read, w.err = true, msg.err
		if msg.err == nil {
			w.lang, w.theme, w.mode = indexOf(wizLangs, msg.set.Language), indexOf(wizThemes, msg.set.Theme), indexOf(wizModes, msg.set.Mode)
			w.systemKorean = msg.set.WindowsKorean
			for _, d := range msg.st.List {
				if d.Kind != "set-up" && d.Kind != "stop-only" && d.Kind != "not-read" {
					continue
				}
				cur, known := levelIn(d.Probe, d.AtLogon)
				w.distros = append(w.distros, wizDistro{name: d.Name, running: d.Running, cur: cur, want: cur, known: known})
			}
			if msg.hook != nil {
				w.hookIn, w.install = msg.hook.Last, !msg.hook.Last
			}
			w.preview()
		}
	case wizApplyMsg:
		if msg.err != nil {
			w.steps[msg.i].state, w.steps[msg.i].err = "failed", errWords(msg.err)
		} else {
			w.steps[msg.i].state, w.steps[msg.i].note = "done", msg.note
		}
		if msg.i+1 < len(w.steps) {
			w.steps[msg.i+1].state = "run"
			return w, w.applyStep(msg.i + 1)
		}
		w.done = true
	case tickMsg:
		w.spin++
		if w.page == pApply && !w.done {
			return w, tick()
		}
	case tea.KeyPressMsg:
		return w.key(msg.String())
	}
	return w, nil
}

// preview shows the language and the colors chosen at once.
func (w *wizard) preview() {
	switch wizLangs[w.lang] {
	case "en":
		i18n.Set(i18n.English)
	case "ko":
		i18n.Set(i18n.Korean)
	default:
		i18n.Set(i18n.Choose("auto", w.systemKorean))
	}
	switch wizThemes[w.theme] {
	case "dark":
		w.th = ui.NewTheme(true)
	case "light":
		w.th = ui.NewTheme(false)
	default:
		w.th = ui.NewTheme(w.startDark)
	}
}

// key handles a key on the page open.
func (w wizard) key(key string) (tea.Model, tea.Cmd) {
	if key == "ctrl+c" {
		return w, tea.Quit
	}
	if w.page == pApply {
		if w.done && (key == "enter" || key == "esc" || key == "q") {
			w.finished = true
			return w, tea.Quit
		}
		return w, nil
	}
	if !w.read || w.err != nil {
		if key == "esc" || key == "q" || key == "enter" {
			return w, tea.Quit
		}
		return w, nil
	}
	choose := func(n *int, count, d int) { *n = (*n + d + count) % count }
	switch key {
	case "esc", "q":
		return w, tea.Quit
	case "enter", "right", "l", "tab":
		if w.page == pConfirm {
			return w.apply()
		}
		w.page++
		return w, nil
	case "left", "h", "backspace", "shift+tab":
		if w.page > pWelcome {
			w.page--
		}
		return w, nil
	case "up", "k", "down", "j":
		d := 1
		if key == "up" || key == "k" {
			d = -1
		}
		switch w.page {
		case pLanguage:
			choose(&w.lang, len(wizLangs), d)
			w.preview()
		case pColors:
			choose(&w.theme, len(wizThemes), d)
			w.preview()
		case pMode:
			choose(&w.mode, len(wizModes), d)
		case pDistros:
			if len(w.distros) > 0 {
				w.sel = min(max(0, w.sel+d), len(w.distros)-1)
			}
		case pHook:
			w.install = !w.install
		}
		return w, nil
	case "space":
		if w.page == pDistros && len(w.distros) > 0 {
			d := &w.distros[w.sel]
			d.want = (d.want + 1) % len(levels)
		}
		if w.page == pHook {
			w.install = !w.install
		}
	}
	return w, nil
}

// changes are what the confirm page lists and the last page makes.
func (w wizard) changes() []applyStep {
	var out []applyStep
	out = append(out, applyStep{what: i18n.T("wiz.apply.settings", w.langName(w.lang), w.themeName(w.theme), w.modeName(w.mode)), state: "wait"})
	for _, d := range w.distros {
		if d.want != d.cur {
			out = append(out, applyStep{what: d.name + ": " + i18n.T(levels[d.want]), state: "wait"})
		}
	}
	if w.install && !w.hookIn {
		out = append(out, applyStep{what: i18n.T("wiz.apply.hook"), state: "wait"})
	}
	return out
}

// apply makes the changes, one after another.
func (w wizard) apply() (tea.Model, tea.Cmd) {
	w.page, w.steps = pApply, w.changes()
	if len(w.steps) == 0 {
		w.done = true
		return w, nil
	}
	w.steps[0].state = "run"
	return w, tea.Batch(w.applyStep(0), tick())
}

// applyStep makes change i, through the commands.
func (w wizard) applyStep(i int) tea.Cmd {
	c, ctx := w.c, w.ctx
	lang, theme, mode := wizLangs[w.lang], wizThemes[w.theme], wizModes[w.mode]
	var changed []wizDistro
	for _, d := range w.distros {
		if d.want != d.cur {
			changed = append(changed, d)
		}
	}
	return guard(func() tea.Msg {
		var err error
		switch {
		case i == 0:
			err = c.Do(ctx, "setup", "--language", lang, "--theme", theme, "--mode", mode)
		case i-1 < len(changed):
			d := changed[i-1]
			s := stateAt(d.want)
			_, err = c.Set(ctx, d.name, &s.Managed, &s.Keep, &s.Logon, nil)
		default:
			err = c.Do(ctx, "hook", "install", "--yes")
			// Exit 3: installed, but Windows has not read it yet. The hook is
			// in, and setup run again offers no hook step: done, with a note
			// in the command's own words. Do gives its *CommandError as is.
			if ce, ok := err.(*api.CommandError); ok && ce.Attention() {
				// Without the program's name, which a command prints first
				// when no terminal reads it: the step names the hook.
				return wizApplyMsg{i: i, note: strings.TrimPrefix(ce.Said, "wsl-spawner: ")}
			}
		}
		return wizApplyMsg{i: i, err: err}
	})
}

// The names of the choices, in the wizard's own words.
func (w wizard) langName(i int) string {
	return i18n.T("wiz.lang." + wizLangs[i])
}
func (w wizard) themeName(i int) string { return i18n.T("wiz.theme." + wizThemes[i]) }
func (w wizard) modeName(i int) string  { return i18n.T("wiz.mode." + wizModes[i]) }

// View draws the page open.
func (w wizard) View() tea.View {
	v := tea.NewView(ui.Marks(w.render()))
	v.AltScreen = true
	v.WindowTitle = "wsl-spawner setup"
	return v
}

// render draws the page as text.
func (w wizard) render() string {
	m := model{th: w.th, width: w.width, height: w.height}
	if w.width < 40 || w.height < 12 {
		return m.frame([]string{ui.Fg(w.th.Muted).Render(i18n.T("small"))})
	}
	t := w.th
	head := " " + t.Chip("wsl-spawner", 0) + " " + ui.Fg(t.Ink).Render(version.Short()) + ui.Fg(t.Muted).Render("  ·  "+i18n.T("wiz.title"))
	lines := []string{head, "", " " + w.progress(), ""}
	var body []string
	keys := [][2]string{{"⏎", i18n.T("wiz.key.next")}, {"←", i18n.T("wiz.key.back")}, {"esc", i18n.T("wiz.key.quit")}}
	switch {
	case !w.read:
		body = []string{"  " + ui.Fg(t.Muted).Render(i18n.T("loading"))}
	case w.err != nil:
		body = []string{"  " + ui.Fg(t.Cat[ui.CatCut]).Render("✗ "+errWords(w.err))}
		keys = [][2]string{{"esc", i18n.T("wiz.key.quit")}}
	default:
		body, keys = w.pageBody(keys)
	}
	lines = append(lines, body...)
	for len(lines) < w.height-1 {
		lines = append(lines, "")
	}
	lines = append(lines[:w.height-1], m.keyBar(keys))
	return m.frame(lines)
}

// progress is where the wizard is: a dot for each page, the one open in
// the tool's color, then its number and name.
func (w wizard) progress() string {
	t := w.th
	var b strings.Builder
	for i := 0; i < pages; i++ {
		switch {
		case i == min(w.page, pages-1):
			b.WriteString(ui.Fg(t.Mark).Render("●"))
		case i < w.page:
			b.WriteString(ui.Fg(t.Muted).Render("●"))
		default:
			b.WriteString(ui.Fg(t.Faint).Render("○"))
		}
		b.WriteString(" ")
	}
	n := min(w.page, pages-1) + 1
	return b.String() + ui.Fg(t.Muted).Render(i18n.T("wiz.step", n, pages)+" · ") + lipgloss.NewStyle().Bold(true).Foreground(t.Ink).Render(i18n.T(fmt.Sprintf("wiz.page.%d", min(w.page, pages-1))))
}

// option is one choice in a list: marked when chosen, in the tool's color
// with the selection bar.
func (w wizard) option(chosen bool, label, note string) string {
	t := w.th
	if chosen {
		return "  " + ui.Fg(t.Mark).Render("▌ ● "+label) + ui.Fg(t.Muted).Render(note)
	}
	return "    " + ui.Fg(t.Faint).Render("○ ") + ui.Fg(t.Ink).Render(label) + ui.Fg(t.Muted).Render(note)
}

// pageBody is the open page's lines, and its keys.
func (w wizard) pageBody(keys [][2]string) ([]string, [][2]string) {
	t := w.th
	m := model{th: t, width: w.width, height: w.height}
	para := func(key string, args ...any) []string {
		return m.wrapHang("  ", "  ", i18n.T(key, args...), ui.Fg(t.Ink))
	}
	choose := [2]string{"↑↓", i18n.T("wiz.key.choose")}
	switch w.page {
	case pWelcome:
		body := append(m.logoBlockIndented(), "")
		body = append(body, para("wiz.welcome.1")...)
		body = append(body, "")
		body = append(body, para("wiz.welcome.2")...)
		return body, [][2]string{{"⏎", i18n.T("wiz.key.start")}, {"esc", i18n.T("wiz.key.quit")}}
	case pLanguage:
		body := para("wiz.lang.q")
		body = append(body, "")
		for i := range wizLangs {
			note := ""
			if wizLangs[i] == "auto" {
				note = "  " + i18n.T("wiz.lang.auto.note")
			}
			body = append(body, w.option(i == w.lang, w.langName(i), note))
		}
		return body, append([][2]string{choose}, keys...)
	case pColors:
		body := para("wiz.theme.q")
		body = append(body, "")
		for i := range wizThemes {
			body = append(body, w.option(i == w.theme, w.themeName(i), ""))
		}
		// The colors as they are now, on a row of the screen.
		body = append(body, "", "  "+ui.Fg(t.Muted).Render(i18n.T("wiz.preview")),
			"    "+t.Dot(ui.CatRunning)+" "+ui.Fg(t.Cat[ui.CatRunning]).Render(i18n.T("state.running"))+"   "+
				ui.Fg(t.Cat[ui.CatClean]).Render(i18n.T("end.clean"))+"   "+ui.Fg(t.Cat[ui.CatCut]).Render(i18n.T("end.cut"))+"   "+
				ui.Fg(t.Cat[ui.CatWarn]).Render("! "+i18n.T("header.nohook")))
		return body, append([][2]string{choose}, keys...)
	case pMode:
		body := para("wiz.mode.q")
		body = append(body, "")
		for i := range wizModes {
			body = append(body, w.option(i == w.mode, w.modeName(i), "  "+i18n.T("wiz.mode."+wizModes[i]+".note")))
		}
		return body, append([][2]string{choose}, keys...)
	case pDistros:
		body := para("wiz.distros.q")
		body = append(body, "")
		if len(w.distros) == 0 {
			body = append(body, "  "+ui.Fg(t.Muted).Render(i18n.T("wiz.distros.none")))
			return body, keys
		}
		nameW := 0
		for _, d := range w.distros {
			nameW = max(nameW, lipgloss.Width(d.name))
		}
		for i, d := range w.distros {
			// The level as the main screen's table draws it.
			dots, words := t.Level(d.want), ui.Fg(t.Ink).Render(i18n.T(levels[d.want]))
			if !d.known && d.want == d.cur {
				// Not read and not changed: what is in it is not guessed.
				dots, words = ui.Fg(t.Faint).Render("–  "), ui.Fg(t.Muted).Render(i18n.T("choices.unknown"))
			}
			note := ""
			if !d.running && d.want != d.cur && (d.want == 0) != (d.cur == 0) {
				note = ui.Fg(t.Muted).Render("  " + i18n.T("wiz.distros.starts"))
			}
			row := ui.Fit(d.name, nameW) + "  " + dots + "  " + words + note
			if i == w.sel {
				body = append(body, "  "+ui.Fg(t.Mark).Render("▌ ")+row)
			} else {
				body = append(body, "    "+row)
			}
		}
		return body, append([][2]string{{"↑↓", i18n.T("wiz.key.choose")}, {"space", i18n.T("wiz.key.level")}}, keys...)
	case pHook:
		body := para("wiz.hook.q")
		body = append(body, "")
		if w.hookIn {
			body = append(body, "  "+ui.Fg(t.Cat[ui.CatClean]).Render("✓ "+i18n.T("wiz.hook.in")))
			return body, keys
		}
		body = append(body, w.option(w.install, i18n.T("wiz.hook.install"), "  "+i18n.T("wiz.hook.install.note")),
			w.option(!w.install, i18n.T("wiz.hook.later"), "  "+i18n.T("wiz.hook.later.note")))
		return body, append([][2]string{choose}, keys...)
	case pConfirm:
		body := para("wiz.confirm.q")
		body = append(body, "")
		for _, s := range w.changes() {
			body = append(body, "  "+ui.Fg(t.Mark).Render("•")+" "+s.what)
		}
		return body, [][2]string{{"⏎", i18n.T("wiz.key.apply")}, {"←", i18n.T("wiz.key.back")}, {"esc", i18n.T("wiz.key.quit")}}
	}
	// The last page: each change as it is made.
	var body []string
	for _, s := range w.steps {
		var mark string
		switch s.state {
		case "wait":
			mark = ui.Fg(t.Faint).Render("·")
		case "run":
			mark = ui.Fg(t.Cat[ui.CatStarting]).Render(spinner[w.spin%len(spinner)])
		case "done":
			mark = ui.Fg(t.Cat[ui.CatClean]).Render("✓")
		default:
			mark = ui.Fg(t.Cat[ui.CatCut]).Render("✗")
		}
		body = append(body, "  "+mark+" "+s.what)
		if s.err != "" {
			body = append(body, m.wrapHang("      ", "      ", s.err, ui.Fg(t.Cat[ui.CatCut]))...)
		}
		// A done step's note is no failure: muted, under it.
		if s.note != "" {
			body = append(body, m.wrapHang("      ", "      ", s.note, ui.Fg(t.Muted))...)
		}
	}
	if w.done {
		body = append(body, "", "  "+ui.Fg(t.Muted).Render(i18n.T("wiz.done")))
		return body, [][2]string{{"⏎", i18n.T("wiz.key.finish")}}
	}
	return body, nil
}

// logoBlockIndented is the logo as the welcome page draws it, at the left.
func (m model) logoBlockIndented() []string {
	out := m.logoBlock()
	for i, l := range out {
		out[i] = "  " + l
	}
	return out
}

// RunSetup runs the wizard on c. first is set for the first start, when no
// settings file is there yet: leaving it then writes the defaults, so it
// does not open again. It reports whether the person went to its end.
func RunSetup(ctx context.Context, c api.Client, o Options, first bool) (bool, error) {
	dark := o.Theme != "light"
	if o.Theme == "" || o.Theme == "auto" {
		dark = darkTerminal()
	}
	w := newWizard(ctx, c, dark, first)
	final, err := tea.NewProgram(w, tea.WithContext(ctx)).Run()
	fw, _ := final.(wizard)
	if err != nil {
		return false, err
	}
	if first && !fw.finished {
		// Left on the first start: the defaults, written, end the asking.
		_ = c.Do(ctx, "setup", "--mode", "tui")
	}
	return fw.finished, nil
}

// SetupSnapshot is one frame of the wizard, for pictures and tests, after
// the keys; changes the keys make are made.
func SetupSnapshot(c api.Client, width, height int, dark bool, keys []string) string {
	w := newWizard(context.Background(), c, dark, false)
	w.width, w.height = width, height
	next, _ := w.Update(w.readAll()())
	w = next.(wizard)
	for _, k := range keys {
		var cmd tea.Cmd
		key := snapshotKey(k).String()
		next, cmd = w.key(key)
		w = next.(wizard)
		for cmd != nil {
			msg := runCmd(cmd)
			if msg == nil {
				break
			}
			next, cmd = w.Update(msg)
			w = next.(wizard)
		}
	}
	// The marks swapped as View swaps them, so a picture in plain or ASCII
	// marks is what the program draws.
	return ui.Marks(w.render())
}
