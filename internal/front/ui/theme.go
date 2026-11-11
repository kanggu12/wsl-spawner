// Package ui holds what the screen and the command line share: the colors,
// keycaps, bars and fitting text to a width.
package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Category groups distributions, containers and steps by what they mean to
// the person looking, which is what the color says. Surfaces stay neutral:
// color is only ever one of these.
type Category int

const (
	// CatRunning: running.
	CatRunning Category = iota
	// CatStopped: stopped, nothing to say about it.
	CatStopped
	// CatClean: stopped by wsl-spawner, every service given its time; the
	// tool's own color.
	CatClean
	// CatStarting: starting, or coming back.
	CatStarting
	// CatCut: ended without its time, or a step that failed.
	CatCut
	// CatWarn: worth a second look.
	CatWarn
	// CatAside: left to WSL or to another tool.
	CatAside
)

// Theme is one set of colors, for a dark or a light terminal.
type Theme struct {
	Dark bool
	// Ink and Muted are the two text levels; Rule draws borders; Faint is
	// for separators between pieces of text, quieter than Muted but still
	// read on either background.
	Ink, Muted, Rule, Faint color.Color
	// Mark is the tool's own color: the logo, chosen items, a clean stop.
	Mark color.Color
	// Cat holds the color of each category.
	Cat map[Category]color.Color
	// Selected is the background of the selected row; Key the keycaps';
	// Track the empty part of a bar.
	Selected, Key, Track color.Color
}

// NewTheme returns the theme for a dark or a light background. Every color
// words are written in reads at a contrast of 4.5:1 or more on its
// background and on the selected row's; Faint, which only draws separators
// and dashes, at 3:1 or more (theme_test.go measures them).
func NewTheme(dark bool) Theme {
	ld := lipgloss.LightDark(dark)
	mark := ld(lipgloss.Color("#6A4BE0"), lipgloss.Color("#A18CFF"))
	return Theme{
		Dark:  dark,
		Ink:   ld(lipgloss.Color("#16161D"), lipgloss.Color("#ECEBF2")),
		Muted: ld(lipgloss.Color("#5E5C6B"), lipgloss.Color("#A2A0B0")),
		Rule:  ld(lipgloss.Color("#DAD9E2"), lipgloss.Color("#302F3A")),
		Faint: ld(lipgloss.Color("#868493"), lipgloss.Color("#6E6C7C")),
		Mark:  mark,
		Cat: map[Category]color.Color{
			CatRunning:  ld(lipgloss.Color("#197B43"), lipgloss.Color("#5CD68A")),
			CatStopped:  ld(lipgloss.Color("#5E5C6B"), lipgloss.Color("#A2A0B0")),
			CatClean:    mark,
			CatStarting: ld(lipgloss.Color("#2459D6"), lipgloss.Color("#6FA4FF")),
			CatCut:      ld(lipgloss.Color("#C4313B"), lipgloss.Color("#FF6B6B")),
			CatWarn:     ld(lipgloss.Color("#A85207"), lipgloss.Color("#F2B24C")),
			CatAside:    ld(lipgloss.Color("#6A6974"), lipgloss.Color("#8A8895")),
		},
		Selected: ld(lipgloss.Color("#F0EDFB"), lipgloss.Color("#211F2C")),
		Key:      ld(lipgloss.Color("#ECEBF1"), lipgloss.Color("#26252F")),
		Track:    ld(lipgloss.Color("#E7E6ED"), lipgloss.Color("#292833")),
	}
}

// Fg is a style with a foreground color.
func Fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// Dot is the colored block that leads a row: one cell wide in every
// terminal, unlike "●", which East Asian settings draw two cells wide.
func (t Theme) Dot(c Category) string { return Fg(t.Cat[c]).Render(Block) }

// Block is the identity's unit: a small square, one cell wide everywhere.
const Block = "▪"

// KeyCap draws a key as a small raised cap followed by what it does.
func (t Theme) KeyCap(key, label string) string {
	// ⏎ is a word where the console has no font for it.
	k := lipgloss.NewStyle().Background(t.Key).Foreground(t.Ink).Bold(true).Padding(0, 1).Render(Words(key))
	return k + " " + Fg(t.Muted).Render(label)
}

