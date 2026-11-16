package tui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/app/demo"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/cli"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/front/tui"
	"github.com/kanggu12/wsl-spawner/internal/front/ui"
	"github.com/kanggu12/wsl-spawner/internal/version"
)

// longVersion has the header measured with a version longer than any this
// tool will have soon, as a test build's "dev" leaves room a release's
// version does not.
func longVersion(t *testing.T) {
	was := version.Version
	version.Version = "v10.10.10"
	t.Cleanup(func() { version.Version = was })
}

// checkRule holds the row under the header, rule, against the row under
// it: a rule from margin to margin whose every ┬ has a pane rule under it.
// Where a question's box clears its row from edge to edge, or stands on
// it, the row has no rule at all, never a piece of one beside the box.
func checkRule(t *testing.T, at string, w int, rule, under []string) {
	t.Helper()
	if len(rule) != w {
		t.Errorf("%s: the row under the header is %d cells, not %d", at, len(rule), w)
		return
	}
	if isRule(rule) {
		// A rule: whole, between the margins, joined only to pane rules.
		if rule[0] != " " || rule[w-1] != " " {
			t.Errorf("%s: the rule does not stand between the margins: %q", at, strings.Join(rule, ""))
		}
		for i := 1; i < w-1; i++ {
			switch {
			case rule[i] == "┬" && (i >= len(under) || under[i] != "│"):
				t.Errorf("%s: the rule's ┬ at column %d hangs over %q, not a pane rule", at, i, strings.Join(under, ""))
			case rule[i] != "─" && rule[i] != "┬":
				t.Errorf("%s: the rule has %q at column %d: %q", at, rule[i], i, strings.Join(rule, ""))
			}
		}
		return
	}
	// No rule: a box's row. The box's own border may run along it, but no
	// piece of the rule is left beside the box.
	first, last := -1, -1
	for i, c := range rule {
		if c != "" && strings.ContainsAny(c, "╭┌└╰╮┐┘╯│") {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	for i, c := range rule {
		if (c == "─" || c == "┬") && (first < 0 || i < first || i > last) {
			t.Errorf("%s: a piece of the rule is left beside the box at column %d: %q", at, i, strings.Join(rule, ""))
			return
		}
	}
}

// isRule reports whether a row is the header's rule: it starts a cell in.
func isRule(row []string) bool { return len(row) > 2 && (row[1] == "─" || row[1] == "┬") }

// cells is a row of a frame as its cells: a character two cells wide, as a
// Korean one, takes two, the second empty, so a column is an index.
func cells(row string) []string {
	var out []string
	for _, r := range row {
		out = append(out, string(r))
		// The second cell of a wide character holds nothing of its own.
		for range ansi.StringWidth(string(r)) - 1 {
			out = append(out, "")
		}
	}
	return out
}

// rows is a frame as plain text, a row a string.
func rows(f string) []string { return strings.Split(ansi.Strip(f), "\n") }

// TestTheHeaderStandsOnARuleThatMeetsThePanes: on every screen, at every
// size and in both languages, the row under the header is a rule from
// margin to margin. Where it has a ┬, a pane rule starts right under it,
// and where the main screen or the history starts a pane rule under it, the
// rule has its ┬ there: the panes hang from the header. A question's box
// may clear the rule's row or stand on it, never cut the rule short; the
// short heights are where a box comes closest.
func TestTheHeaderStandsOnARuleThatMeetsThePanes(t *testing.T) {
	all := append(append([][2]int{}, sizes...), [2]int{120, 16}, [2]int{160, 16}, [2]int{200, 16}, [2]int{100, 14})
	for _, lang := range []string{"en", "ko"} {
		for name, keys := range states {
			for _, s := range all {
				w, h := s[0], s[1]
				r := rows(frame(t, lang, w, h, keys))
				rule, under := cells(r[1]), cells(r[2])
				at := fmt.Sprintf("%s %s %dx%d", lang, name, w, h)
				// Screens with no box have their rule whole.
				if !strings.HasPrefix(name, "ask ") && !isRule(rule) {
					t.Errorf("%s: no rule under the header: %q", at, r[1])
					continue
				}
				checkRule(t, at, w, rule, under)
				// The main screen and the history have no other │ in their
				// first row than the rule between their panes.
				if name != "main" && name != "history" {
					continue
				}
				for i, c := range under {
					if c == "│" && i < w && rule[i] != "┬" {
						t.Errorf("%s %s %dx%d: the pane rule at column %d does not meet the header's rule: %q", lang, name, w, h, i, r[1])
					}
				}
			}
		}
	}
}

// TestTheHeaderSaysWhereItIs: a screen opened from the main one names
// itself after the version, in the words of the key that opens it, as the
// setup does; the main screen says WSL's version there.
func TestTheHeaderSaysWhereItIs(t *testing.T) {
	for _, lang := range []string{"en", "ko"} {
		for keys, key := range map[string]string{"h": "key.history", "?": "key.help", "c": "key.check", "s enter": "key.stop", "down t enter": "key.start"} {
			head := rows(frame(t, lang, 120, 30, keys))[0]
			// The frame set the language, so the catalog speaks it now.
			if want := i18n.T(key); !strings.Contains(head, "wsl-spawner ") || !strings.Contains(head, "·  "+want) {
				t.Errorf("%s after %q: the header does not name the screen %q after the version: %q", lang, keys, want, head)
			}
		}
		if head := rows(frame(t, lang, 120, 30, ""))[0]; !strings.Contains(head, "·  WSL ") {
			t.Errorf("%s: the main screen's header does not say WSL's version: %q", lang, head)
		}
	}
}

// TestTheHeaderKeepsItsTwoSidesApart: the right side, from the hook's
// square on, stands at least six cells from the left, more than the five
// between two of its pieces, so the two read as two; closer only where the
// hook alone stands beside the name alone, on a narrow screen. The square
// is the theme's dot, ▪, which only the right side has. Every width from
// 40 to 160 columns, as a piece drops at a width of its own.
func TestTheHeaderKeepsItsTwoSidesApart(t *testing.T) {
	for _, lang := range []string{"en", "ko"} {
		longVersion(t)
		// One demo for every width: the main screen and the history only
		// read it.
		a, _, done, err := demo.New(true)
		if err != nil {
			t.Fatal(err)
		}
		i18n.Set(i18n.Lang(lang))
		c := api.Client{Run: cli.InProcess(func() (*app.App, error) { return a, nil }), Lang: lang}
		for _, keys := range []string{"", "h"} {
			for w := 40; w <= 160; w++ {
				head := rows(tui.Snapshot(c, w, 30, true, tui.ParseKeys(keys), demo.Now))[0]
				// A screen opened from the main one keeps its name before the
				// hook, which may go on a narrow one: nothing on the right then.
				i := strings.Index(head, "▪")
				// The main screen keeps the hook at every width, in fewer
				// words on the narrowest.
				if keys == "" && !strings.Contains(head, i18n.T("header.hook")) && !strings.Contains(head, i18n.T("header.hook.short")) {
					t.Errorf("%s at %d columns: the hook left the main screen's header: %q", lang, w, head)
				}
				if i < 0 {
					continue
				}
				left := strings.TrimRight(head[:i], " ")
				gap := ansi.StringWidth(head[:i]) - ansi.StringWidth(left)
				least := 2
				if strings.Contains(left, "·") || strings.Contains(head[i:], "·") {
					least = 6
				}
				if gap < least {
					t.Errorf("%s %q at %d columns: the header's sides are %d cells apart, not %d: %q", lang, keys, w, gap, least, head)
				}
			}
		}
		done()
	}
	i18n.Set(i18n.English)
}

// TestTheHookOutlastsWSLsVersion: on a narrow main screen the sign-out hook
// stays in the header where WSL's version goes, as it matters more.
func TestTheHookOutlastsWSLsVersion(t *testing.T) {
	for _, lang := range []string{"en", "ko"} {
		head := rows(frame(t, lang, 60, 20, ""))[0]
		if !strings.Contains(head, i18n.T("header.hook")) {
			t.Errorf("%s 60x20: the hook left the header: %q", lang, head)
		}
	}
}

// TestTheWizardHeaderNamesTheStep: the setup's header says the step's
// number and name, as wsl-shrinker's does, short of room the name first
// and then the number gone, and right under it lies the bar of the steps,
// in the place of the rule, at every size.
func TestTheWizardHeaderNamesTheStep(t *testing.T) {
	longVersion(t)
	keys := "enter enter enter enter"
	for _, s := range [][2]int{{100, 22}, {80, 24}} {
		f, _ := wizardOn(t, "en", s[0], s[1], keys)
		r := rows(f)
		if !strings.Contains(r[0], "step 5 of 7 · Distributions") {
			t.Errorf("%dx%d: the wizard's header does not name the step: %q", s[0], s[1], r[0])
		}
		if !strings.Contains(r[1], "▪") || strings.Contains(r[1], "─") {
			t.Errorf("%dx%d: the wizard's bar is not right under its header: %q", s[0], s[1], r[1])
		}
	}
	// The name goes first, then the number; the name of the tool, its
	// version and "setup" stay.
	f, _ := wizardOn(t, "en", 60, 22, keys)
	if head := rows(f)[0]; !strings.Contains(head, "step 5 of 7") || strings.Contains(head, "Distributions") || !strings.Contains(head, "setup") {
		t.Errorf("60 columns: the wizard's header does not keep the number alone: %q", head)
	}
	f, _ = wizardOn(t, "en", 48, 22, keys)
	if head := rows(f)[0]; strings.Contains(head, "step") || !strings.Contains(head, "v10.10.10") || !strings.Contains(head, "setup") {
		t.Errorf("48 columns: the wizard's header does not keep its name, version and \"setup\" alone: %q", head)
	}
	f, _ = wizardOn(t, "ko", 100, 22, keys)
	if head := rows(f)[0]; !strings.Contains(head, "5/7 단계") {
		t.Errorf("the Korean wizard's header does not count the step: %q", head)
	}
}

// TestTheRuleIsASCIIWhereTheMarksAre: where the console's marks are ASCII,
// the rule is dashes and its join a plus, one cell each as the marks were.
func TestTheRuleIsASCIIWhereTheMarksAre(t *testing.T) {
	ui.SetMarks(ui.MarksASCII)
	defer ui.SetMarks(ui.MarksUnicode)
	r := rows(frame(t, "en", 120, 30, ""))
	if strings.Trim(r[1], " -+") != "" || !strings.Contains(r[1], "+") || len(r[1]) != 120 {
		t.Errorf("the ASCII rule is not dashes and a plus: %q", r[1])
	}
}
