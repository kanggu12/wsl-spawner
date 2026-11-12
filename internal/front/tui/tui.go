package tui

import (
	"context"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kanggu12/wsl-spawner/internal/front/api"
)

// Options is how the screen starts.
type Options struct {
	// Theme is "dark", "light", or "" and "auto" to ask the terminal.
	Theme string
}

// Run shows the screen until it is closed. A bug the screen stopped on
// comes back as a *BugError.
func Run(ctx context.Context, c api.Client, o Options) error {
	dark := true
	switch o.Theme {
	case "light":
		dark = false
	case "dark":
	default:
		dark = darkTerminal()
	}
	m := newModel(ctx, c, dark)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	final, err := p.Run()
	if fm, ok := final.(model); ok && fm.bug != nil {
		return fm.bug
	}
	return err
}

// darkTerminal asks the terminal whether its background is dark; one that
// does not say is taken for dark, as most are.
func darkTerminal() bool { return lipgloss.HasDarkBackground(os.Stdin, os.Stdout) }

// Snapshot is one frame of the screen, for the README's pictures and the
// tests: the screen at w by h after loading, with keys pressed, at the
// moment now. A stop or start the keys began plays to its end, all its
// steps taken at once.
func Snapshot(c api.Client, w, h int, dark bool, keys []string, now time.Time) string {
	return SnapshotAt(c, w, h, dark, keys, now, 0)
}

// SnapshotAt is Snapshot with a stop the keys began played only to at
// after its start. Each distribution's steps follow one another on a clock
// of its own, as distributions stop at the same time: a step's end moves
// that clock on by the time the step says it took, and the step that would
// end after at is left running. The frame is drawn at that moment, and the
// keys after it are not pressed. 0, or an at past the stop's end, draws the
// frame Snapshot draws.
func SnapshotAt(c api.Client, w, h int, dark bool, keys []string, now time.Time, at time.Duration) string {
	m := newModel(context.Background(), c, dark)
	m.width, m.height, m.now = w, h, now
	// How far into a stop the frame is: 0 is to its end.
	m.play = &playback{to: at}
	// Load as Init would, but in this goroutine.
	next, _ := m.Update(m.load()())
	m = next.(model)
	for _, k := range keys {
		// The moment asked for has come: the keys after it wait.
		if m.play.done() {
			break
		}
		var cmd tea.Cmd
		next, cmd = m.Update(snapshotKey(k))
		m = next.(model)
		m = m.settle(cmd)
	}
	return m.View().Content
}

// playback is how far into a stop a snapshot plays it: to, and each
// distribution's clock so far, those whose step under way would end after
// to, and whether the frame's moment has come.
type playback struct {
	to     time.Duration
	run    *runState
	clocks map[string]time.Duration
	cut    map[string]bool
	frozen bool
}

// done reports whether the frame's moment has come; false without a
// playback, as for a frame drawn at a stop's end.
func (p *playback) done() bool { return p != nil && p.frozen }

