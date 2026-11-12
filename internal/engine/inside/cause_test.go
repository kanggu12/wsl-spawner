package inside

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/machine/eventlog"
)

// The lines below are shaped as a distribution's journal gives them in
// journalctl's short-unix form, copied once from a real one's; the times,
// the host and the numbers are made up.
const (
	// systemd's power off, to its end: WSL asked for it.
	poweredOff = "1741068000.100000 box systemd[1]: Reached target System Power Off.\n" +
		"1741068000.200000 box systemd[1]: Shutting down.\n" +
		"1741068000.300000 box systemd-journald[67]: Journal stopped\n"
	// The power off did not end in its time, and WSL ended the rest.
	powerOffCut = "1741068000.100000 box systemd[1]: Stopped Snap Daemon.\n" +
		"1741068007.000000 box unknown: WSL (2 - init-systemd(Box)) ERROR: InitTerminateInstanceInternal:2901: systemctl poweroff did not terminate in time\n"
	// Nothing about an end: the last line before Windows went down.
	quiet = "1741068000.000000 box systemd[453]: Finished Something.\n" +
		"1741068030.500000 box kernel: sched: DL replenish lagged too much\n"
)

func TestParseSeenTakesTheLastMomentAndWhetherItWasAsked(t *testing.T) {
	for _, c := range []struct {
		name, out string
		at        time.Time
		asked     bool
	}{
		{"powered off", poweredOff, time.Unix(1741068000, 300000000), true},
		{"power off cut short", powerOffCut, time.Unix(1741068007, 0), true},
		{"no word", quiet, time.Unix(1741068030, 500000000), false},
	} {
		s, ok := ParseSeen(c.out)
		if !ok || !s.At.Equal(c.at) || s.Asked != c.asked {
			t.Errorf("%s: got %+v %v", c.name, s, ok)
		}
	}
	// Nothing printed, or nothing with a moment: not seen.
	for _, out := range []string{"", "-- No entries --\n", "garbage\n"} {
		if s, ok := ParseSeen(out); ok {
			t.Errorf("%q: seen %+v", out, s)
		}
	}
}

func TestWhyCutPutsEachEndingWithTheJournal(t *testing.T) {
	end := time.Unix(1741068000, 0)
	asked := Seen{At: end, Asked: true}
	abrupt := Seen{At: end}
	at := func(d time.Duration) time.Time { return end.Add(d) }
	for _, c := range []struct {
		name    string
		seen    Seen
		endings []eventlog.Ending
		kind    string
		when    time.Time
	}{
		// An update begins a little before WSL stops the distributions,
		// and notes its end after: the first of its events says when.
		{"an update", asked, []eventlog.Ending{{Kind: eventlog.Update, At: at(-33 * time.Second)}, {Kind: eventlog.Update, At: at(34 * time.Second)}},
			"update", at(-33 * time.Second)},
		{"a shutdown", asked, []eventlog.Ending{{Kind: eventlog.Shutdown, At: at(-10 * time.Second)}, {Kind: eventlog.Shutdown, At: at(12 * time.Second)}},
			"shutdown", at(-10 * time.Second)},
		// An update that was over a while before it stopped did not stop
		// it: an update stops WSL while it installs.
		{"an update over before", asked, []eventlog.Ending{{Kind: eventlog.Update, At: at(-150 * time.Second)}, {Kind: eventlog.Update, At: at(-120 * time.Second)}},
			"outside", end},
		// One whose only event came just after.
		{"an update's end just after", asked, []eventlog.Ending{{Kind: eventlog.Update, At: at(20 * time.Second)}},
			"update", at(20 * time.Second)},
		// Windows shut down long after the distribution was powered off:
		// that is not what ended it, and nothing else in the logs did.
		{"a shutdown much later", asked, []eventlog.Ending{{Kind: eventlog.Shutdown, At: at(50 * time.Minute)}},
			"outside", end},
		// A crash after a power off is not what ended it either.
		{"a crash after a power off", asked, []eventlog.Ending{{Kind: eventlog.Crash, At: at(2 * time.Hour)}},
			"outside", end},
		// Without a word, the crash that came after its last line did,
		// however quiet it was before: at Windows' last stamp of being
		// alive (6008's moment), logged at the boot after with 41.
		{"a crash", abrupt, []eventlog.Ending{{Kind: eventlog.Crash, At: at(26 * time.Second), Moment: true}, {Kind: eventlog.Crash, At: at(80 * time.Second)}},
			"crash", at(26 * time.Second)},
		{"a crash hours after its last line", abrupt, []eventlog.Ending{{Kind: eventlog.Crash, At: at(3 * time.Hour), Moment: true}, {Kind: eventlog.Crash, At: at(3*time.Hour + time.Minute)}},
			"crash", at(3 * time.Hour)},
		// Only what Windows logged at the boot: when it went down is not
		// known, but that it was after the start's last line.
		{"a crash logged at the boot alone", abrupt, []eventlog.Ending{{Kind: eventlog.Crash, At: at(3 * time.Hour)}},
			"crash", end},
		// A crash before its last line came before the start.
		{"a crash before its last line", abrupt, []eventlog.Ending{{Kind: eventlog.Crash, At: at(-time.Hour)}},
			"outside", end},
		// A shutdown whose power off journald did not write.
		{"a shutdown without a word", abrupt, []eventlog.Ending{{Kind: eventlog.Shutdown, At: at(20 * time.Second)}},
			"shutdown", at(20 * time.Second)},
		{"nothing in the logs", asked, nil, "outside", end},
	} {
		got := WhyCut(c.seen, c.endings)
		if got.Kind != c.kind || !got.At.Equal(c.when) {
			t.Errorf("%s: got %s at %v, want %s at %v", c.name, got.Kind, got.At, c.kind, c.when)
		}
	}
}

