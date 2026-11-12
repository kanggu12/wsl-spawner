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
	}{
		// Short in the table, whole in the details.
		{i18n.English, "update", "ended without its time: WSL update " + at},
		{i18n.English, "update", "ended without its time: WSL was updated " + at},
		{i18n.English, "outside", "ended without its time: stopped outside " + at},
		{i18n.English, "outside", "ended without its time: stopped outside wsl-spawner " + at},
		{i18n.Korean, "crash", "시간 없이 끝남: 비정상 종료 " + at},
		{i18n.Korean, "crash", "시간 없이 끝남: Windows가 정상 종료되지 않음 " + at},
		// Untold, or a cause this screen has no words for: when the start
		// it ended began, as before.
		{i18n.English, "", "ended without its time (the start of " + began + ")"},
		{i18n.English, "volcano", "ended without its time (the start of " + began + ")"},
	} {
		i18n.Set(c.lang)
		f := ansi.Strip(mainWith(140, 34, 0, cutDistro(c.cause)).View().Content)
		if !strings.Contains(f, c.want) {
			t.Errorf("%s %q: no %q in\n%s", c.lang, c.cause, c.want, f)
		}
	}
}

func TestTheScreenSaysASignOut(t *testing.T) {
	at := time.Date(2025, 3, 4, 21, 29, 34, 0, time.UTC).Local().Format("01-02 15:04")
	was := i18n.Current()
	t.Cleanup(func() { i18n.Set(was) })
	for lang, wants := range map[i18n.Lang][]string{
		i18n.English: {"ended without its time: sign-out " + at, "ended without its time: signed out of Windows " + at},
		i18n.Korean:  {"시간 없이 끝남: 로그아웃 " + at, "시간 없이 끝남: Windows에서 로그아웃함 " + at},
	} {
		i18n.Set(lang)
		f := ansi.Strip(mainWith(140, 34, 0, cutDistro("signout")).View().Content)
		for _, want := range wants {
			if !strings.Contains(f, want) {
				t.Errorf("%s: no %q in\n%s", lang, want, f)
			}
		}
	}
}
