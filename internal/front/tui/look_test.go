package tui_test

import (
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/app/demo"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/cli"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/front/tui"
)

// The screens on a wide terminal, as wsl-shrinker lays out its own: panes
// beside each other, rails and panels, bars that say how long each step
// took. These keep what the look is for, whatever the words.

func TestTheWideWizardHasItsStepsBesideThePage(t *testing.T) {
	f, _ := wizardOn(t, "en", 100, 22, wizardPages["distros"])
	plain := ansi.Strip(f)
	// The step and its name in the header, and every step on the rail.
	if !strings.Contains(lineWith(plain, "wsl-spawner"), "5 of 7 · Distributions") {
		t.Errorf("the header:\n%s", plain)
	}
	for _, step := range []string{"✓ Welcome", "✓ Language", "✓ Colors", "✓ Start", "▪ Distributions", "▪ Sign-out hook", "▪ Confirm"} {
		row := lineWith(plain, step)
		// On the rail: at the left, the page beside it.
		if row == "" || strings.Index(row, step) > 3 {
			t.Errorf("%q is not on the rail:\n%s", step, plain)
		}
	}
	// The page beside the rail: its question starts right of the rail.
	if row := lineWith(plain, "For each distribution"); strings.Index(row, "For each") < 20 {
		t.Errorf("the page is not beside the rail:\n%s", plain)
	}
}

func TestTheHistoryShowsTheChosenRunStepByStep(t *testing.T) {
	// The eighth run from the newest is the stop whose hook failed: chosen,
	// its pane names the hook and its exit code, and each step's time.
	f := ansi.Strip(frame(t, "en", 120, 30, "h down down down down down down down"))
	pane := tui.PaneText(f)
	for _, want := range []string{"2026-11-08 22:41", "a step failed", "hook 10-backup-db, exit 1", "docker: 3 containers", "34 s", "sync to disk"} {
		if !strings.Contains(pane, want) {
			t.Errorf("no %q in the pane of\n%s", want, f)
		}
	}
	// Up again: the newest, a start.
	if pane := tui.PaneText(ansi.Strip(frame(t, "en", 120, 30, "h down up"))); !strings.Contains(pane, "start") || !strings.Contains(pane, "started") {
		t.Errorf("the newest run's pane: %q", pane)
	}
}

func TestAStopShowsEachStepsTimeAsABar(t *testing.T) {
	f := ansi.Strip(frame(t, "en", 112, 18, "s enter"))
	for _, step := range []string{"✓ hook 10-backup-db", "✓ docker: 3 containers"} {
		row := lineWith(f, step)
		// The block's edge, the step, its bar, its seconds.
		if !strings.Contains(row, "▌") || !strings.Contains(row, "▪") || !strings.HasSuffix(strings.TrimRight(row, " "), " s") {
			t.Errorf("%q: %q in\n%s", step, row, f)
		}
	}
}

