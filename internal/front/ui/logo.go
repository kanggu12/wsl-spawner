package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// logoPixels is the logo's mark (docs/assets) as the screen draws it, 14
// square pixels across and 12 down, two to a cell: "v" the power ring, open
// at the top, in the tool's color; "k" the play triangle in ink; "." none.
// scratch/fig/make_logo.py prints it from the same shapes the SVG has.
var logoPixels = []string{
	"...vv....vv...",
	"..vvv....vvv..",
	".vvv.k....vvv.",
	".vv..kkk...vv.",
	".vv..kkkk..vv.",
	".vv..kkkk..vv.",
	".vv..kkk...vv.",
	".vvv.k....vvv.",
	"..vvv....vvv..",
	"...vvvvvvvv...",
	"....vvvvvv....",
	"..............",
}

// Logo is the mark as the screen draws it: half blocks, two square pixels
// to a cell, the upper one in the character's color and the lower one in
// its background where both are drawn. Six rows of fourteen cells. A
// console that draws block characters two cells wide gets the same shape in
// ASCII, one character a cell.
func (t Theme) Logo() []string {
	// paint is a pixel's color, nil for none.
	paint := func(p byte) lipgloss.Style {
		switch p {
		case 'v':
			return Fg(t.Mark)
		case 'k':
			return Fg(t.Ink)
		}
		return lipgloss.NewStyle()
	}
	var out []string
	for r := 0; r+1 < len(logoPixels); r += 2 {
		top, bottom := logoPixels[r], logoPixels[r+1]
		var b strings.Builder
		for x := 0; x < len(top); x++ {
			hi, lo := top[x], bottom[x]
			switch {
			case set == MarksASCII && (hi == 'k' || lo == 'k'):
				b.WriteString(paint('k').Render(">"))
			case set == MarksASCII && (hi == 'v' || lo == 'v'):
				b.WriteString(paint('v').Render("#"))
			case hi == '.' && lo == '.':
				b.WriteString(" ")
			case lo == '.':
				b.WriteString(paint(hi).Render("▀"))
			case hi == '.':
				b.WriteString(paint(lo).Render("▄"))
			case hi == lo:
				b.WriteString(paint(hi).Render("█"))
			default:
				// Two colors in one cell: the upper as the character, the
				// lower as its background.
				bg := t.Mark
				if lo == 'k' {
					bg = t.Ink
				}
				b.WriteString(paint(hi).Background(bg).Render("▀"))
			}
		}
		out = append(out, b.String())
	}
	return out
}

// Chip is the name in the header after the mark's play triangle, in the
// tool's color; while something runs, a spinner stands in for it, turned
// turn steps.
func (t Theme) Chip(s string, turn int) string {
	mark := "▶"
	if turn > 0 {
		mark = spinner[turn%len(spinner)]
	}
	return Fg(t.Mark).Render(mark) + " " + lipgloss.NewStyle().Foreground(t.Ink).Bold(true).Render(s)
}

// spinner is the frames of the chip while something runs.
var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