// cut is a set-up distribution's probe whose last start ended cut: two
// starts noted, no stop of wsl-spawner's.
func cut() Probe {
	now := time.Unix(1741100000, 0)
	return Probe{StartScript: StartScriptMark + "3", BootID: "0e2a1d5c-0b8e-4c1a-9f3e-7d6c5b4a3921", InitStart: "250",
		Now: now, Uptime: 600 * time.Second, Runs: []Run{
			{At: time.Unix(1741000000, 0), ID: "5f1c0d2e-3a4b-4c5d-8e9f-0a1b2c3d4e5f@106"},
			{At: now.Add(-590 * time.Second), ID: "0e2a1d5c-0b8e-4c1a-9f3e-7d6c5b4a3921@250"},
		}}
}

func TestCutStartIsTheCutStartsBootAndThisStartsBeginning(t *testing.T) {
	p := cut()
	boot, until, ok := p.CutStart()
	// The boot without its dashes; this start's init began 250 ticks of 100
	// a second after the kernel booted, 600 s before the probe ran.
	if !ok || boot != "5f1c0d2e3a4b4c5d8e9f0a1b2c3d4e5f" || !until.Equal(time.Unix(1741100000-600+2, 500000000)) {
		t.Fatalf("got %s %v %v", boot, until, ok)
	}
	// A start that ended clean has no cut start; nor has a probe without
	// its clock, or with a start ID that is not a boot's.
	clean := cut()
	clean.LastStop = &StopRecord{Run: clean.Runs[0].ID, At: time.Unix(1741000100, 0), Result: "clean"}
	noClock := cut()
	noClock.Now = time.Time{}
	odd := cut()
	odd.Runs[0].ID = "not-a-boot@1"
	for name, p := range map[string]Probe{"clean": clean, "no clock": noClock, "odd ID": odd} {
		if _, _, ok := p.CutStart(); ok {
			t.Errorf("%s: a cut start", name)
		}
	}
}

func TestSeenScriptAsksTheJournalForThatBootBeforeThisStart(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	// A journalctl that prints, as an entry of its own, the arguments it
	// was given.
	fake := filepath.Join(dir, "journalctl")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"1741068000.000000 box args[1]: $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := SeenScript("5f1c0d2e3a4b4c5d8e9f0a1b2c3d4e5f", time.Unix(1741099402, 500000000))
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "R=1", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	// The moment before this start, in whole seconds.
	want := "1741068000.000000 box args[1]: --no-pager -q -o short-unix -b 5f1c0d2e3a4b4c5d8e9f0a1b2c3d4e5f --until @1741099401 -n 12"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// A boot that is not one reads nothing.
	if s := SeenScript("$(reboot)", time.Now()); strings.Contains(s, "journalctl") {
		t.Fatalf("got %q", s)
	}
}

func FuzzParseSeen(f *testing.F) {
	// The journal's own lines, and lines it never prints, as seeds.
	f.Add(poweredOff)
	f.Add(powerOffCut)
	f.Add("1741068000.1 x\n-1.999999999999 y\n99999999999999999999.0 z\n\x00.\xff \n")
	f.Fuzz(func(t *testing.T, out string) {
		s, ok := ParseSeen(out)
		if !ok {
			return
		}
		// Whatever it read, judging it never fails, and says something.
		if c := WhyCut(s, []eventlog.Ending{{Kind: eventlog.Update, At: s.At}, {Kind: eventlog.Crash, At: s.At}}); c.Kind == "" {
			t.Fatalf("no cause for %+v", s)
		}
	})
}

func TestAnUpdateWhoseEndComesLaterStillEndedIt(t *testing.T) {
	// The install's span is all its events: one whose end is logged more
	// than a minute after it stopped the start is still what stopped it.
	end := time.Unix(1741068000, 0)
	got := WhyCut(Seen{At: end, Asked: true}, []eventlog.Ending{
		{Kind: eventlog.Update, At: end.Add(-33 * time.Second)}, {Kind: eventlog.Update, At: end.Add(94 * time.Second)}})
	if got.Kind != "update" || !got.At.Equal(end.Add(-33*time.Second)) {
		t.Fatalf("got %+v", got)
	}
	// Two installs a day apart are two spans: the later does not stretch
	// the first over a start that ended between them.
	got = WhyCut(Seen{At: end, Asked: true}, []eventlog.Ending{
		{Kind: eventlog.Update, At: end.Add(-150 * time.Second)}, {Kind: eventlog.Update, At: end.Add(-120 * time.Second)},
		{Kind: eventlog.Update, At: end.Add(24 * time.Hour)}})
	if got.Kind != CauseOutside {
		t.Fatalf("got %+v", got)
	}
}