func TestASnapshotPartWayIntoAStopShowsTheStepUnderWay(t *testing.T) {
	// atKeys is a frame of keys that far into the stop they begin, on a
	// new demo PC; at is the one of "s enter".
	atKeys := func(keys string, d time.Duration) string {
		a, _, done, err := demo.New(true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(done)
		i18n.Set(i18n.Lang("en"))
		c := api.Client{Run: cli.InProcess(func() (*app.App, error) { return a, nil }), Lang: "en"}
		return ansi.Strip(tui.SnapshotAt(c, 120, 24, true, tui.ParseKeys(keys), demo.Now, d))
	}
	at := func(d time.Duration) string { return atKeys("s enter", d) }
	// Nine seconds in: the hook (2 s) has ended, Docker has run for 7.
	f := at(9 * time.Second)
	if row := lineWith(f, "hook 10-backup-db"); !strings.Contains(row, "✓") || !strings.HasSuffix(strings.TrimRight(row, " "), "2 s") {
		t.Errorf("the hook is not done in 2 s: %q in\n%s", row, f)
	}
	if row := lineWith(f, "docker: 3 containers"); strings.Contains(row, "✓") || !strings.Contains(row, "7 s") {
		t.Errorf("Docker is not 7 s under way: %q in\n%s", row, f)
	}
	// The stop has not ended: no verdict yet.
	if strings.Contains(f, "Stopped") {
		t.Errorf("the stop ended in\n%s", f)
	}
	// Past its end, the frame is Snapshot's.
	if got, want := at(time.Hour), ansi.Strip(frame(t, "en", 120, 24, "s enter")); got != want {
		t.Errorf("an hour in:\n%s\nto its end:\n%s", got, want)
	}
	// Two distributions stop at the same time, each on its own clock: nine
	// seconds in, Ubuntu's Docker has run for 7, and Work (1 s) has ended.
	both := atKeys("S enter", 9*time.Second)
	if row := lineWith(both, "docker: 3 containers"); !strings.Contains(row, " 7 s") || !strings.Contains(lineWith(both, "Stopping"), " 9 s") {
		t.Errorf("two stops nine seconds in:\n%s", both)
	}
	if !strings.Contains(lineWith(both, "▌ Work"), "stopped") {
		t.Errorf("Work has not ended nine seconds in:\n%s", both)
	}
	if got, want := atKeys("S enter", time.Minute), ansi.Strip(frame(t, "en", 120, 24, "S enter")); got != want {
		t.Errorf("a minute in:\n%s\nto its end:\n%s", got, want)
	}
	// A key after the moment is not pressed: the frame stays at it.
	if f := atKeys("s enter enter", 9*time.Second); !strings.Contains(f, "Stopping Ubuntu-24.04") {
		t.Errorf("a key after the moment went on:\n%s", f)
	}
}

func TestItsStopsBarsShareOneScale(t *testing.T) {
	// Every stop of the selected distribution has a bar of the same width,
	// the one that failed too, whose word is longer: one scale for all.
	f := ansi.Strip(frame(t, "en", 120, 44, ""))
	row := regexp.MustCompile(`^\s+\d\d-\d\d \d\d:\d\d  (▪+)  `)
	var widths []int
	for _, l := range tui.RightPane(f) {
		if m := row.FindStringSubmatch(l); m != nil {
			widths = append(widths, utf8.RuneCountInString(m[1]))
		}
	}
	if len(widths) < 2 {
		t.Fatalf("no stops in the pane of\n%s", f)
	}
	for _, w := range widths {
		if w != widths[0] || w < 8 {
			t.Errorf("bars of %v cells in\n%s", widths, f)
			break
		}
	}
	if !strings.Contains(tui.PaneText(f), "Its stops · against the 300 s limit") {
		t.Errorf("no scale in the title of\n%s", f)
	}
}

func TestTheWideCheckSaysItsVerdictAndFitsInTwoColumns(t *testing.T) {
	f := ansi.Strip(frame(t, "en", 120, 30, "c"))
	// The verdict first: Work's failed unit is the one to look at.
	if !strings.Contains(f, "! 1 to look at  ·  8 fine") {
		t.Errorf("no verdict in\n%s", f)
	}
	// Work's group stands right of the rule, the first group left of it.
	if row := lineWith(f, "▪ Work"); !strings.Contains(row, "│") || strings.Index(row, "│") > strings.Index(row, "▪ Work") {
		t.Errorf("Work is not in the right column: %q in\n%s", row, f)
	}
	if row := lineWith(f, "▪ Stopping"); strings.Contains(row[:strings.Index(row, "▪ Stopping")], "│") {
		t.Errorf("Stopping is not in the left column: %q in\n%s", row, f)
	}
	// Every group fits without scrolling, the last one too.
	if !strings.Contains(f, "▪ Network") {
		t.Errorf("the last group does not fit in\n%s", f)
	}
}

func TestTheHistorysChoiceFollowsTheKeysBackFromItsEnd(t *testing.T) {
	// Down past the oldest run, then one up: the next to oldest is chosen,
	// not the oldest still.
	keys := "h" + strings.Repeat(" down", 15) + " up"
	if pane := tui.PaneText(ansi.Strip(frame(t, "en", 120, 30, keys))); !strings.Contains(pane, "2026-11-07 23:10") {
		t.Errorf("one up from the end chose: %q", pane)
	}
	// The check and help scroll no further than their last line: one up
	// from far past it moves at once.
	for _, screen := range []string{"c", "?"} {
		far := ansi.Strip(frame(t, "en", 80, 16, screen+strings.Repeat(" down", 40)))
		back := ansi.Strip(frame(t, "en", 80, 16, screen+strings.Repeat(" down", 40)+" up"))
		if far == back {
			t.Errorf("%s: one up from far past the end does not move", screen)
		}
	}
}

func TestEveryWizardPageShowsWholeAtEverySize(t *testing.T) {
	// No page is cut short: no "…" on any, and a page beside its rail shows
	// every word the same page shows without the rail at that height.
	word := regexp.MustCompile(`[A-Za-z가-힣][A-Za-z0-9가-힣'-]{2,}`)
	words := func(f string) map[string]bool {
		out := map[string]bool{}
		for _, w := range word.FindAllString(ansi.Strip(f), -1) {
			out[w] = true
		}
		return out
	}
	for _, lang := range []string{"en", "ko"} {
		for name, keys := range wizardPages {
			for _, h := range []int{16, 21, 22, 30} {
				narrow, _ := wizardOn(t, lang, 79, h, keys)
				want := words(narrow)
				for _, w := range []int{80, 97, 98, 120} {
					f, _ := wizardOn(t, lang, w, h, keys)
					if strings.Contains(ansi.Strip(f), "…") {
						t.Errorf("%s %s %dx%d is cut:\n%s", lang, name, w, h, ansi.Strip(f))
						continue
					}
					got := words(f)
					for x := range want {
						if !got[x] {
							t.Errorf("%s %s %dx%d lost %q:\n%s", lang, name, w, h, x, ansi.Strip(f))
							break
						}
					}
				}
			}
		}
	}
}

func TestTheHistorysDistributionsShareTheirBars(t *testing.T) {
	// The run of 11-11 22:36 stopped Ubuntu-24.04 (three steps) and Work
	// (two, shorter names): their bars start in one column.
	f := ansi.Strip(frame(t, "en", 120, 30, "h down"))
	var starts []int
	for _, l := range tui.RightPane(f) {
		if i := strings.Index(l, "▪"); i >= 0 && strings.Contains(l, "✓") {
			starts = append(starts, i)
		}
	}
	if len(starts) < 5 {
		t.Fatalf("not the run of two distributions:\n%s", f)
	}
	for _, i := range starts {
		if i != starts[0] {
			t.Errorf("bars start at %v:\n%s", starts, f)
			break
		}
	}
}
