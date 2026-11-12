package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// logoPixels is the logo's mark (docs/assets) as the screen draws it, 14
// square pixels across and 12 down, two to a cell: "v" the power ring, open
// at the top, in the tool's color; "k" the prompt's chevron and "u" its
// underscore, in ink; "." none. The prompt is set by hand in one-pixel
// strokes, which read as > and _ at this size where a drawn shape would
// blur; scratch/fig/make_logo.py holds it and checks it keeps off the ring.
var logoPixels = []string{
	"...vv....vv...",
	"..vvv....vvv..",
	".vvv.k....vvv.",
	".vv...k....vv.",
	".vv....k...vv.",
	".vv...k....vv.",
	".vv..k.uuu.vv.",
	".vvv......vvv.",
	"..vvv....vvv..",
	"...vvvvvvvv...",
	"....vvvvvv....",
	"..............",
}

// logoASCII is the mark for a console that draws block characters two
// cells wide, one character a cell: "#" the ring in the tool's color, the
// prompt in ink, the underscore a cell short of the ring so the two stay
// apart.
var logoASCII = []string{
	"  ###    ###  ",
	` ### \\   ### `,
	" ##   />   ## ",
	" ### / __ ### ",
	"  ##########  ",
	"    ######    ",
}

// Logo is the mark as the screen draws it: half blocks, two square pixels
// to a cell, the upper one in the character's color and the lower one in
// its background where both are drawn. Six rows of fourteen cells. A
// console that draws block characters two cells wide gets logoASCII, of
// the same size.
func (t Theme) Logo() []string {
	// paint is a pixel's color, nil for none.
	paint := func(p byte) lipgloss.Style {
		switch p {
		case 'v':
			return Fg(t.Mark)
		case 'k', 'u':
			return Fg(t.Ink)
		}
		return lipgloss.NewStyle()
	}
	if set == MarksASCII {
		var out []string
		for _, l := range logoASCII {
			var b strings.Builder
			for _, c := range l {
				switch c {
				case '#':
					b.WriteString(paint('v').Render("#"))
				case ' ':
					b.WriteString(" ")
				default:
					b.WriteString(paint('k').Render(string(c)))
				}
			}
			out = append(out, b.String())
		}
		return out
	}
	// prompt is whether a pixel is the prompt's.
	prompt := func(p byte) bool { return p == 'k' || p == 'u' }
	var out []string
	for r := 0; r+1 < len(logoPixels); r += 2 {
		top, bottom := logoPixels[r], logoPixels[r+1]
		var b strings.Builder
		for x := 0; x < len(top); x++ {
			hi, lo := top[x], bottom[x]
			switch {
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
				if prompt(lo) {
					bg = t.Ink
				}
				b.WriteString(paint(hi).Background(bg).Render("▀"))
			}
		}
		out = append(out, b.String())
	}
	return out
}

// Chip is the name in the header after the mark's prompt, ❯ in the tool's
// color; while something runs, a spinner stands in for it, turned turn
// steps.
func (t Theme) Chip(s string, turn int) string {
	mark := "❯"
	if turn > 0 {
		mark = spinner[turn%len(spinner)]
	}
	return Fg(t.Mark).Render(mark) + " " + lipgloss.NewStyle().Foreground(t.Ink).Bold(true).Render(s)
}

// spinner is the frames of the chip while something runs.
var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