// Level draws a distribution's level as three squares, the way setup
// chooses it: set up, kept running, started at sign-in, each holding the
// one before. Those reached are in the tool's color, the rest the track.
func (t Theme) Level(n int) string {
	n = max(0, min(3, n))
	return Fg(t.Mark).Render(strings.Repeat(Block, n)) + Fg(t.Track).Render(strings.Repeat(Block, 3-n))
}

// Bar draws parts of a whole as one bar w cells wide: each part takes cells
// in proportion to its value, in its color, and the rest is the track. A
// part above zero always gets at least one cell, so it stays visible.
func (t Theme) Bar(w int, total int64, parts ...Part) string {
	if w <= 0 {
		return ""
	}
	cells := make([]int, len(parts))
	used := 0
	for i, p := range parts {
		if p.Value <= 0 || total <= 0 {
			continue
		}
		cells[i] = int(float64(p.Value) / float64(total) * float64(w))
		if cells[i] == 0 {
			cells[i] = 1
		}
		used += cells[i]
	}
	// Parts that make up the whole fill the bar: rounding down must not leave
	// a cell of track that reads as a part of its own.
	var sum int64
	for _, p := range parts {
		sum += max(0, p.Value)
	}
	if sum >= total && used < w && len(parts) > 0 {
		cells[len(parts)-1] += w - used
		used = w
	}
	// Rounding can overshoot; take the excess from the largest part.
	for used > w {
		big := 0
		for i := range cells {
			if cells[i] > cells[big] {
				big = i
			}
		}
		cells[big]--
		used--
	}
	// Each cell is a block; the empty part is the track.
	var b strings.Builder
	for i, p := range parts {
		b.WriteString(Fg(p.Color).Render(strings.Repeat(Block, cells[i])))
	}
	b.WriteString(Fg(t.Track).Render(strings.Repeat(Block, w-used)))
	return b.String()
}

// Part is one segment of a Bar.
type Part struct {
	Value int64
	Color color.Color
}

// Fit pads or cuts s to exactly w cells, with "…" where it was cut, right
// after the last word kept: "Docker Desktop…", never "Docker Desktop …".
func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		cut := ansi.Truncate(s, w-1, "")
		// A space the cut ends on goes, also where the codes that end a
		// color come after it, so the "…" stands right after the word.
		for strings.HasSuffix(ansi.Strip(cut), " ") {
			cut = ansi.Truncate(cut, lipgloss.Width(cut)-1, "")
		}
		s = cut + "…"
	}
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// FitRight is Fit with the text against the right edge.
func FitRight(s string, w int) string {
	if lipgloss.Width(s) > w {
		return Fit(s, w)
	}
	return strings.Repeat(" ", w-lipgloss.Width(s)) + s
}

// Middle shortens a long text in its middle, keeping its start and its
// end, which say the most: C:\Users\…\wsl-spawner\config.toml.
func Middle(s string, w int) string {
	if lipgloss.Width(s) <= w || w < 8 {
		return Fit(s, w)
	}
	keep := w - 1
	head := keep / 3
	tail := keep - head
	// Cut by cells, not letters: a Korean letter takes two.
	return Fit(ansi.Truncate(s, head, "")+"…"+ansi.TruncateLeft(s, lipgloss.Width(s)-tail, ""), w)
}

// Highlight paints a whole row's background, w cells wide. Styled parts of
// the row end with a reset that would clear the background too, so the
// background is set again after every reset.
func Highlight(row string, bg color.Color, w int) string {
	// The escape sequence that turns this background on, taken from a
	// rendered space.
	on := lipgloss.NewStyle().Background(bg).Render(" ")
	on = on[:strings.Index(on, " ")]
	row = strings.ReplaceAll(Fit(row, w), "\x1b[m", "\x1b[m"+on)
	row = strings.ReplaceAll(row, "\x1b[0m", "\x1b[0m"+on)
	return on + row + "\x1b[m"
}

// ShellArg is a name as it goes into a command to type: as it is when it
// is one word, in quotes when it holds spaces or anything a shell reads,
// in PowerShell and in Linux shells alike.
func ShellArg(s string) string {
	plain := s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-/:", r))
	}) < 0
	switch {
	case plain:
		return s
	case !strings.ContainsAny(s, "'"):
		// Single quotes take everything in them as it is, in both shells.
		return "'" + s + "'"
	}
	// A name with a single quote: double quotes, which it does not end.
	return `"` + s + `"`
}
