package inside

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/machine/eventlog"
)

// What ended a start that wsl-spawner did not stop. The distribution's own
// journal says when that start ended, and how: asked to power off, as WSL
// asks a distribution it ends, or without a word, as when Windows or the VM
// goes down at once. Windows' event logs say what happened then. Only the
// two together tell it: a start may end long before Windows next shuts
// down, so an event of Windows' alone does not say it ended that start.

// CauseOutside is a cut with nothing in Windows' logs to explain it: WSL
// was stopped outside wsl-spawner, by wsl --shutdown, wsl --terminate or WSL
// itself (a distribution left without a session, an error of its own).
const CauseOutside = "outside"

// Cause is what ended a start that wsl-spawner did not stop.
type Cause struct {
	// Kind is eventlog's kind ("crash", "update" or "shutdown"), or
	// CauseOutside; "" when it cannot be told, and nothing is said.
	Kind string
	// At is when it happened: the event's moment, or for CauseOutside
	// when the journal saw the start end.
	At time.Time
}

// Seen is how a distribution's journal saw a start end: the moment of its
// last line, and whether it was asked to power off.
type Seen struct {
	At    time.Time
	Asked bool
}

// The time around the moment a start was asked to power off in which
// Windows' event that asked for it may be: a shutdown is asked for seconds
// before, a sign-out at it, and the scripts before the hook may take a few
// minutes.
const (
	AskedBefore = 3 * time.Minute
	askedAfter  = time.Minute
	// An update stops WSL while it installs: a start it ended stopped
	// between the install's first event and its last, give or take this.
	updateSlack = 30 * time.Second
	// The events of one install come one after another; a gap longer than
	// this is another install.
	installGap = 10 * time.Minute
	// EventLog 6008's moment is Windows' last stamp of being alive, which
	// it writes every minute or so: the crash came up to that much after
	// it, so the stamp may come a little before the start's last line.
	aliveLag = 70 * time.Second
	// The events Windows logs about a crash come in the minutes after the
	// boot that follows it.
	bootLog = 5 * time.Minute
)

// userHZ is how many clock ticks make a second in /proc/<pid>/stat: the
// kernel gives the start of a process in USER_HZ ticks, 100 on Linux.
const userHZ = 100

// bootID is a kernel boot's ID as the journal takes it: 32 hex digits.
var bootID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// CutStart is, when the start before this one ended cut, the kernel boot
// that start ran in, for its journal, and the moment this start began:
// that start's lines all come before it, whether or not the VM booted again
// between them. ok is false when the probe does not say enough.
func (p Probe) CutStart() (boot string, until time.Time, ok bool) {
	end, prev := p.LastEnd()
	if end != EndCut || prev.ID == "" {
		return "", time.Time{}, false
	}
	// A start's ID is its boot and its init's start: the boot, without the
	// dashes /proc gives it with.
	b, _, _ := strings.Cut(prev.ID, "@")
	b = strings.ReplaceAll(strings.ToLower(b), "-", "")
	ticks, err := strconv.ParseInt(p.InitStart, 10, 64)
	// This start's init began this long after the kernel booted; the kernel
	// booted Uptime before the probe's clock.
	if !bootID.MatchString(b) || err != nil || p.Now.IsZero() {
		return "", time.Time{}, false
	}
	began := p.Now.Add(-p.Uptime).Add(time.Duration(ticks) * time.Second / userHZ)
	return b, began, true
}

// SeenScript prints the last lines the distribution's journal holds of the
// boot named, logged before until, as journalctl's short-unix form gives
// them: each starts with its moment, in seconds and six digits. Only those
// lines are kept (a message's further lines come indented), each cut at 300
// bytes, so one huge entry pushes out no other. It prints nothing where the
// journal does not hold that boot, or there is no journalctl. It reads only.
// R set (by a test) keeps the PATH it brings, as the probe does.
func SeenScript(boot string, until time.Time) string {
	// The boot is 32 hex digits, or nothing to read, and until a number:
	// neither needs quoting.
	b := bootID.FindString(boot)
	if b == "" {
		return "exit 0\n"
	}
	return "[ -n \"${R:-}\" ] || PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n" +
		"export PATH LC_ALL=C\n" +
		"command -v journalctl >/dev/null 2>&1 || exit 0\n" +
		// A journal that hangs must not hold the read: timeout where it is.
		"if command -v timeout >/dev/null 2>&1; then t='timeout 10'; else t=; fi\n" +
		"$t journalctl --no-pager -q -o short-unix -b " + b + " --until @" + strconv.FormatInt(until.Unix()-1, 10) + " -n 12 2>/dev/null |\n" +
		"\tgrep -E '^[0-9]+\\.[0-9]{6} ' | cut -c1-300\n" +
		"exit 0\n"
}

// seenLine is one of SeenScript's lines: its moment, in seconds and six
// digits, the host, and the rest, "identifier[pid]: message", where the pid
// is the one journald noted itself, or "identifier: message" for a line of
// the kernel's log.
var seenLine = regexp.MustCompile(`^([0-9]+)\.([0-9]{6}) (\S+) (.*)$`)

