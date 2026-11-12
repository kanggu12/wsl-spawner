package ui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFitCutsRightAfterTheLastWordKept(t *testing.T) {
	// "Docker Desktop…", never "Docker Desktop …".
	if got := Fit("Docker Desktop data", 15); got != "Docker Desktop…" {
		t.Fatalf("%q", got)
	}
	if got := Fit("Debian", 8); got != "Debian  " {
		t.Fatalf("%q", got)
	}
	// In color too: the codes that end the color come after the space the
	// cut ends on, which goes all the same.
	colored := Fg(lipgloss.Color("#A2A0B0")).Render("Docker Desktop data")
	if got := Fit(colored, 16); ansi.Strip(got) != "Docker Desktop… " || lipgloss.Width(got) != 16 {
		t.Fatalf("%q", got)
	}
}

func TestMiddleCountsCellsNotLetters(t *testing.T) {
	// A Korean sentence around a path: two cells a letter, the end kept,
	// the line exactly as wide as asked.
	s := "⏎ 누르면 C:\\Users\\ann\\AppData\\Roaming\\wsl-spawner\\config.toml에 저장"
	for _, w := range []int{20, 40, 58} {
		got := Middle(s, w)
		if lipgloss.Width(got) != w {
			t.Errorf("%d: %d cells: %q", w, lipgloss.Width(got), got)
		}
		if w >= 40 && got[len(got)-len("에 저장"):] != "에 저장" {
			t.Errorf("%d: lost its end: %q", w, got)
		}
	}
}

func TestShellArgQuotesANameWithSpaces(t *testing.T) {
	// As PowerShell and a Linux shell both take it.
	for name, want := range map[string]string{
		"Ubuntu-24.04":        "Ubuntu-24.04",
		"Docker Desktop data": "'Docker Desktop data'",
		"pre-stop.d/10-save":  "pre-stop.d/10-save",
		"a$b":                 "'a$b'",
		"Ann's server":        `"Ann's server"`,
	} {
		if got := ShellArg(name); got != want {
			t.Errorf("%q: %s", name, got)
		}
	}
}

func TestPlainMarksKeepEachMarksWidth(t *testing.T) {
	// A screen is laid out with the marks, then swapped: each stand-in
	// takes the cells its mark took, and ⏎ becomes a word only where the
	// words are not measured yet.
	t.Cleanup(func() { SetMarks(MarksUnicode) })
	lackingMarks := "✓✗↻❯⏎⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	wideMarks := "▪·…→←↑↓–—│╰●•›▌○"
	if Marks(lackingMarks+wideMarks) != lackingMarks+wideMarks || Words(lackingMarks) != lackingMarks {
		t.Fatal("marks swapped with the set left as it is")
	}
	for _, c := range []struct {
		set     MarkSet
		swapped string
		kept    string
	}{
		{MarksPlain, lackingMarks, wideMarks},
		{MarksASCII, lackingMarks + wideMarks, ""},
	} {
		SetMarks(c.set)
		for _, m := range c.swapped {
			s := string(m)
			if got := Marks(s); lipgloss.Width(got) != lipgloss.Width(s) || len(got) != 1 || got[0] > 0x7e {
				t.Errorf("set %d: %s stands as %q", c.set, s, got)
			}
		}
		for _, m := range c.kept {
			if got := Marks(string(m)); got != string(m) {
				t.Errorf("set %d: %c swapped for %q", c.set, m, got)
			}
		}
		if got := Words("⏎ writes them to"); got != "Enter writes them to" {
			t.Errorf("set %d: %q", c.set, got)
		}
		// The words themselves, Korean included, pass as they are.
		if got := Marks("Ubuntu-24.04 정상 종료"); got != "Ubuntu-24.04 정상 종료" {
			t.Errorf("set %d: %q", c.set, got)
		}
	}
}

// luminance is a color's relative luminance, as WCAG defines it.
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		x := float64(v) / 0xffff
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrast is the contrast ratio of two colors.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

func TestEveryColorReadsOnItsBackground(t *testing.T) {
	// The terminals' own backgrounds the screen is drawn on, and the
	// selected row's, which words are on too.
	backgrounds := map[bool][]color.Color{
		true:  {lipgloss.Color("#161719"), lipgloss.Color("#0D1117"), lipgloss.Color("#000000")},
		false: {lipgloss.Color("#FFFFFF"), lipgloss.Color("#FAFAFA")},
	}
	for _, dark := range []bool{true, false} {
		th := NewTheme(dark)
		bgs := append(backgrounds[dark], th.Selected)
		words := map[string]color.Color{"Ink": th.Ink, "Muted": th.Muted, "Mark": th.Mark}
		for c, col := range th.Cat {
			words[fmt.Sprintf("Cat %d", c)] = col
		}
		for name, col := range words {
			for _, bg := range bgs {
				if r := contrast(col, bg); r < 4.5 {
					t.Errorf("dark %v: %s reads at %.2f:1 on %v", dark, name, r, bg)
				}
			}
		}
		for _, bg := range bgs {
			if r := contrast(th.Faint, bg); r < 3 {
				t.Errorf("dark %v: Faint at %.2f:1 on %v", dark, r, bg)
			}
		}
	}
}

func TestASCIIMarksKeepASeparatorAndTheMarkApart(t *testing.T) {
	// In ASCII a dot between words is a bar, not a full stop; a step
	// waiting keeps its dot. The mark is six rows of fourteen cells, its
	// underscore a cell short of the ring.
	t.Cleanup(func() { SetMarks(MarksUnicode) })
	SetMarks(MarksASCII)
	if got := Sep(); got != "|" {
		t.Errorf("the separator: %q", got)
	}
	if got := Marks("·"); got != "." {
		t.Errorf("a step waiting: %q", got)
	}
	logo := NewTheme(true).Logo()
	if len(logo) != 6 {
		t.Fatalf("%d rows", len(logo))
	}
	for i, l := range logo {
		plain := ansi.Strip(l)
		if lipgloss.Width(plain) != 14 || strings.ContainsAny(plain, "▀▄█") {
			t.Errorf("row %d: %q", i, plain)
		}
		if strings.Contains(plain, "_#") {
			t.Errorf("row %d: the underscore meets the ring: %q", i, plain)
		}
	}
}
