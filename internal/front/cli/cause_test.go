package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/machine/eventlog"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
)

// The made-up moments of a start that was cut: it began, its journal saw it
// asked to power off, and the start after it, which runs now, began.
var (
	cutBegan = time.Date(2025, 3, 4, 9, 0, 0, 0, time.UTC)
	cutEnded = time.Date(2025, 3, 4, 21, 30, 7, 0, time.UTC)
	nowBegan = time.Date(2025, 3, 5, 8, 0, 0, 0, time.UTC)
)

// cutPC is the made-up PC with Ubuntu-24.04's last start cut: two starts
// noted, no stop of wsl-spawner's. journal is what its journal holds of the
// cut start; endings and endErr what Windows' logs answer, and asked, the
// moments they were asked between.
func cutPC(t *testing.T, journal string, endings []eventlog.Ending, endErr error) (a *app.App, asked *[2]time.Time) {
	t.Helper()
	a, f := pc(t)
	asked = new([2]time.Time)
	probe := fmt.Sprintf("probe=1\nstart_script=%s%s\nnow=%d\nboot_id=0e2a1d5c-0b8e-4c1a-9f3e-7d6c5b4a3921\ninit_start=250\nuptime=600\n"+
		"run=%d 5f1c0d2e-3a4b-4c5d-8e9f-0a1b2c3d4e5f@106\nrun=%d 0e2a1d5c-0b8e-4c1a-9f3e-7d6c5b4a3921@250\nend=1\n",
		inside.StartScriptMark, inside.StartScriptVersion, nowBegan.Add(598*time.Second).Unix(), cutBegan.Unix(), nowBegan.Unix())
	f.Script = func(distro, user, script string) (string, error) {
		switch {
		case script == inside.ProbeScript() && distro == "Ubuntu-24.04":
			return probe, nil
		case script == inside.ProbeScript():
			return "probe=1\nend=1\n", nil
		case strings.Contains(script, "journalctl"):
			// The cut start's boot, and before this start began.
			if user != "root" || !strings.Contains(script, "-b 5f1c0d2e3a4b4c5d8e9f0a1b2c3d4e5f ") {
				return "", errors.New("asked for another boot")
			}
			return journal, nil
		}
		return "", errors.New("unexpected script")
	}
	a.PC.Endings = func(from, to time.Time) ([]eventlog.Ending, error) {
		asked[0], asked[1] = from, to
		return endings, endErr
	}
	// Windows' clock, as the distribution's probe reads its own.
	a.Clock = func() time.Time { return nowBegan.Add(598 * time.Second) }
	return a, asked
}

// poweredOffAt is the journal's last lines of a start WSL asked to power off at t.
func poweredOffAt(t time.Time) string {
	return fmt.Sprintf("%d.100000 box systemd[1]: Shutting down.\n%d.200000 box systemd-journald[67]: Journal stopped\n", t.Unix(), t.Unix())
}

func TestStatusSaysWhatEndedACutStart(t *testing.T) {
	// An update of WSL begun half a minute before it stopped the start, and
	// ended after: its first event says when.
	update := cutEnded.Add(-33 * time.Second)
	a, asked := cutPC(t, poweredOffAt(cutEnded), []eventlog.Ending{{Kind: eventlog.Update, At: update}, {Kind: eventlog.Update, At: cutEnded.Add(34 * time.Second)}}, nil)
	out, _, code := cli(t, a, false, "status", "--json")
	if code != exitOK {
		t.Fatalf("exit %d\n%s", code, out)
	}
	var st api.Status
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}
	var end api.End
	for _, d := range st.List {
		if d.Name == "Ubuntu-24.04" && d.Probe != nil {
			end = d.Probe.LastEnd
		}
	}
	if end.How != "cut" || end.Cause != "update" || end.CauseAt == nil || !end.CauseAt.Equal(update) {
		t.Fatalf("got %+v\n%s", end, out)
	}
	// Windows' logs were read from a little before the journal saw it end
	// to the start that runs now.
	// The journal's last line came 0.2 s into that second; this start's
	// init began 2.5 s after a boot 600 s before the probe, 598 s into it.
	if !asked[0].Equal(cutEnded.Add(200*time.Millisecond-inside.AskedBefore)) || !asked[1].Equal(nowBegan.Add(500*time.Millisecond)) {
		t.Fatalf("asked %v", *asked)
	}
	// The table says it in words, in local time.
	out, _, _ = cli(t, a, false, "status")
	if want := "ended without its time: WSL update " + update.Local().Format("01-02 15:04"); !strings.Contains(out, want) {
		t.Fatalf("no %q in\n%s", want, out)
	}
}

