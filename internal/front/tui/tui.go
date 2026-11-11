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
	m := newModel(context.Background(), c, dark)
	m.width, m.height, m.now = w, h, now
	// Load as Init would, but in this goroutine.
	next, _ := m.Update(m.load()())
	m = next.(model)
	for _, k := range keys {
		var cmd tea.Cmd
		next, cmd = m.Update(snapshotKey(k))
		m = next.(model)
		m = m.settle(cmd)
	}
	return m.View().Content
}

// settle runs what a key started, as the program would, until it is done:
// the check read, a change made, a stop or start played to its end. A
// spinner's tick is not waited for.
func (m model) settle(cmd tea.Cmd) model {
	for cmd != nil {
		msg := runCmd(cmd)
		if msg == nil {
			return m
		}
		if _, ok := msg.(tickMsg); ok {
			return m.drain()
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
	for m.run != nil && !m.run.done {
		next, cmd := m.Update(<-m.run.events)
		m = next.(model)
		// The reload a run's end asks for.
		if m.run.done {
			m = m.settle(cmd)
		}
	}
	return m
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
	"enter":  {Code: tea.KeyEnter},
	"esc":    {Code: tea.KeyEscape},
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
