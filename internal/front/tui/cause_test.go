package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
)

// cutDistro is a set-up distribution whose last start was cut, by cause at
// a made-up moment; "" is a cause the commands did not tell.
func cutDistro(cause string) api.Distro {
	began := time.Date(2025, 3, 4, 9, 0, 0, 0, time.UTC)
	at := time.Date(2025, 3, 4, 21, 29, 34, 0, time.UTC)
	end := api.End{How: "cut", Start: &began}
	if cause != "" {
		end.Cause, end.CauseAt = cause, &at
	}
	return api.Distro{Name: "Ubuntu-24.04", Running: true, Kind: "set-up", Version: 2,
		Probe: &api.Probe{SetUp: true, KeptRunning: true, Systemd: "running", LastEnd: end}}
}

func TestTheScreenSaysWhatEndedACutStart(t *testing.T) {
	at := time.Date(2025, 3, 4, 21, 29, 34, 0, time.UTC).Local().Format("01-02 15:04")
	began := time.Date(2025, 3, 4, 9, 0, 0, 0, time.UTC).Local().Format("01-02 15:04")
	was := i18n.Current()
	t.Cleanup(func() { i18n.Set(was) })
	for _, c := range []struct {
		lang        i18n.Lang
		cause, want string
		// table: said in the table's row, where the screen is one column;
		// else in the details beside the table on a wide screen, which may
		// wrap it.
		table bool
	}{
		// Short in the table, whole in the details.
		{i18n.English, "update", "ended without its time: WSL update " + at, true},
		{i18n.English, "update", "ended without its time: WSL was updated " + at, false},
		{i18n.English, "outside", "ended without its time: stopped outside " + at, true},
		{i18n.English, "outside", "ended without its time: stopped outside wsl-spawner " + at, false},
		{i18n.Korean, "crash", "시간 없이 끝남: 비정상 종료 " + at, true},
		{i18n.Korean, "crash", "시간 없이 끝남: Windows가 정상 종료되지 않음 " + at, false},
		// Untold, or a cause this screen has no words for: when the start
		// it ended began, as before.
		{i18n.English, "", "ended without its time (the start of " + began + ")", false},
		{i18n.English, "volcano", "ended without its time (the start of " + began + ")", false},
	} {
		i18n.Set(c.lang)
		if c.table {
			f := ansi.Strip(mainWith(100, 34, 0, cutDistro(c.cause)).View().Content)
			if !strings.Contains(lineWith(f, "▪ Ubuntu-24.04 "), c.want) {
				t.Errorf("%s %q: no %q in the row of\n%s", c.lang, c.cause, c.want, f)
			}
			continue
		}
		f := ansi.Strip(mainWith(140, 34, 0, cutDistro(c.cause)).View().Content)
		if !strings.Contains(PaneText(f), c.want) {
			t.Errorf("%s %q: no %q in the details of\n%s", c.lang, c.cause, c.want, f)
		}
		// The wide table says how it ended in its word.
		if row := TableRow(f, "Ubuntu-24.04"); !strings.Contains(row, strings.SplitN(c.want, ":", 2)[0][:10]) {
			t.Errorf("%s %q: the row %q", c.lang, c.cause, row)
		}
	}
}

func TestTheScreenSaysASignOut(t *testing.T) {
	at := time.Date(2025, 3, 4, 21, 29, 34, 0, time.UTC).Local().Format("01-02 15:04")
	was := i18n.Current()
	t.Cleanup(func() { i18n.Set(was) })
	for lang, wants := range map[i18n.Lang][2]string{
		i18n.English: {"ended without its time: sign-out " + at, "ended without its time: signed out of Windows " + at},
		i18n.Korean:  {"시간 없이 끝남: 로그아웃 " + at, "시간 없이 끝남: Windows에서 로그아웃함 " + at},
	} {
		i18n.Set(lang)
		// Short in the table's row, where the screen is one column.
		if f := ansi.Strip(mainWith(100, 34, 0, cutDistro("signout")).View().Content); !strings.Contains(lineWith(f, "▪ Ubuntu-24.04 "), wants[0]) {
			t.Errorf("%s: no %q in the row of\n%s", lang, wants[0], f)
		}
		// Whole in the details beside the table on a wide screen.
		if f := ansi.Strip(mainWith(140, 34, 0, cutDistro("signout")).View().Content); !strings.Contains(PaneText(f), wants[1]) {
			t.Errorf("%s: no %q in the details of\n%s", lang, wants[1], f)
		}
	}
}