func TestCheckSaysWhatEndedACutStartOnALineOfItsOwn(t *testing.T) {
	a, _ := cutPC(t, poweredOffAt(cutEnded), nil, nil)
	checkPC(t, a, "")
	out, _, _ := cli(t, a, false, "check", "--json")
	var ch api.Check
	if err := json.Unmarshal([]byte(out), &ch); err != nil {
		t.Fatal(err)
	}
	// The cut as before, then what ended it: nothing in Windows' logs, so
	// it was stopped outside wsl-spawner, as the journal saw it end.
	var keys []string
	text := ""
	for _, g := range ch.Groups {
		if g.Name != "Ubuntu-24.04" {
			continue
		}
		for _, it := range g.Items {
			keys = append(keys, it.Mark+" "+it.Key)
			if it.Key == "d.end.cause.outside" {
				text = it.Text
			}
		}
	}
	got := strings.Join(keys, ", ")
	if !strings.Contains(got, "bad d.end.cut, info d.end.cause.outside") {
		t.Fatalf("got %s", got)
	}
	if want := "it was stopped at " + cutEnded.Local().Format("2006-01-02 15:04") + " outside wsl-spawner"; !strings.HasPrefix(text, want) {
		t.Fatalf("got %q", text)
	}
}

func TestACutStartsCauseUntoldIsSaidAsBefore(t *testing.T) {
	for name, a := range map[string]*app.App{
		// Windows' logs could not be read, or the journal holds nothing of
		// that start: its cut is said as it was, with when it began.
		"logs unread":   first(cutPC(t, poweredOffAt(cutEnded), nil, errors.New("access denied"))),
		"journal empty": first(cutPC(t, "", []eventlog.Ending{{Kind: eventlog.Update, At: cutEnded}}, nil)),
	} {
		out, _, _ := cli(t, a, false, "status", "--json")
		if strings.Contains(out, `"cause"`) {
			t.Errorf("%s: a cause in\n%s", name, out)
		}
		out, _, _ = cli(t, a, false, "status")
		if !strings.Contains(out, "ended without its time (the start of "+cutBegan.Local().Format("01-02 15:04")+")") {
			t.Errorf("%s:\n%s", name, out)
		}
	}
}

// first is the first of two values, for a map of the PCs a test makes.
func first(a *app.App, _ *[2]time.Time) *app.App { return a }

// The made-up PC's other distributions are no starts to explain: only the
// one whose last start was cut is asked.
func TestOnlyACutStartIsLookedInto(t *testing.T) {
	a, _ := cutPC(t, poweredOffAt(cutEnded), nil, nil)
	f := a.WSL.(*wsl.Fake)
	cli(t, a, false, "status", "--json")
	n := 0
	for _, c := range f.CallList() {
		if c == "run:Ubuntu-24.04" {
			n++
		}
	}
	// The probe and the journal's read, once each.
	if n != 2 {
		t.Fatalf("calls %v", f.CallList())
	}
}

// probeNow is the made-up distribution's clock as its probe reads it.
var probeNow = nowBegan.Add(598 * time.Second)

func TestACutIsJudgedOnWindowsClock(t *testing.T) {
	// The distribution's clock runs two minutes ahead of Windows': its
	// journal's moment is set back by that before Windows' logs are read.
	ahead := 2 * time.Minute
	ended := cutEnded.Add(-ahead)
	a, asked := cutPC(t, poweredOffAt(cutEnded), []eventlog.Ending{{Kind: eventlog.Update, At: ended.Add(-33 * time.Second)}, {Kind: eventlog.Update, At: ended.Add(34 * time.Second)}}, nil)
	a.Clock = func() time.Time { return probeNow.Add(-ahead) }
	out, _, _ := cli(t, a, false, "status", "--json")
	var st api.Status
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}
	for _, d := range st.List {
		if d.Name == "Ubuntu-24.04" && (d.Probe == nil || d.Probe.LastEnd.Cause != "update") {
			t.Fatalf("got %+v, read from %v", d.Probe, *asked)
		}
	}
}

func TestASignOutWithoutTheHookIsSaid(t *testing.T) {
	a, _ := cutPC(t, poweredOffAt(cutEnded), []eventlog.Ending{{Kind: "signout", At: cutEnded.Add(-5 * time.Second)}}, nil)
	a.Clock = func() time.Time { return probeNow }
	checkPC(t, a, "")
	out, _, _ := cli(t, a, false, "check")
	if want := "· signed out of Windows at " + cutEnded.Add(-5*time.Second).Local().Format("2006-01-02 15:04") + " without wsl-spawner's stop"; !strings.Contains(out, want) {
		t.Fatalf("no %q in\n%s", want, out)
	}
	out, _, _ = cli(t, a, false, "status")
	if want := "ended without its time: sign-out " + cutEnded.Add(-5*time.Second).Local().Format("01-02 15:04"); !strings.Contains(out, want) {
		t.Fatalf("no %q in\n%s", want, out)
	}
}