// ParseSeen reads SeenScript's output: the latest moment of its lines, and
// whether they say the start was asked to power off. Only a line as
// journalctl starts an entry counts. ok is false when there is none.
func ParseSeen(out string) (Seen, bool) {
	var s Seen
	ok := false
	for _, line := range strings.Split(out, "\n") {
		m := seenLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sec, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		usec, _ := strconv.ParseInt(m[2], 10, 64)
		// The latest, wherever its line is.
		if at := time.Unix(sec, usec*1000); !ok || at.After(s.At) {
			s.At = at
		}
		ok = true
		if saysAsked(m[4]) {
			s.Asked = true
		}
	}
	return s, ok
}

// saysAsked reports whether the rest of a journal line says the
// distribution was asked to power off, in the words only systemd as PID 1
// and WSL can give it: a line any program logs carries its own pid, which
// journald notes, whatever name it gives itself.
func saysAsked(rest string) bool {
	switch {
	case rest == "systemd[1]: Shutting down.":
		// systemd's last, as the system goes down.
		return true
	case strings.HasPrefix(rest, "unknown: WSL (") && strings.Contains(rest, "InitTerminateInstance"):
		// WSL's, in the kernel's log, when the power off it asked for did
		// not end in its time: no pid, no name of its own.
		return true
	}
	return false
}

// ClockSkew is how far a distribution's clock runs ahead of Windows' (less
// than 0: behind), as its probe read it at now, in whole seconds, asked at
// asked and answered at answered on Windows' clock: none within that time.
func ClockSkew(now, asked, answered time.Time) time.Duration {
	if now.IsZero() || asked.IsZero() || answered.IsZero() {
		return 0
	}
	// The probe's clock is whole seconds: a second before asking is still
	// within it.
	switch from := asked.Add(-time.Second); {
	case now.Before(from):
		return now.Sub(from)
	case now.After(answered):
		return now.Sub(answered)
	}
	return 0
}

// WhyCut is what ended a start the journal saw end, of the endings Windows'
// logs hold from AskedBefore before then to the start after it:
//   - a shutdown or a sign-out about when it ended, asked to power off or
//     not, as journald may be ended first;
//   - an update whose install it ended within, from its first event to its
//     last;
//   - a crash, for a start that went without a word: one Windows logs at
//     its first boot after the end, with no shutdown or sign-out before
//     that boot, which would have ended it first. Its moment is the end, or
//     Windows' last stamp of being alive where that is later.
//
// The first of them is what did it; none, and it was stopped outside
// wsl-spawner, when the journal saw it end.
func WhyCut(seen Seen, endings []eventlog.Ending) Cause {
	es := slices.Clone(endings)
	slices.SortStableFunc(es, func(a, b eventlog.Ending) int { return a.At.Compare(b.At) })
	near := func(t time.Time) bool {
		return !t.Before(seen.At.Add(-AskedBefore)) && !t.After(seen.At.Add(askedAfter))
	}
	var fit []eventlog.Ending
	// Windows' first boot after the end, as its own events or those it
	// logs at a boot about a crash say; and the first shutdown or sign-out.
	var up, down time.Time
	for _, e := range es {
		if !e.At.After(seen.At) {
			continue
		}
		switch {
		case up.IsZero() && (e.Kind == eventlog.Boot || e.Kind == eventlog.Crash && !e.Moment):
			up = e.At
		case down.IsZero() && (e.Kind == eventlog.Shutdown || e.Kind == eventlog.Signout):
			down = e.At
		}
	}
	// One install's events, with no gap longer than installGap between them.
	var spans [][2]time.Time
	for _, e := range es {
		switch e.Kind {
		case eventlog.Shutdown, eventlog.Signout:
			if near(e.At) {
				fit = append(fit, e)
			}
		case eventlog.Update:
			if n := len(spans); n > 0 && e.At.Sub(spans[n-1][1]) <= installGap {
				spans[n-1][1] = e.At
			} else {
				spans = append(spans, [2]time.Time{e.At, e.At})
			}
		}
	}
	for _, sp := range spans {
		if !seen.At.Before(sp[0].Add(-updateSlack)) && !seen.At.After(sp[1].Add(updateSlack)) {
			fit = append(fit, eventlog.Ending{Kind: eventlog.Update, At: sp[0]})
		}
	}
	if !seen.Asked && !up.IsZero() && (down.IsZero() || !down.Before(up)) {
		logged, alive := false, time.Time{}
		for _, e := range es {
			switch {
			case e.Kind != eventlog.Crash:
			case !e.Moment && !e.At.Before(up) && !e.At.After(up.Add(bootLog)):
				// Logged at that boot.
				logged = true
			case e.Moment && alive.IsZero() && !e.At.Before(seen.At.Add(-aliveLag)) && !e.At.After(up):
				// Windows' last stamp of being alive, before that boot.
				alive = e.At
			}
		}
		if logged || !alive.IsZero() {
			at := seen.At
			if alive.After(at) {
				at = alive
			}
			fit = append(fit, eventlog.Ending{Kind: eventlog.Crash, At: at})
		}
	}
	if first, ok := eventlog.Earliest(fit); ok {
		return Cause{Kind: string(first.Kind), At: first.At}
	}
	return Cause{Kind: CauseOutside, At: seen.At}
}