// settle runs what a key started, as the program would, until it is done:
// the check read, a change made, a stop or start played to its end. A
// spinner's tick is not waited for.
func (m model) settle(cmd tea.Cmd) model {
	for cmd != nil && !m.play.done() {
		msg := runCmd(cmd)
		if msg == nil {
			return m
		}
		if _, ok := msg.(tickMsg); ok {
			return m.drain()
		}
		// A message a part-way frame does not take: the run's next one.
		var skip bool
		if m, skip = m.clock(msg); skip {
			if m.play.done() || m.run == nil || m.run.done {
				return m
			}
			cmd = m.run.next()
			continue
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(model)
	}
	return m.drain()
}

// drain plays a run's messages to its end, and reads what its end asked
// for.
func (m model) drain() model {
	for m.run != nil && !m.run.done && !m.play.done() {
		msg := <-m.run.events
		// A message a part-way frame does not take.
		var skip bool
		if m, skip = m.clock(msg); skip {
			continue
		}
		next, cmd := m.Update(msg)
		m = next.(model)
		// The reload a run's end asks for.
		if m.run.done {
			m = m.settle(cmd)
		}
	}
	return m
}

// clock keeps a snapshot's clock when its frame is drawn part way into a
// stop (play.to): it sets the time each of the stop's messages happens at,
// on its distribution's own clock, and says to skip a message that comes
// after the moment asked for. At the stop's end, the frame's moment has
// come if a distribution was still stopping then; else the stop ended
// before it, and the frame is the one Snapshot draws.
func (m model) clock(msg tea.Msg) (_ model, skip bool) {
	p := m.play
	if p == nil || p.to <= 0 || m.run == nil || m.run.done || p.frozen {
		return m, false
	}
	// A new stop: new clocks.
	if p.run != m.run {
		p.run, p.clocks, p.cut = m.run, map[string]time.Duration{}, map[string]bool{}
	}
	switch e := msg.(type) {
	case eventMsg:
		// WSL's own stop comes after every distribution's: not yet, while
		// one is still stopping.
		if e.Event == "shutdown" {
			return m, len(p.cut) > 0
		}
		// The distribution is stopping at the moment asked for: what it
		// says after is not taken.
		if p.cut[e.Distro] {
			return m, true
		}
		at := p.clocks[e.Distro]
		if e.Event == "line" && e.Kind == "done" {
			// The time the step says it took.
			took := time.Duration(e.Took * float64(time.Second))
			if at+took > p.to {
				// It is still running at the moment asked for.
				p.cut[e.Distro] = true
				return m, true
			}
			at += took
			p.clocks[e.Distro] = at
		}
		// The distribution's own time, for the step this begins or ends.
		m.now = m.run.started.Add(at)
	case stopMsg:
		if len(p.cut) > 0 {
			// The stop goes on past the moment asked for: the frame is then.
			m.now, p.frozen = m.run.started.Add(p.to), true
			return m, true
		}
		// It ended before: the frame is its end, as Snapshot draws it.
		m.now = m.run.started
	}
	return m, false
}

// runCmd runs a command, and a batch's commands, returning the first
// message that is not a tick's.
func runCmd(cmd tea.Cmd) tea.Msg {
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var first tea.Msg
		for _, c := range batch {
			if c == nil {
				continue
			}
			// Ticks wait for time to pass: in a picture, none does.
			if m := c(); m != nil {
				if _, tick := m.(tickMsg); !tick && first == nil {
					first = m
				}
			}
		}
		return first
	}
	return msg
}

// namedKeys are the keys a snapshot names in words: every key the screen
// and the wizard answer that is not a letter or a sign of its own.
var namedKeys = map[string]tea.KeyPressMsg{
	"enter": {Code: tea.KeyEnter},
	"esc":   {Code: tea.KeyEscape},
	// Ctrl+C, which a stop under way answers first with what it does.
	"ctrl+c": {Code: 'c', Mod: tea.ModCtrl},
	"down":   {Code: tea.KeyDown},
	"up":     {Code: tea.KeyUp},
	"left":   {Code: tea.KeyLeft},
	"right":  {Code: tea.KeyRight},
	"tab":    {Code: tea.KeyTab},
	"pgdown": {Code: tea.KeyPgDown},
	"pgup":   {Code: tea.KeyPgUp},
	"home":   {Code: tea.KeyHome},
	"end":    {Code: tea.KeyEnd},
	// Backspace goes back a page in the wizard, and out of a list.
	"backspace": {Code: tea.KeyBackspace},
	// Shift+Tab goes back a page in the wizard.
	"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
	// Space is a sign the terminal sends as text too.
	"space": {Code: ' ', Text: " "},
}

// snapshotKey is the key event for a key's name: a named key, or the one
// letter or sign typed. A word it does not know presses nothing, rather
// than its first letter: "lefft" is not l, which turns a page.
func snapshotKey(k string) tea.KeyPressMsg {
	if msg, ok := namedKeys[k]; ok {
		return msg
	}
	// One character: the key that types it.
	if r := []rune(k); len(r) == 1 {
		return tea.KeyPressMsg{Code: r[0], Text: k}
	}
	// A word no key is named: a key event no screen answers.
	return tea.KeyPressMsg{Code: tea.KeyExtended, Text: "unknown key " + k}
}

// ParseKeys splits a snapshot's keys ("down s enter").
func ParseKeys(s string) []string { return strings.Fields(s) }