func TestACrashCountsOnlyAtTheFirstBootAfterTheEnd(t *testing.T) {
	end := time.Unix(1741068000, 0)
	quiet := Seen{At: end}
	at := func(d time.Duration) time.Time { return end.Add(d) }
	for _, c := range []struct {
		name    string
		endings []eventlog.Ending
		kind    string
		when    time.Time
	}{
		// Windows shut down cleanly two hours after the start went quiet,
		// booted, and crashed the next day: not what ended it.
		{"a crash after a clean shutdown", []eventlog.Ending{{Kind: eventlog.Shutdown, At: at(2 * time.Hour)}, {Kind: "boot", At: at(3 * time.Hour)},
			{Kind: eventlog.Crash, At: at(26 * time.Hour)}, {Kind: "boot", At: at(26 * time.Hour)}}, "outside", end},
		// A sign-out ends WSL too: a crash after it was not what did.
		{"a crash after a sign-out", []eventlog.Ending{{Kind: "signout", At: at(time.Hour)}, {Kind: eventlog.Crash, At: at(2 * time.Hour)}}, "outside", end},
		// EventLog 6008's moment is Windows' last stamp of being alive,
		// written now and then: it may come a little before the start's
		// last line, and says no later than that.
		{"6008 just before the last line", []eventlog.Ending{{Kind: eventlog.Crash, At: at(-50 * time.Second)}, {Kind: eventlog.Crash, At: at(40 * time.Second)}}, "crash", end},
		// One long before is another crash.
		{"6008 long before", []eventlog.Ending{{Kind: eventlog.Crash, At: at(-2 * time.Hour)}}, "outside", end},
		// A boot after the end with nothing logged about a crash: not one.
		{"a boot alone", []eventlog.Ending{{Kind: "boot", At: at(time.Hour)}}, "outside", end},
	} {
		got := WhyCut(quiet, c.endings)
		if got.Kind != c.kind || !got.At.Equal(c.when) {
			t.Errorf("%s: got %s at %v, want %s at %v", c.name, got.Kind, got.At, c.kind, c.when)
		}
	}
}

func TestParseSeenTrustsOnlyItsOwnLines(t *testing.T) {
	// A message's next lines come indented: one that reads like a moment
	// is not an entry.
	s, ok := ParseSeen("1741068000.250000 box app[42]: done\n    0 errors found\n")
	if !ok || !s.At.Equal(time.Unix(1741068000, 250000000)) {
		t.Fatalf("got %+v %v", s, ok)
	}
	// The latest moment, wherever its line is.
	s, _ = ParseSeen("1741068009.000000 box a[1]: later\n1741068000.000000 box b[2]: earlier\n")
	if !s.At.Equal(time.Unix(1741068009, 0)) {
		t.Fatalf("got %+v", s)
	}
	// A line any program may log does not say it was asked: only systemd's
	// own as PID 1, and WSL's kernel line.
	for _, line := range []string{
		"1741068000.100000 box someone[4242]: please InitTerminateInstance now",
		"1741068000.100000 box systemd[1][4242]: Shutting down.",
		"1741068000.100000 box systemd[1234]: Shutting down.",
		"1741068000.100000 box logger[77]: systemd[1]: Shutting down.",
		"1741068000.100000 box systemd-journald[4242]: Journal stopped",
	} {
		if s, _ := ParseSeen(line + "\n"); s.Asked {
			t.Errorf("asked by %q", line)
		}
	}
	for _, line := range []string{poweredOff, powerOffCut} {
		if s, _ := ParseSeen(line); !s.Asked {
			t.Errorf("not asked by %q", line)
		}
	}
	// Seconds without their six digits, or a line that does not start
	// with its moment, are none.
	for _, out := range []string{"1741068000 box a[1]: x\n", " 1741068000.000000 box a[1]: x\n", "1741068000.12 box a[1]: x\n"} {
		if s, ok := ParseSeen(out); ok {
			t.Errorf("%q: seen %+v", out, s)
		}
	}
}

func TestSeenScriptKeepsTheLinesShortAndOnlyEntries(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	// A journalctl whose last entries hold a huge line and a message with
	// lines of its own.
	long := strings.Repeat("x", 10000)
	body := "#!/bin/sh\nprintf '%s\\n' '1741068000.000000 box a[1]: " + long + "' '    0 errors found' '1741068001.000000 box b[2]: end'\n"
	if err := os.WriteFile(filepath.Join(dir, "journalctl"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", SeenScript("5f1c0d2e3a4b4c5d8e9f0a1b2c3d4e5f", time.Unix(1741099402, 0)))
	cmd.Env = append(os.Environ(), "R=1", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 || len(lines[0]) > 300 || lines[1] != "1741068001.000000 box b[2]: end" {
		t.Fatalf("got %d lines: %.120q", len(lines), out)
	}
}
