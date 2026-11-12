package cli

import (
	"context"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/distros"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
)

// seenTime is how long the read of a distribution's journal may take: one
// that does not answer leaves the cause untold, never the command waiting.
const seenTime = 15 * time.Second

// cutCauses says, by a distribution's name, what ended its last start
// where wsl-spawner did not stop it: its journal says when that start
// ended and how, and Windows' event logs what happened then. Only running
// distributions whose last start was cut are asked, once each. One that
// cannot tell (no journal of that start, logs that cannot be read, the
// demo's made-up PC) is left out, and its cut is said as before.
func cutCauses(ctx context.Context, a *app.App, ds []distros.Distro) map[string]inside.Cause {
	out := map[string]inside.Cause{}
	for _, d := range ds {
		if d.Probe == nil {
			continue
		}
		boot, until, ok := d.Probe.CutStart()
		if !ok {
			continue
		}
		// The journal's last lines of that start, read as root: a
		// distribution's journal is not every user's to read.
		rctx, cancel := context.WithTimeout(ctx, seenTime)
		said, err := a.WSL.Run(rctx, d.Name, "root", inside.SeenScript(boot, until))
		cancel()
		if err != nil {
			continue
		}
		seen, ok := inside.ParseSeen(said)
		if !ok {
			continue
		}
		// The distribution's moments on Windows' clock, which its logs
		// keep: set back by how far its clock ran ahead as it was read.
		skew := inside.ClockSkew(d.Probe.Now, d.Asked, d.Answered)
		seen.At, until = seen.At.Add(-skew), until.Add(-skew)
		// From a little before the journal saw it end, to this start: a
		// crash is logged as Windows boots after it.
		endings, err := a.PC.EndingsBetween(seen.At.Add(-inside.AskedBefore), until)
		if err != nil {
			continue
		}
		out[d.Name] = inside.WhyCut(seen, endings)
	}
	return out
}

// causeWords says what ended a cut start, after the words of the cut, with
// when, in a table's short words: "" when nothing is known of it. The
// moment is the person's local time, as the rest of the row's.
func causeWords(c inside.Cause) (words, when string) {
	if c.Kind == "" {
		return "", ""
	}
	return i18n.T("end.cause." + c.Kind), c.At.Local().Format("01-02 15:04")
}
