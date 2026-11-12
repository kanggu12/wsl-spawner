package tui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/app/demo"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/cli"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/front/tui"
)

// frame is one frame of the demo's screen at w by h after keys, in lang.
func frame(t *testing.T, lang string, w, h int, keys string) string {
	t.Helper()
	a, _, done, err := demo.New(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	i18n.Set(i18n.Lang(lang))
	c := api.Client{Run: cli.InProcess(func() (*app.App, error) { return a, nil }), Lang: lang}
	return tui.Snapshot(c, w, h, true, tui.ParseKeys(keys), demo.Now)
}

// states are the screen's states, as the keys that reach them.
var states = map[string]string{
	"main":           "",
	"other row":      "down down",
	"stopped row":    "down",
	"owned row":      "end",
	"ask stop":       "s",
	"ask stop all":   "S",
	"ask set keep":   "down down k",
	"ask set skip":   "x",
	"stopped":        "s enter",
	"started":        "down t enter",
	"set":            "down down m enter",
	"check":          "c",
	"check scrolled": "c pgdown",
	"history":        "h",
	"help":           "?",
	"help scrolled":  "? pgdown pgdown",
}

// sizes are the terminal sizes the screen is measured at.
var sizes = [][2]int{{160, 40}, {120, 30}, {100, 30}, {80, 24}, {60, 20}, {40, 12}}

// leak finds words that are a catalog key or a formatting mistake.
var leak = regexp.MustCompile(`%!|\(MISSING\)|EXTRA|(^|\s)(key|col|state|kind|end|detail|plan|back|choice|note|ask|change|run|step|result|check|history|help|header|logo)\.[a-z]`)

func TestEveryStateFitsEverySizeInBothLanguages(t *testing.T) {
	for _, lang := range []string{"en", "ko"} {
		for name, keys := range states {
			for _, s := range sizes {
				f := frame(t, lang, s[0], s[1], keys)
				lines := strings.Split(f, "\n")
				if len(lines) != s[1] {
					t.Errorf("%s %s %dx%d: %d lines", lang, name, s[0], s[1], len(lines))
					continue
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w != s[0] {
						t.Errorf("%s %s %dx%d: line %d is %d wide: %q", lang, name, s[0], s[1], i, w, ansi.Strip(l))
						break
					}
				}
				if m := leak.FindString(ansi.Strip(f)); m != "" {
					t.Errorf("%s %s %dx%d: %q on screen", lang, name, s[0], s[1], m)
				}
			}
		}
	}
	i18n.Set(i18n.English)
}

func TestTheMainScreenSaysWhatMatters(t *testing.T) {
	// At the size of the README's animation: two panes; the table names whose a distribution
	// is in short, and the last stop says who asked in short words, as its
	// line has room for.
	f := ansi.Strip(frame(t, "en", 120, 30, ""))
	pane := tui.PaneText(f)
	for _, want := range []string{
		"wsl-spawner", "WSL 3.0.5.0", "sign-out hook", "1 kept running", "1 at sign-in",
		"Ubuntu-24.04", "at sign-in", "3/4", "docker-desktop", "Docker Desktop's", "Recent",
		"s  stop", "q  quit",
	} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	for _, want := range []string{
		"clean 11-11 22:36 · 41 s · sign-out", "hook 10-backup-db", "docker: 3 containers", "sync to disk", "38 s",
		"db brought back", "Next stop", "60 s of 300", "docker: 3 containers, the longest stop 60 s",
		"then WSL ends the rest", "Containers", "Choices", "✓ set up", "✓ kept running", "✓ starts at sign-in",
	} {
		if !strings.Contains(pane, want) {
			t.Errorf("no %q in the right pane of\n%s", want, f)
		}
	}
	// Wider, the table says each last stop with its time, and the pane who
	// asked in whole words.
	f = ansi.Strip(frame(t, "en", 160, 40, ""))
	if !strings.Contains(tui.TableRow(f, "Ubuntu-24.04"), "clean 11-11 22:36") || !strings.Contains(tui.PaneText(f), "41 s · as Windows signed out") {
		t.Errorf("160x40:\n%s", f)
	}
}

func TestAStopPlaysToItsEnd(t *testing.T) {
	f := ansi.Strip(frame(t, "en", 120, 30, "s enter"))
	for _, want := range []string{"Stopped Ubuntu-24.04", "✓ hook 10-backup-db", "✓ docker: 3 containers", "21 s", "✓ WSL ends the rest", "stopped, every step in its time", "back"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
}

func TestTheBoxAsksFirstAndEscDoesNothing(t *testing.T) {
	f := ansi.Strip(frame(t, "en", 120, 30, "s"))
	if !strings.Contains(f, "Stop Ubuntu-24.04?") || !strings.Contains(f, "▪ docker: 3 containers, the longest stop 60 s") {
		t.Fatalf("%s", f)
	}
	// esc closes the box: the main screen again, nothing stopped.
	f = ansi.Strip(frame(t, "en", 120, 30, "s esc"))
	if strings.Contains(f, "Stop Ubuntu-24.04?") || !strings.Contains(f, "running") {
		t.Fatalf("%s", f)
	}
}

func TestKoreanScreen(t *testing.T) {
	f := ansi.Strip(frame(t, "ko", 120, 30, ""))
	for _, want := range []string{"배포판", "실행 중", "✓ 설치", "계속 켜 두기", "멈춤", "점검"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	i18n.Set(i18n.English)
}

// keyCall finds a word key in the screen's source: i18n.T("…") or
// i18n.TN("…".
var keyCall = regexp.MustCompile(`i18n\.TN?\("([a-z0-9.]+)"`)

func TestEveryWordTheScreenNamesIsInBothCatalogs(t *testing.T) {
	var keys []string
	for _, f := range []string{"model.go", "view.go", "setup.go"} {
		b, err := os.ReadFile(filepath.Join(".", f))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range keyCall.FindAllStringSubmatch(string(b), -1) {
			// A key built at run time ("back."+what) is checked below,
			// with every value it is built from.
			if !strings.HasSuffix(m[1], ".") && !slices.Contains(keys, m[1]) {
				keys = append(keys, m[1])
			}
		}
	}
	for prefix, values := range map[string][]string{
		"back.":    {"started", "running", "failed", "exited", "gone"},
		"result.":  {"clean", "limit", "failed", "error", "started", "running"},
		"cstate.":  {"running", "exited", "paused", "created", "restarting", "dead"},
		"history.": {"stop", "start"},
		"check.g.": {"stopping", "signin", "wsl", "network"},
	} {
		for _, v := range values {
			keys = append(keys, prefix+v)
		}
	}
	if len(keys) < 60 {
		t.Fatalf("only %d keys found", len(keys))
	}
	for _, k := range keys {
		if !i18n.Has(i18n.English, k) || !i18n.Has(i18n.Korean, k) {
			t.Errorf("%s: not in both catalogs", k)
		}
	}
	fmt.Fprint(os.Stderr, "")
}

func TestAContainerSaysWhetherItComesBack(t *testing.T) {
	// Ubuntu is set up and brings back every running one: db, with no
	// policy, comes back by wsl-spawner; web by its own policy.
	f := ansi.Strip(frame(t, "en", 120, 30, ""))
	if !strings.Contains(lineWith(f, "▪ db "), "back by wsl-spawner") || !strings.Contains(lineWith(f, "▪ web "), "back by its policy") {
		t.Fatalf("%s", f)
	}
	// Work is not set up: its container with no policy won't be back, and
	// the screen says so, not that wsl-spawner brings it back.
	f = ansi.Strip(frame(t, "en", 120, 30, "down down"))
	if !strings.Contains(lineWith(f, "▪ notes "), "won't be back") || strings.Contains(f, "back by wsl-spawner") {
		t.Fatalf("%s", f)
	}
}

func TestTheContainersStandBesideTheRestFrom100Columns(t *testing.T) {
	// At 100 columns the containers stand beside the other sections, every
	// one of them, rather than under Next stop cut to two.
	f := ansi.Strip(frame(t, "en", 100, 30, ""))
	if row := lineWith(f, "Containers"); !strings.Contains(row, "Choices") {
		t.Errorf("the containers are not beside the choices:\n%s", f)
	}
	for _, name := range []string{"▪ web ", "▪ db ", "▪ cache ", "▪ job "} {
		if lineWith(f, name) == "" {
			t.Errorf("no %q in\n%s", name, f)
		}
	}
}

func TestAWideScreenHasTheNewestStopsBesideTheRest(t *testing.T) {
	// From about 140 columns the newest stops and starts stand in the right
	// column, under the containers, rather than under everything with the
	// right half empty; at 120 they stay under the details.
	for _, s := range [][2]int{{120, 30}, {140, 40}, {160, 45}, {200, 50}} {
		f := ansi.Strip(frame(t, "en", s[0], s[1], ""))
		lines := strings.Split(f, "\n")
		// The newest stops and starts under the table, in the left pane; the
		// selected distribution's sections in the right one.
		if !strings.Contains(strings.Join(tui.LeftPane(f), "\n"), "\n   Recent") {
			t.Errorf("%dx%d: Recent is not under the table:\n%s", s[0], s[1], f)
		}
		if right := strings.Join(tui.RightPane(f), "\n"); !strings.Contains(right, "Containers") || !strings.Contains(right, "Last stop") {
			t.Errorf("%dx%d: the details are not beside the table:\n%s", s[0], s[1], f)
		}
		// Every line exactly as wide as the screen, no more lines than it has.
		for i, l := range lines {
			if lipgloss.Width(l) != s[0] || len(lines) != s[1] {
				t.Errorf("%dx%d: line %d is %d wide of %d lines", s[0], s[1], i, lipgloss.Width(l), len(lines))
				break
			}
		}
	}
	// Under the two panes' width, one column: Recent under the details.
	f := ansi.Strip(frame(t, "en", 100, 30, ""))
	if strings.Index(lineWith(f, "Recent"), "Recent") != 3 {
		t.Errorf("100x30: Recent is not under the details:\n%s", f)
	}
	// The newest stops and starts are never cut short: a long one goes on
	// under its first line.
	for _, keys := range []string{"s enter enter", "down down", "S enter enter down down"} {
		for _, w := range []int{120, 130, 140, 200} {
			// In the left pane: an image beside a container may be shortened
			// in its middle, as it is meant to.
			if f := ansi.Strip(frame(t, "en", w, 40, keys)); strings.Contains(strings.Join(tui.LeftPane(f), "\n"), "…") {
				t.Errorf("%q at %d: cut short:\n%s", keys, w, f)
			}
		}
	}
}

func TestALongLineWrapsLeftAndLeavesTheContainersTheirRoom(t *testing.T) {
	// After a start that brought back three, the line that says so is long:
	// it wraps in the left column, and the containers keep the room for
	// their images, as before it.
	f := ansi.Strip(frame(t, "en", 160, 40, "s enter enter t enter enter"))
	if !strings.Contains(lineWith(f, "▪ web "), "nginx:1.27") || !strings.Contains(tui.PaneText(f), "its policy · 11-12 09:30") {
		t.Errorf("%s", f)
	}
}

func TestTheNextStopBarEndsWithWhatItSays(t *testing.T) {
	// The bar of the longest wait ends with its label, as a run's bar does:
	// not with a label at the far edge, said again in the steps.
	for _, w := range []int{80, 100, 120, 200} {
		f := ansi.Strip(frame(t, "en", w, 50, ""))
		bar := lineWith(f, "60 s of 300")
		if !strings.Contains(bar, "▪  60 s of 300") || strings.Contains(f, "limit 300 s") {
			t.Errorf("%d: the bar %q in\n%s", w, bar, f)
		}
	}
}

func TestADistributionStoppedHereKeepsWhatIsSetUpInIt(t *testing.T) {
	// Stopped from the screen, Ubuntu no longer says what runs in it; the
	// screen still knows it was set up, kept running and started at sign-in,
	// and takes its last stop from the history.
	f := ansi.Strip(frame(t, "en", 120, 30, "s enter enter"))
	row := tui.TableRow(f, "Ubuntu-24.04")
	for _, want := range []string{"stopped", "at sign-in"} {
		if !strings.Contains(row, want) {
			t.Errorf("no %q in its row %q", want, row)
		}
	}
	if strings.Contains(row, "stop only") || !strings.Contains(f, "✓ set up") {
		t.Errorf("Ubuntu reads as not set up:\n%s", f)
	}
	// Debian was never read: what is set up in it is not guessed, and m
	// asks nothing but says so.
	f = ansi.Strip(frame(t, "en", 120, 30, "down m"))
	if strings.Contains(f, "Change Debian?") || !strings.Contains(f, "Debian is not running: what is set up in it shows once it runs") {
		t.Fatalf("%s", f)
	}
}

func TestAStoppedDistributionSaysWhatIsKnownOnce(t *testing.T) {
	// Debian, never read: the verdict says its choices show once it runs,
	// once; no section says it again, and no last stop is a lone dash.
	f := ansi.Strip(frame(t, "en", 120, 30, "down"))
	all := strings.ToLower(tui.PaneText(f) + " " + strings.Join(strings.Fields(strings.Join(tui.LeftPane(f), " ")), " "))
	if n := strings.Count(all, "shows once it runs"); n != 1 {
		t.Errorf("said %d times:\n%s", n, f)
	}
	for _, l := range strings.Split(f, "\n") {
		// The details' sections, and a dash standing for a last stop.
		if strings.TrimSpace(l) == "Choices" || strings.TrimSpace(l) == "· –" {
			t.Errorf("%q in\n%s", l, f)
		}
	}
	// Work, read and then stopped here: its choices are known and shown, so
	// the verdict does not say they show once it runs.
	f = ansi.Strip(frame(t, "en", 120, 30, "down down s enter enter"))
	if strings.Contains(f, "shows once it runs") || !strings.Contains(f, "Choices") {
		t.Errorf("%s", f)
	}
}

func TestTheBoxStandsOnRowsOfItsOwn(t *testing.T) {
	// Beside the box, nothing of the screen is left cut in two.
	f := ansi.Strip(frame(t, "en", 120, 30, "s"))
	row := lineWith(f, "Stop Ubuntu-24.04?")
	if left := row[:strings.Index(row, "│")]; strings.TrimSpace(left) != "" {
		t.Errorf("left of the box: %q", left)
	}
}

// boxes are the screen's boxes, as the keys that open them.
var boxes = map[string]string{
	"stop": "s", "stop all": "S", "nothing to stop": "S enter enter S", "start": "down t",
	"set up, keep, sign-in": "down down l", "take out": "m", "skip": "x",
}

// boxText is the text inside the box of a frame without its colors: each
// row between its two borders, the margins of the box left in.
func boxText(f string) []string {
	var out []string
	for _, l := range strings.Split(f, "\n") {
		// A row of the box has its two borders; the keys row has none.
		if a, b := strings.Index(l, "│"), strings.LastIndex(l, "│"); a >= 0 && b > a {
			out = append(out, strings.TrimRight(l[a+len("│"):b], " "))
		}
	}
	return out
}

func TestABoxFitsTheScreenWithItsEndAndKeys(t *testing.T) {
	// Whatever the size, the box stands between the header and the keys,
	// whole: its bottom border and its own keys in it.
	for _, lang := range []string{"en", "ko"} {
		for name, keys := range boxes {
			for _, s := range sizes {
				f := ansi.Strip(frame(t, lang, s[0], s[1], keys))
				lines := strings.Split(f, "\n")
				do := map[string]string{"en": "do it", "ko": "실행"}[lang]
				switch {
				case !strings.Contains(lines[0], "wsl-spawner"):
					t.Errorf("%s %s %dx%d: the header is gone:\n%s", lang, name, s[0], s[1], f)
				case !strings.Contains(lines[len(lines)-1], do):
					t.Errorf("%s %s %dx%d: the keys are gone:\n%s", lang, name, s[0], s[1], f)
				case !strings.Contains(f, "└") || !strings.Contains(strings.Join(boxText(f), "\n"), do):
					t.Errorf("%s %s %dx%d: the box lost its end:\n%s", lang, name, s[0], s[1], f)
				}
			}
		}
	}
	i18n.Set(i18n.English)
}

func TestABoxWrapsUnderItsWordsAndNeverAtAHyphen(t *testing.T) {
	for _, lang := range []string{"en", "ko"} {
		for name, keys := range boxes {
			for _, s := range sizes {
				f := ansi.Strip(frame(t, lang, s[0], s[1], keys))
				rows := boxText(f)
				for i, r := range rows {
					// A line that ends on a hyphen broke a word there: wsl-
					// spawner, wsl --shutdown.
					if strings.HasSuffix(r, "-") {
						t.Errorf("%s %s %dx%d: a word broken at its hyphen: %q\n%s", lang, name, s[0], s[1], r, f)
					}
					// An item's next line stands under its words, not under
					// its square.
					if i+1 < len(rows) && strings.Contains(r, "▪ ") {
						lead := strings.Index(r, "▪") + len("▪ ")
						next := rows[i+1]
						if next != "" && !strings.Contains(next, "▪") && !strings.HasPrefix(next, strings.Repeat(" ", lipgloss.Width(r[:lead]))) {
							t.Errorf("%s %s %dx%d: %q under %q", lang, name, s[0], s[1], next, r)
						}
					}
				}
			}
		}
	}
	i18n.Set(i18n.English)
}

func TestTheStopBoxNamesTheHooks(t *testing.T) {
	// The box names the hook as the details and the run do, not "1 hook".
	f := ansi.Strip(frame(t, "en", 120, 30, "s"))
	if !strings.Contains(strings.Join(boxText(f), "\n"), "▪ hook 10-backup-db") {
		t.Fatalf("%s", f)
	}
}

func TestTakingOutSaysKeepingItRunningEnds(t *testing.T) {
	f := ansi.Strip(frame(t, "en", 120, 30, "m"))
	if !strings.Contains(f, "take wsl-spawner out of it") || !strings.Contains(f, "no longer keep it running") {
		t.Fatalf("%s", f)
	}
}

func TestAStopOfEverythingShowsWSLStopping(t *testing.T) {
	f := ansi.Strip(frame(t, "en", 120, 30, "S enter"))
	if !strings.Contains(f, "then WSL") || !strings.Contains(lineAfter(f, "  ▌ WSL"), "✓ stopped") {
		t.Fatalf("%s", f)
	}
}

func TestStoppingEverythingWithNothingOfOursRunningStopsWSL(t *testing.T) {
	// Everything stopped once: nothing of wsl-spawner's runs any more. The
	// box says so and that WSL itself stops, with no rows left empty for a
	// plan it has not.
	f := ansi.Strip(frame(t, "en", 120, 30, "S enter enter S"))
	if !strings.Contains(f, "Stop WSL?") || !strings.Contains(f, "Nothing runs that wsl-spawner stops") {
		t.Fatalf("%s", f)
	}
	// The box's rows, each without its borders.
	var inside []string
	for _, l := range strings.Split(f, "\n") {
		if strings.Count(l, "│") == 2 {
			inside = append(inside, strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "│")))
		}
	}
	for i := 1; i < len(inside); i++ {
		// Two empty rows in a row are a part of the box with nothing in it.
		if inside[i] == "" && inside[i-1] == "" {
			t.Fatalf("an empty part in the box:\n%s", f)
		}
	}
	// Done, the stop runs to WSL's own: no command refused, WSL stopped.
	f = ansi.Strip(frame(t, "en", 120, 30, "S enter enter S enter"))
	if !strings.Contains(lineAfter(f, "  ▌ WSL"), "✓ stopped") || strings.Contains(f, "✗") {
		t.Fatalf("%s", f)
	}
	// With only another tool's distribution running, WSL stops that one too,
	// as the box said.
	f = ansi.Strip(frame(t, "en", 120, 30, "s enter enter down down s enter enter S enter enter"))
	if row := lineWith(f, "docker-desktop  "); !strings.Contains(row, "stopped") {
		t.Fatalf("docker-desktop's row %q in\n%s", row, f)
	}
}

func TestARunSaysItHasEnded(t *testing.T) {
	// Once a run has ended, its title says what it did, in both languages,
	// and none of it says it is still going.
	for _, c := range []struct{ lang, keys, want, going string }{
		{"en", "s enter", "Stopped Ubuntu-24.04", "Stopping"},
		{"en", "down t enter", "Started Debian", "Starting"},
		{"en", "S enter", "Stopped Ubuntu-24.04, Work · then WSL", "Stopping"},
		{"en", "S enter enter S enter", "Stopped WSL", "Stopping"},
		{"ko", "s enter", "Ubuntu-24.04 멈춤", "멈추는 중"},
		{"ko", "down t enter", "Debian 켜짐", "켜는 중"},
	} {
		f := ansi.Strip(frame(t, c.lang, 120, 30, c.keys))
		if title := lineWith(f, c.want); title == "" || strings.Contains(f, c.going) {
			t.Errorf("%s %q: no %q, or %q, in\n%s", c.lang, c.keys, c.want, c.going, f)
		}
	}
	// The next tests speak English.
	i18n.Set(i18n.English)
}

func TestARunKeepsItsTitleAndEachStepUnderItsName(t *testing.T) {
	// However small the screen, a run keeps its title on top, and each step
	// it shows stands under the name of its distribution.
	for _, lang := range []string{"en", "ko"} {
		// The keys of each run, and how its title begins in each language.
		for keys, title := range map[string]map[string]string{
			"s enter":      {"en": "Stopped", "ko": "Ubuntu-24.04"},
			"S enter":      {"en": "Stopped", "ko": "Ubuntu-24.04"},
			"down t enter": {"en": "Started", "ko": "Debian"},
		} {
			for _, s := range sizes {
				f := ansi.Strip(frame(t, lang, s[0], s[1], keys))
				lines := strings.Split(f, "\n")
				// The title, under the header and its blank row.
				if !strings.HasPrefix(lines[2], "  "+title[lang]) {
					t.Errorf("%s %q %dx%d: no title:\n%s", lang, keys, s[0], s[1], f)
				}
				for i, l := range lines {
					// A step, or a bar, of a distribution.
					if !strings.HasPrefix(l, "    ") || strings.TrimSpace(l) == "" {
						continue
					}
					// Up from it, its distribution's name comes before any
					// blank row.
					j := i - 1
					for j > 2 && strings.HasPrefix(lines[j], "    ") {
						j--
					}
					if j <= 2 || strings.TrimSpace(lines[j]) == "" {
						t.Errorf("%s %q %dx%d: %q has no name over it:\n%s", lang, keys, s[0], s[1], l, f)
						break
					}
				}
			}
		}
	}
	i18n.Set(i18n.English)
}

func TestAStopSaysTheTimeItTook(t *testing.T) {
	// The demo stops at once; its steps say 2 s and 21 s: the stop took 23,
	// said in seconds at the title's end as under it, one way of saying it.
	f := ansi.Strip(frame(t, "en", 120, 30, "s enter"))
	if !strings.HasSuffix(strings.TrimRight(lineWith(f, "Stopped Ubuntu-24.04"), " "), "23 s") || !strings.Contains(f, "23 s of 300") || strings.Contains(f, "0:23") {
		t.Fatalf("%s", f)
	}
}

func TestADemoStopTakesTheTimeItsStepsSay(t *testing.T) {
	// The demo's stop of Ubuntu says its steps took 2 s and 21 s: its last
	// stop says 23 s after, as the run did, not the moment it took to play.
	f := ansi.Strip(frame(t, "en", 120, 30, "s enter enter"))
	if !strings.Contains(f, "clean 11-12 09:30 · 23 s · by hand") {
		t.Errorf("%s", f)
	}
	// Two at once: each its own time, Work's 1 s not Ubuntu's 23.
	f = ansi.Strip(frame(t, "en", 120, 30, "S enter"))
	if !strings.Contains(f, "▪  1 s of 300") || !strings.Contains(f, "▪  23 s of 300") {
		t.Errorf("%s", f)
	}
	f = ansi.Strip(frame(t, "en", 120, 30, "S enter enter down down"))
	if !strings.Contains(f, "clean 11-12 09:30 · 1 s · by hand") {
		t.Errorf("%s", f)
	}
}

func TestTheDemoNotesItsOwnStopInside(t *testing.T) {
	// Stopped in the demo and started again: Ubuntu's last stop is that
	// stop, as the history says it, not last night's, and what came back
	// came back at this start, as its start says.
	f := ansi.Strip(frame(t, "en", 120, 30, "s enter enter t enter"))
	if !strings.Contains(f, "✓ started · web back by its policy, db brought back, cache back by its policy") {
		t.Errorf("the start:\n%s", f)
	}
	f = ansi.Strip(frame(t, "en", 120, 30, "s enter enter t enter enter"))
	if !strings.Contains(f, "clean 11-12 09:30 · 23 s · by hand") || strings.Contains(f, "41 s") || !strings.Contains(f, "11-12 09:30") {
		t.Errorf("the last stop:\n%s", f)
	}
}

func TestTheHistorySaysWhoAskedWhole(t *testing.T) {
	for _, lang := range []string{"en", "ko"} {
		f := ansi.Strip(frame(t, lang, 120, 30, "h"))
		want := map[string]string{"en": "sign-out", "ko": "로그아웃"}[lang]
		if !strings.Contains(f, want) || strings.Contains(f, "…") {
			t.Errorf("%s:\n%s", lang, f)
		}
	}
	i18n.Set(i18n.English)
}

func TestALastStopWithNothingNotedInsideComesFromTheHistory(t *testing.T) {
	// Work is not set up: its last stop is the history's.
	f := ansi.Strip(frame(t, "en", 140, 30, ""))
	if row := tui.TableRow(f, "Work"); !strings.Contains(row, "clean 11-11 22:36") {
		t.Fatalf("Work's row: %q", row)
	}
}

func TestSettingUpKeepsTheLastStopTheHistoryHas(t *testing.T) {
	// Work, set up just now: what is inside it notes this start alone, the
	// first. Its last stop is still the history's, and the first start a
	// fact beside its name.
	f := ansi.Strip(frame(t, "en", 160, 30, "down down m enter"))
	if row := tui.TableRow(f, "Work"); !strings.Contains(row, "clean 11-11 22:36") {
		t.Errorf("Work's row %q in\n%s", row, f)
	}
	if !strings.Contains(tui.PaneText(f), "clean 11-11 22:36 · 3 s · as Windows signed out") || !strings.Contains(tui.PaneText(f), "first start noted") {
		t.Errorf("%s", f)
	}
	// The status the commands print says the same.
	a, _, done, err := demo.New(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	c := api.Client{Run: cli.InProcess(func() (*app.App, error) { return a, nil })}
	if err := c.Do(t.Context(), "set", "Work", "--managed", "--yes"); err != nil {
		t.Fatal(err)
	}
	var out, errOut strings.Builder
	if _, err := c.Run(t.Context(), []string{"status"}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if row := lineWith(out.String(), "Work  "); !strings.Contains(row, "clean 11-11 22:36") {
		t.Errorf("Work's row %q in\n%s%s", row, out.String(), errOut.String())
	}
}

func TestTheCheckAndTheMainScreenAgreeOnALastStop(t *testing.T) {
	// Work is not set up: nothing in it notes how its starts end, but the
	// history holds wsl-spawner's clean stop of it, which the main screen
	// shows. The check says both, and nowhere that it is not known. In one
	// column, under 112 cells, each sentence keeps to a line.
	f := ansi.Strip(frame(t, "en", 104, 60, "c"))
	for _, want := range []string{"nothing in it notes how its starts end", "the history holds wsl-spawner's last stop of it: clean (2026-11-11 22:36)"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	if strings.Contains(f, "not known") {
		t.Errorf("the check says it is not known:\n%s", f)
	}
	// Ubuntu says it from inside: the history's line is not said again.
	if n := strings.Count(f, "the history holds"); n != 1 {
		t.Errorf("the history's line %d times:\n%s", n, f)
	}
}

func TestTheKeysAreThoseThatDoSomethingHere(t *testing.T) {
	// docker-desktop is Docker Desktop's: no stop, start or choice.
	f := ansi.Strip(frame(t, "en", 120, 30, "down down down"))
	keys := lineWith(f, "select")
	for _, k := range []string{"s  stop", "t  start", "m  set up"} {
		if strings.Contains(keys, k) {
			t.Errorf("%q in %q", k, keys)
		}
	}
}

func TestHelpKeepsAPathOnOneLine(t *testing.T) {
	// At every width, the paths stay whole: a line never ends at a path's
	// hyphen.
	for w := 60; w <= 120; w++ {
		f := ansi.Strip(frame(t, "en", w, 60, "?"))
		for _, path := range []string{"/etc/wsl-spawner/pre-stop.d", "/etc/wsl-spawner/stop-units"} {
			if !strings.Contains(f, path) {
				t.Fatalf("%d wide: %s cut:\n%s", w, path, f)
			}
		}
	}
}

func TestTheWordsSayWhatTheyMean(t *testing.T) {
	// The sign-in change says where Windows lists the program it adds; the
	// box wraps the sentence, so its end is looked for.
	if f := ansi.Strip(frame(t, "en", 120, 30, "down down l")); !strings.Contains(f, "Task Manager under Startup apps") {
		t.Errorf("the sign-in change in\n%s", f)
	}
	// The help's heading in Korean names how it works, not a question.
	if f := ansi.Strip(frame(t, "ko", 120, 60, "?")); !strings.Contains(f, "작동 방식") {
		t.Errorf("the Korean help's heading in\n%s", f)
	}
	// A time in parentheses after a Korean sentence stands a space apart.
	if f := ansi.Strip(frame(t, "ko", 120, 60, "c")); !strings.Contains(f, "제시간에 (") {
		t.Errorf("the Korean check in\n%s", f)
	}
	// The next tests speak English.
	i18n.Set(i18n.English)
}

func TestASmallScreenShowsWhatItHasRoomFor(t *testing.T) {
	// Help at 40x12: the keys, not the logo's six rows over a heading.
	f := ansi.Strip(frame(t, "en", 40, 12, "?"))
	for _, want := range []string{"Keys", "select a distribution", "stop it"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	// A key's words wrap under its words, not a cell before them, at 40
	// columns, with room for the logo too.
	for _, h := range []int{12, 40} {
		f := ansi.Strip(frame(t, "en", 40, h, "?"))
		if first, next := lineWith(f, "stop it"), lineAfter(f, "   s "); strings.Index(next, "container") != strings.Index(first, "stop it") {
			t.Errorf("%d rows: %q under %q", h, next, first)
		}
	}
	// From 20 rows the logo is back.
	if f := frame(t, "en", 80, 24, "?"); !strings.Contains(f, "▄") {
		t.Errorf("no logo at 80x24:\n%s", ansi.Strip(f))
	}
	// History at 60 columns: the results whole, without the year.
	f = ansi.Strip(frame(t, "ko", 60, 20, "h"))
	if !strings.Contains(f, "Work 정상") || strings.Contains(f, "2026-") {
		t.Errorf("%s", f)
	}
	// The next tests speak English.
	i18n.Set(i18n.English)
}

func TestTheOverviewFitsTheTerminal(t *testing.T) {
	// CLI mode's overview, as the snapshot prints it at a width: no line
	// wider than the terminal, which would wrap it under the commands, and
	// every command and what it does still there.
	for _, w := range []int{60, 80, 112} {
		c := api.Client{Run: cli.InProcess(func() (*app.App, error) { return nil, fmt.Errorf("the snapshot makes its own") })}
		var out, errOut strings.Builder
		if code, err := c.Run(t.Context(), []string{"--snapshot", fmt.Sprintf("%dx24", w), "--cli"}, &out, &errOut); err != nil || code != 0 {
			t.Fatalf("%d: %d %v %s", w, code, err, errOut.String())
		}
		for _, l := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
			if ansi.StringWidth(l) > w {
				t.Errorf("%d: a line %d wide: %q", w, ansi.StringWidth(l), ansi.Strip(l))
			}
		}
		// The words of each, wherever they wrap.
		words := strings.Join(strings.Fields(ansi.Strip(out.String())), " ")
		for _, want := range []string{"wsl-spawner set Work --managed", "set wsl-spawner up in it: its starts noted, its containers brought back", "open the screen"} {
			if !strings.Contains(words, want) {
				t.Errorf("%d: no %q in\n%s", w, want, ansi.Strip(out.String()))
			}
		}
	}
}

// lineWith is the first line of f that holds s.
func lineWith(f, s string) string {
	for _, l := range strings.Split(f, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

// lineAfter is the line after the first line of f that starts with s.
func lineAfter(f, s string) string {
	lines := strings.Split(f, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, s) && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	return ""
}

func TestANewInstallSaysItHasNoStopOrStartYet(t *testing.T) {
	// No history yet, as right after an install: the newest stops and
	// starts say there are none, rather than leave the rows blank.
	a, _, done, err := demo.New(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	if err := os.Remove(filepath.Join(a.Dir, "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "ko"} {
		i18n.Set(i18n.Lang(lang))
		c := api.Client{Run: cli.InProcess(func() (*app.App, error) { return a, nil }), Lang: lang}
		f := ansi.Strip(tui.Snapshot(c, 120, 30, true, nil, demo.Now))
		// The section's title, and what it says of none.
		want := map[string][2]string{"en": {"Recent", "no stop or start yet"}, "ko": {"최근", "아직 멈추거나 켠 기록 없음"}}[lang]
		if !strings.Contains(f, want[0]) || !strings.Contains(f, want[1]) {
			t.Errorf("%s: no %q under %q in\n%s", lang, want[1], want[0], f)
		}
	}
	i18n.Set(i18n.Lang("en"))
}

func TestTheNewestStopsStayBesideTheDetailsInKoreanToo(t *testing.T) {
	// The right column holds the containers and, under them, the newest
	// stops and starts, as many as fit: a section that does not fit whole
	// goes, but these lose their oldest rows instead.
	for _, c := range []struct {
		lang, title string
		w, h        int
	}{{"ko", "최근", 120, 30}, {"en", "Recent", 140, 40}, {"ko", "최근", 140, 40}} {
		f := ansi.Strip(frame(t, c.lang, c.w, c.h, ""))
		left := strings.Join(tui.LeftPane(f), "\n")
		if !strings.Contains(left, "\n   "+c.title) || len(tui.RightPane(f)) < c.h-6 {
			t.Errorf("%s %dx%d: the newest stops are not under the table with the details beside:\n%s", c.lang, c.w, c.h, f)
		}
	}
	i18n.Set(i18n.English)
}

func TestASmallScreenKeepsHowTheLastStopWentAndNoHeadingAlone(t *testing.T) {
	// At 60x20 the selected distribution's last stop has room for its
	// heading and its first line, which says how it went: shown, not given
	// up whole for the PC's recent stops. And at every small size no
	// heading stands with nothing under it.
	f := ansi.Strip(frame(t, "en", 60, 20, ""))
	if !strings.Contains(f, "Last stop") || !strings.Contains(f, "clean 11-11 22:36") {
		t.Errorf("60x20: the last stop is gone:\n%s", f)
	}
	for _, s := range [][2]int{{60, 18}, {60, 20}, {80, 24}, {100, 30}} {
		for _, keys := range []string{"", "down", "down down"} {
			lines := strings.Split(ansi.Strip(frame(t, "en", s[0], s[1], keys)), "\n")
			for i, l := range lines {
				title := strings.TrimSpace(l)
				if title != "Choices" && title != "Last stop" && title != "Next stop" && title != "Recent" {
					continue
				}
				if i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) == "" {
					t.Errorf("%dx%d %q: %s stands alone:\n%s", s[0], s[1], keys, title, strings.Join(lines, "\n"))
				}
			}
		}
	}
}
