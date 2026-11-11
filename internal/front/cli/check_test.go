package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/front/api"

	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
)

// checkPC gives a a made-up PC with its sign-out scripts, sign-in program,
// .wslconfig and kept ports.
func checkPC(t *testing.T, a *app.App, scripts string) {
	t.Helper()
	newPolicy(t, a, scripts, false)
	a.PC.ReadFile = func(p string) ([]byte, error) {
		switch {
		case strings.HasSuffix(p, ".wslconfig"):
			return []byte("[wsl2]\nnetworkingMode=mirrored\n"), nil
		case strings.HasSuffix(p, "old-hook.cmd"):
			return []byte("wsl.exe --shutdown\r\n"), nil
		}
		return nil, errors.New("no such file")
	}
	a.PC.ExcludedPorts = func(proto string) (string, error) {
		if proto == "tcp" {
			return "     50000       50059\n", nil
		}
		return "", nil
	}
	a.PC.HomeDir = func() (string, error) { return `C:\Users\Ann`, nil }
	a.PC.LogonEntry = func(string) (string, bool, bool, error) { return "", false, false, nil }
}

func TestCheckSaysEachFindingAndWhatToDo(t *testing.T) {
	a, _ := pc(t)
	checkPC(t, a, "[Logoff]\r\n0CmdLine=C:\\Tools\\old-hook.cmd\r\n0Parameters=\r\n")
	out, _, code := cli(t, a, false, "check")
	// Nothing grave on this PC: a hook missing is worth a look.
	if code != exitOK {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{
		"Stopping\n",
		"! no sign-out hook",
		"→ wsl-spawner hook install",
		"! another sign-out script stops WSL before wsl-spawner's would: C:\\Tools\\old-hook.cmd",
		"Ubuntu-24.04\n",
		"Sign-in\n",
		"· .wslconfig: networkingMode=mirrored",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	// No word is a key, and no value is left over or missing.
	for _, bad := range []string{"%!", "check.", "(MISSING)", "EXTRA"} {
		if strings.Contains(out, bad) {
			t.Fatalf("%q in\n%s", bad, out)
		}
	}
}

func TestCheckSaysTheHistorysLastStopOfAStoppedDistribution(t *testing.T) {
	// Debian is stopped, so nothing inside it is read: the history's stop of
	// it says how its last start ended, as the screen's check says it.
	a, _ := pc(t)
	checkPC(t, a, "")
	at := time.Date(2026, 11, 10, 22, 36, 0, 0, time.Local)
	if err := history.Open(a.Dir).Add(history.Entry{At: at, What: "stop", By: "signout", Distros: []history.Distro{{Name: "Debian", Result: "clean"}}}); err != nil {
		t.Fatal(err)
	}
	out, _, _ := cli(t, a, false, "check")
	if !strings.Contains(out, "✓ the history holds wsl-spawner's last stop of it: clean (2026-11-10 22:36)") {
		t.Fatalf("%s", out)
	}
}

func TestCheckJSONAndAGraveFindingExitsThree(t *testing.T) {
	a, _ := pc(t)
	checkPC(t, a, "")
	// The settings start a distribution at sign-in whose program is gone.
	a.Config.Logon = []string{"Ubuntu-24.04"}
	out, _, code := cli(t, a, false, "check", "--json")
	if code != exitAttention {
		t.Fatalf("exit %d\n%s", code, out)
	}
	var got api.Check
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Worst != "bad" {
		t.Fatalf("%v %+v", err, got)
	}
	found := false
	for _, g := range got.Groups {
		for _, it := range g.Items {
			if it.Key == "s.noentry" && it.Mark == "bad" && strings.Contains(it.Do, "wsl-spawner set Ubuntu-24.04 --logon") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("%s", out)
	}
}

func TestEveryCheckWordIsInBothLanguages(t *testing.T) {
	// The keys the check can give, from its source, each in English and
	// Korean with the same values.
	for _, k := range checkKeys(t) {
		if !i18n.Has(i18n.English, k) || !i18n.Has(i18n.Korean, k) {
			t.Errorf("%s: not in both catalogs", k)
			continue
		}
		i18n.Set(i18n.English)
		en := i18n.T(k)
		i18n.Set(i18n.Korean)
		ko := i18n.T(k)
		if strings.Count(en, "%") != strings.Count(ko, "%") {
			t.Errorf("%s: %q and %q take different values", k, en, ko)
		}
	}
	i18n.Set(i18n.English)
}

// checkKeys are the words' keys the check's source names: every quoted
// "check.…" in it.
func checkKeys(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "engine", "check", "check.go"))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, m := range regexp.MustCompile(`"(check\.[a-z0-9.]+)"`).FindAllStringSubmatch(string(b), -1) {
		if !slices.Contains(keys, m[1]) {
			keys = append(keys, m[1])
		}
	}
	// A count too small means the source was not read as it is.
	if len(keys) < 50 {
		t.Fatalf("only %d keys found", len(keys))
	}
	return keys
}
