package tui_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/app/demo"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/cli"
	"github.com/kanggu12/wsl-spawner/internal/front/tui"
	"github.com/kanggu12/wsl-spawner/internal/machine/gpo"
)

func TestTheWizardTakesAHookWindowsHasNotReadYetAsDone(t *testing.T) {
	// gpupdate fails, as on a laptop of a domain away from it: hook install
	// ends with 3, the hook in the policy's files. The step is done, with
	// the command's words as its note, not a failure: setup run again
	// offers no hook step.
	a, _, done, err := demo.New(true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	// No hook on the made-up PC yet, and Windows does not read its policy.
	text, _ := a.PC.ReadScripts()
	if err := a.PC.UninstallHook(gpo.Logoff(text)[1].CmdLine); err != nil {
		t.Fatal(err)
	}
	a.PC.RefreshPolicy = func() error {
		return errors.New("gpupdate: exit status 1: User policy could not be updated successfully")
	}
	c := api.Client{Run: cli.InProcess(func() (*app.App, error) { return a, nil }), Lang: "en"}
	f := ansi.Strip(tui.SetupSnapshot(c, 100, 30, true, tui.ParseKeys("enter enter enter enter enter enter enter")))
	if !strings.Contains(f, "✓ the sign-out hook") || strings.Contains(f, "✗") || !strings.Contains(f, "      sign-out hook installed, but Windows has not read it yet") {
		t.Fatalf("%s", f)
	}
	// It is in, last.
	text, _ = a.PC.ReadScripts()
	if s := gpo.Logoff(text); len(s) != 2 || !strings.HasSuffix(s[1].CmdLine, "wsl-spawnerw.exe") {
		t.Fatalf("%+v", s)
	}
}
