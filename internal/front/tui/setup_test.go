package tui_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/app/demo"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/cli"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/front/tui"
	"github.com/kanggu12/wsl-spawner/internal/front/ui"
	"github.com/kanggu12/wsl-spawner/internal/machine/gpo"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// wizardOn is a frame of the wizard on the demo's PC after keys, with the
// App it ran on.
func wizardOn(t *testing.T, lang string, w, h int, keys string) (string, *app.App) {
	t.Helper()
	a, _, done, err := demo.New(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	// The settings speak the language, as the snapshot's do: the wizard
	// shows the language the settings choose, and "auto" is English here.
	a.Config.Language = lang
	i18n.Set(i18n.Lang(lang))
	t.Cleanup(func() { i18n.Set(i18n.English) })
	c := api.Client{Run: cli.InProcess(func() (*app.App, error) { return a, nil }), Lang: lang}
	return tui.SetupSnapshot(c, w, h, true, tui.ParseKeys(keys)), a
}

// wizardPages are the wizard's pages, as the keys that reach them.
var wizardPages = map[string]string{
	"welcome": "", "language": "enter", "colors": "enter enter", "mode": "enter enter enter",
	"distros": "enter enter enter enter", "hook": "enter enter enter enter enter",
	"confirm": "enter enter enter enter enter enter", "applied": "enter enter enter enter enter enter enter",
}

func TestEveryWizardPageFitsEverySize(t *testing.T) {
	for _, lang := range []string{"en", "ko"} {
		for name, keys := range wizardPages {
			for _, s := range sizes {
				f, _ := wizardOn(t, lang, s[0], s[1], keys)
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
				if m := leak.FindString(ansi.Strip(f)); m != "" || strings.Contains(f, "wiz.") {
					t.Errorf("%s %s %dx%d: a key or a mistake on screen: %q", lang, name, s[0], s[1], m)
				}
			}
		}
	}
}

func TestTheWizardMakesWhatWasChosen(t *testing.T) {
	// Korean (one down from English), light, the overview; Work set up and
	// kept running; the hook is installed on the demo's PC already.
	keys := "enter down enter down down enter down enter down down space space enter enter enter"
	f, a := wizardOn(t, "en", 100, 30, keys)
	plain := ansi.Strip(f)
	for _, want := range []string{"✓", "Work"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("no %q in\n%s", want, plain)
		}
	}
	c, err := config.Load(a.ConfigPath)
	if err != nil || c.Language != "ko" || c.Theme != "light" || c.Mode != "cli" {
		t.Fatalf("%+v %v\n%s", c, err, plain)
	}
	// Work: set up and kept running, on the made-up PC.
	st, err := api.Client{Run: cli.InProcess(func() (*app.App, error) { return a, nil })}.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range st.List {
		if d.Name == "Work" && (d.Probe == nil || !d.Probe.SetUp || !d.Probe.KeptRunning) {
			t.Fatalf("Work %+v", d.Probe)
		}
	}
}

func TestTheWizardInstallsTheHookWhenChosen(t *testing.T) {
	a, w, done, err := demo.New(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	// No hook on this made-up PC yet.
	text, _ := a.PC.ReadScripts()
	if err := a.PC.UninstallHook(gpo.Logoff(text)[1].CmdLine); err != nil {
		t.Fatal(err)
	}
	_ = w
	c := api.Client{Run: cli.InProcess(func() (*app.App, error) { return a, nil }), Lang: "en"}
	f := tui.SetupSnapshot(c, 100, 30, true, tui.ParseKeys("enter enter enter enter enter enter enter"))
	text, _ = a.PC.ReadScripts()
	if s := gpo.Logoff(text); len(s) != 2 || !strings.HasSuffix(s[1].CmdLine, "wsl-spawnerw.exe") {
		t.Fatalf("%+v\n%s", s, ansi.Strip(f))
	}
}

func TestTheWizardSnapshotPressesWhatTheProgramDoes(t *testing.T) {
	// The key names a picture is made with, and the page each lands on.
	for keys, want := range map[string]string{
		// left goes back a page, as the arrow does, not on as l.
		wizardPages["distros"] + " left left": "3 of 7 · Colors",
		// right and tab go on, backspace and shift+tab back.
		"right tab backspace":   "2 of 7 · Language",
		"tab tab tab shift+tab": "3 of 7 · Colors",
		// A name no key has presses nothing, not its first letter.
		"enter lefft": "2 of 7 · Language",
	} {
		f, _ := wizardOn(t, "en", 100, 22, keys)
		if plain := ansi.Strip(f); !strings.Contains(plain, want) {
			t.Errorf("%q: no %q in\n%s", keys, want, plain)
		}
	}
}

func TestAPictureInASCIIMarksHasNothingElse(t *testing.T) {
	// In ASCII marks every character is ASCII but the words' own letters:
	// a mark an East Asian console draws two cells wide pushes a line off.
	ui.SetMarks(ui.MarksASCII)
	t.Cleanup(func() { ui.SetMarks(ui.MarksUnicode) })
	// check reports the first character of f that is neither.
	check := func(what, f string) {
		for _, r := range ansi.Strip(f) {
			// Hangul is a word's own; anything else past ASCII is a mark.
			if r > 0x7e && !unicode.Is(unicode.Hangul, r) {
				t.Errorf("%s: %q in\n%s", what, r, ansi.Strip(f))
				return
			}
		}
	}
	for _, lang := range []string{"en", "ko"} {
		// Every page of the wizard, as the program draws it.
		for name, keys := range wizardPages {
			f, _ := wizardOn(t, lang, 100, 30, keys)
			check(lang+" wizard "+name, f)
		}
		// Every state of the screen, and the line a too small one shows.
		for name, keys := range states {
			check(lang+" "+name, frame(t, lang, 120, 30, keys))
		}
		check(lang+" too small", frame(t, lang, 39, 11, ""))
	}
}

func TestTheColorsPageExplainsTheChoiceItShows(t *testing.T) {
	// The question explains the first choice by the name the list gives it:
	// a name the list does not show reads as a choice that is missing.
	for _, lang := range []string{"en", "ko"} {
		f, _ := wizardOn(t, lang, 100, 22, wizardPages["colors"])
		// The question is the first line that asks.
		q := lineWith(ansi.Strip(f), "?")
		// The choice's own name, in the language the page speaks; it may
		// start the sentence, in a capital.
		if name := i18n.T("wiz.theme.auto"); !strings.Contains(strings.ToLower(q), strings.ToLower(name)+":") {
			t.Errorf("%s: the question %q does not name %q", lang, q, name)
		}
	}
}

func TestTheWizardSaysItsStepsPlainly(t *testing.T) {
	// Debian, stopped, set up: each level includes the ones before it, and
	// the wizard starts Debian to make the change.
	// Wide enough for the question on one line.
	f, _ := wizardOn(t, "en", 160, 30, wizardPages["distros"]+" down space")
	plain := ansi.Strip(f)
	for _, want := range []string{"each includes the ones before it", "Debian", "starts it to make the change"} {
		// Each is a phrase the old words lacked or said unclearly.
		if !strings.Contains(plain, want) {
			t.Errorf("no %q in\n%s", want, plain)
		}
	}
	// In Korean the colors stand after a colon of their own, as a value:
	// "색 터미널 따라" read as one odd phrase.
	f, _ = wizardOn(t, "ko", 120, 30, wizardPages["confirm"])
	if plain := ansi.Strip(f); !strings.Contains(plain, "색: 터미널 따라") {
		t.Errorf("the settings line in\n%s", plain)
	}
}
