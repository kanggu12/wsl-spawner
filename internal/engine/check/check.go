// Package check looks over everything wsl-spawner's work stands on, and
// changes nothing: the sign-out hook and how long Windows waits for it, each
// distribution's systemd, how its last start ended, its containers' stop
// timeouts and restart policies, the start at sign-in, WSL's own settings,
// and the ports Windows keeps from programs. Each finding says what it is,
// and what to do when there is something to do.
package check

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/engine/distros"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// Mark is what a finding means.
type Mark int

const (
	// OK: as it should be.
	OK Mark = iota
	// Info: said for the look, nothing to do.
	Info
	// Warn: worth a second look; something may not go as it should.
	Warn
	// Bad: something does not go as it should.
	Bad
)

// Item is one finding: what it is and what to do, as keys of the words
// catalog with their values, so each front says it in its own language.
type Item struct {
	Mark Mark
	// Key and Args say what it is; Do and DoArgs what to do, "" for none.
	Key    string
	Args   []any
	Do     string
	DoArgs []any
}

// Group is the findings about one thing.
type Group struct {
	// Key names it in the catalog; Arg is its distribution, for one.
	Key, Arg string
	Items    []Item
}

// Worst is the gravest mark among the groups: OK when there is nothing
// but OK and Info, which is said for the look only.
func Worst(gs []Group) Mark {
	w := OK
	for _, g := range gs {
		for _, it := range g.Items {
			if it.Mark != Info {
				w = max(w, it.Mark)
			}
		}
	}
	return w
}

// Hook is the sign-out hook as found.
type Hook struct {
	// Installed and Last say where wsl-spawner's is.
	Installed, Last bool
	// Count is how many sign-out scripts there are, wsl-spawner's included.
	Count int
	// PowerShell is how many PowerShell sign-out scripts run after the
	// hook: Windows runs them after the others unless it is set to run them
	// first. PowerShellLast is set when psscripts.ini itself has them run
	// last, so nothing else decides it.
	PowerShell     int
	PowerShellLast bool
	// Changeable are the programs a sign-out hook of wsl-spawner's starts,
	// this copy's or another's, that someone beyond the person at this PC,
	// the system and the administrators can change, or put one in the place
	// of. The hook runs for every user of the PC, as whoever signs out.
	Changeable []Changeable
	// Copies are other copies' sign-out hooks, whose program is there, run
	// while this copy has none: they stop the distributions and WSL as this
	// copy's would.
	Copies []string
	// StopsWSL are the other sign-out scripts that run before wsl-spawner's
	// (all of them, when it is not installed) and stop WSL themselves:
	// after them, wsl-spawner's finds nothing running.
	StopsWSL []string
	// MaxWait is how long Windows waits for the sign-out scripts; 0 is as
	// long as they take.
	MaxWait time.Duration
	// Standard is set when the person is a standard user: a hook of theirs
	// has an administrator's own install for its way out.
	Standard bool
	// Err is why the scripts could not be read.
	Err error
}

// Changeable is a program a sign-out hook starts that another user can
// change: Program is it, By who may, At the file or folder they may change it
// by; Copy is set when the hook is another copy's, which that copy takes
// out.
type Changeable struct {
	Program, By, At string
	Copy            bool
}

// LastStop is wsl-spawner's newest stop of a distribution in the history:
// how it ended (clean, limit, failed or error) and when.
type LastStop struct {
	Result string
	At     time.Time
}

// Logon is the sign-in program as found.
type Logon struct {
	// Present is set when it is there, Command what it runs; Off when Task
	// Manager has it turned off. Want is what it should run.
	Present, Off  bool
	Command, Want string
	// Err is why it could not be read.
	Err error
}

// PortRange is a range of ports Windows keeps from programs.
type PortRange struct {
	// Proto is "tcp" or "udp"; Start and End the first and last port.
	Proto      string
	Start, End int
}

// Inputs is everything a check reads.
type Inputs struct {
	Config     config.Config
	Distros    []distros.Distro
	WSLVersion string
	Hook       Hook
	Logon      Logon
	// WSLConfig is the text of %USERPROFILE%\.wslconfig, "" for none.
	WSLConfig string
	// LastStops are wsl-spawner's newest stop of each distribution, by its
	// name, as the history has them: said where nothing inside one notes
	// how its last start ended.
	LastStops map[string]LastStop
	// Causes say, by a distribution's name, what ended its last start
	// where wsl-spawner did not stop it, as its journal and Windows' event
	// logs tell it together; one they do not tell is not there.
	Causes map[string]inside.Cause
	// Excluded are the ports Windows keeps, and why they could not be read.
	Excluded    []PortRange
	ExcludedErr error
}

// Run makes every finding.
func Run(in Inputs) []Group {
	gs := []Group{stopping(in)}
	for _, d := range in.Distros {
		gs = append(gs, distro(in, d))
	}
	gs = append(gs, signIn(in), wslGroup(in), network(in))
	return gs
}

// secs is a duration in whole seconds, for the words.
func secs(d time.Duration) int { return int(d / time.Second) }

// stopping is what stops the distributions when Windows signs out.
func stopping(in Inputs) Group {
	g := Group{Key: "check.g.stopping"}
	h := in.Hook
	switch {
	case h.Err != nil:
		g.Items = append(g.Items, Item{Mark: Warn, Key: "check.hook.unread", Args: []any{h.Err.Error()}})
	case h.Last && h.PowerShell > 0:
		// Last of the others, but the PowerShell ones come after it, once
		// WSL has stopped: one that uses WSL would start it again. Said as
		// the policy's own choice when psscripts.ini makes it.
		key := "check.hook.powershell"
		if h.PowerShellLast {
			key = "check.hook.pslast"
		}
		g.Items = append(g.Items, Item{Mark: Warn, Key: key, Args: []any{h.Count, h.PowerShell}, Do: "check.hook.do.powershell"})
	case h.Last:
		g.Items = append(g.Items, Item{Mark: OK, Key: "check.hook.last", Args: []any{h.Count}})
	case h.Installed:
		g.Items = append(g.Items, Item{Mark: Warn, Key: "check.hook.notlast", Do: "check.hook.do.install"})
	case len(h.Copies) > 0:
		// Another copy's hook does this copy's work: said for what it is,
		// and how this copy's takes its place.
		g.Items = append(g.Items, Item{Mark: Info, Key: "check.hook.copy", Args: []any{strings.Join(h.Copies, ", ")}, Do: "check.hook.do.install"})
	default:
		g.Items = append(g.Items, Item{Mark: Warn, Key: "check.hook.none", Do: "check.hook.do.install"})
	}
	// What a hook starts is another user's to change: worth more than where
	// it stands among the scripts, and said for each, by the program itself
	// or the folder that holds it, with the way out that fits: another
	// copy's own, or, for a standard user, an administrator's install.
	for _, c := range h.Changeable {
		it := Item{Mark: Bad, Key: "check.hook.writable", Args: []any{c.By, c.Program}, Do: "check.hook.do.writable"}
		if c.At != c.Program {
			it.Key, it.Args = "check.hook.writable.at", []any{c.By, c.At, c.Program}
		}
		switch {
		case c.Copy:
			it.Do = "check.hook.do.writable.copy"
		case h.Standard:
			it.Do = "check.hook.do.writable.std"
		}
		g.Items = append(g.Items, it)
	}
	for _, s := range h.StopsWSL {
		g.Items = append(g.Items, Item{Mark: Warn, Key: "check.hook.otherwsl", Args: []any{s}, Do: "check.hook.do.other"})
	}
	if h.Err == nil {
		limit := time.Duration(in.Config.StopLimit) * time.Second
		switch {
		case h.MaxWait == 0:
			g.Items = append(g.Items, Item{Mark: OK, Key: "check.wait.unbounded"})
		case h.MaxWait >= limit+time.Minute:
			g.Items = append(g.Items, Item{Mark: OK, Key: "check.wait.enough", Args: []any{secs(h.MaxWait), secs(limit)}})
		default:
			// hook install raises it; a standard user's hook is an
			// administrator's install, whose wait is theirs to raise.
			do := "check.wait.do.install"
			if h.Standard {
				do = "check.wait.do"
			}
			g.Items = append(g.Items, Item{Mark: Warn, Key: "check.wait.short", Args: []any{secs(h.MaxWait), secs(limit)}, Do: do})
		}
	}
	return g
}

// causeKeys are the words of what ended a start that was cut, by its kind:
// one the check has no words for says nothing.
var causeKeys = map[string]string{
	"crash": "check.d.end.cause.crash", "update": "check.d.end.cause.update",
	"shutdown": "check.d.end.cause.shutdown", "signout": "check.d.end.cause.signout",
	inside.CauseOutside: "check.d.end.cause.outside",
}

// distro is what one distribution's stop and start stand on.
func distro(in Inputs, d distros.Distro) Group {
	g := Group{Key: "check.g.distro", Arg: d.Name}
	add := func(it Item) { g.Items = append(g.Items, it) }
	// What the history holds of wsl-spawner's last stop of it, right after
	// the finding that says nothing inside notes how its last start ended,
	// once: as the main screen says it.
	told := false
	fromHistory := func() {
		s, ok := in.LastStops[d.Name]
		if told || !ok {
			return
		}
		// Each ending by its own words, marked as the main screen colors it.
		said := map[string]struct {
			mark Mark
			key  string
		}{
			"clean":  {OK, "check.d.history.clean"},
			"limit":  {Warn, "check.d.history.limit"},
			"failed": {Warn, "check.d.history.failed"},
			"error":  {Bad, "check.d.history.error"},
		}
		if w, known := said[s.Result]; known {
			add(Item{Mark: w.mark, Key: w.key, Args: []any{stamp(s.At)}})
			told = true
		}
	}
	switch d.Kind() {
	case distros.KindOwned:
		add(Item{Mark: Info, Key: "check.d.owned", Args: []any{d.Owner}})
		return g
	case distros.KindWSL1:
		add(Item{Mark: Info, Key: "check.d.wsl1"})
		return g
	case distros.KindSkipped:
		add(Item{Mark: Info, Key: "check.d.skipped"})
		return g
	}
	if !d.Running {
		add(Item{Mark: Info, Key: "check.d.stopped"})
		fromHistory()
		return g
	}
	p := d.Probe
	if p == nil {
		add(Item{Mark: Warn, Key: "check.d.unread", Args: []any{errText(d.ProbeErr)}})
		return g
	}
	// Cut off before its end: what follows is what it read by then, and
	// what it did not read is not said to be fine.
	if !p.Complete {
		why := errText(d.ProbeErr)
		if why == "" {
			why = "its look ended before its last line"
		}
		add(Item{Mark: Warn, Key: "check.d.partial", Args: []any{why}})
	}
	switch p.Systemd {
	case "":
		add(Item{Mark: Warn, Key: "check.d.nosystemd", Do: "check.d.do.systemd", DoArgs: []any{d.Name}})
	case inside.SystemdNoAnswer:
		// It runs, and did not answer the probe in its time: busy or stuck,
		// which a stop's own calls to it meet too.
		add(Item{Mark: Warn, Key: "check.d.systemdsilent"})
	case "running":
		add(Item{Mark: OK, Key: "check.d.systemd"})
	case "degraded":
		add(Item{Mark: Warn, Key: "check.d.degraded", Args: []any{len(p.Failed), strings.Join(p.Failed, ", ")}, Do: "check.d.do.failed"})
	default:
		add(Item{Mark: Info, Key: "check.d.systemdstate", Args: []any{p.Systemd}})
	}
	if !p.SetUp() {
		add(Item{Mark: Info, Key: "check.d.notsetup", Do: "check.d.do.setup", DoArgs: []any{d.Name}})
		fromHistory()
	} else if p.StartScriptOlder() {
		add(Item{Mark: Warn, Key: "check.d.oldsetup", Do: "check.d.do.setup", DoArgs: []any{d.Name}})
	} else if p.StartScriptNewer() {
		// A later wsl-spawner put it, and this one came back since: it is
		// left as it is, and said for the look.
		add(Item{Mark: Info, Key: "check.d.newsetup"})
	}
	end, prev := p.LastEnd()
	switch end {
	case inside.EndClean:
		add(Item{Mark: OK, Key: "check.d.end.clean", Args: []any{stamp(p.LastStop.At)}})
	case inside.EndLimit:
		add(Item{Mark: Warn, Key: "check.d.end.limit", Args: []any{stamp(p.LastStop.At)}, Do: "check.d.do.limit"})
	case inside.EndFailed:
		add(Item{Mark: Warn, Key: "check.d.end.failed", Args: []any{stamp(p.LastStop.At)}, Do: "check.d.do.history"})
	case inside.EndCut:
		add(Item{Mark: Bad, Key: "check.d.end.cut", Args: []any{stamp(prev.At)}, Do: "check.d.do.cut"})
		// What ended it, where the journal and Windows' logs tell it: said
		// on a line of its own, so the finding above stays as it was.
		if key, ok := causeKeys[in.Causes[d.Name].Kind]; ok {
			add(Item{Mark: Info, Key: key, Args: []any{stamp(in.Causes[d.Name].At)}})
		}
	case inside.EndFirst:
		add(Item{Mark: Info, Key: "check.d.end.first"})
		fromHistory()
	}
	switch {
	case p.KeptRunning() && !p.Interop:
		add(Item{Mark: Bad, Key: "check.d.keepnointerop", Do: "check.d.do.interop", DoArgs: []any{d.Name}})
	case d.AtLogon && !p.KeptRunning():
		add(Item{Mark: Warn, Key: "check.d.logonnotkept", Do: "check.d.do.keep", DoArgs: []any{d.Name}})
	case p.KeptRunning():
		add(Item{Mark: OK, Key: "check.d.kept"})
	}
	// The units stop-units names that a stop passes by, as it stops only
	// those that run: one systemd does not have (a typo, or a note on its
	// line) is worth a look; those that do not run now are said for the
	// look, each with its state. One whose state was not read is not said.
	var missing, idle []string
	for _, u := range p.StopUnits {
		st, ok := p.StopUnitStates[u]
		switch {
		case !ok || st.Load == "":
			// Not read: nothing is said of it either way.
		case st.Load == "not-found":
			missing = append(missing, u)
		case st.Active != "" && st.Active != "active":
			idle = append(idle, u+" ("+st.Active+")")
		}
	}
	if len(missing) > 0 {
		add(Item{Mark: Warn, Key: "check.d.unitsmissing", Args: []any{strings.Join(missing, ", ")}, Do: "check.d.do.units"})
	}
	if len(idle) > 0 {
		add(Item{Mark: Info, Key: "check.d.unitsidle", Args: []any{strings.Join(idle, ", ")}})
	}
	for _, e := range []string{"docker", "podman"} {
		switch p.EngineState(e) {
		case inside.EngineDown:
			add(Item{Mark: Warn, Key: "check.d.enginedown", Args: []any{e}})
		case inside.EngineAsleep:
			// Stopped by hand, its socket listening: nothing is wrong, and
			// nothing of it runs. Said for the look.
			add(Item{Mark: Info, Key: "check.d.engineasleep", Args: []any{e}})
		case inside.EngineForeign:
			// Another's daemon, Docker Desktop's or another engine's: left
			// alone, which is nothing to do. Said for the look.
			add(Item{Mark: Info, Key: "check.d.engineforeign", Args: []any{e}})
		}
	}
	if p.Engines["docker"] && p.LiveRestore {
		add(Item{Mark: Info, Key: "check.d.liverestore"})
	}
	limit := time.Duration(in.Config.StopLimit) * time.Second
	var noPolicy []string
	for _, c := range p.Containers {
		if !c.Running() {
			continue
		}
		if c.Wait() >= limit {
			add(Item{Mark: Warn, Key: "check.d.longstop", Args: []any{c.Name, secs(c.Wait()), secs(limit)}, Do: "check.d.do.longstop"})
		}
		// One removed as it stops comes back by nothing: no setting to
		// say of it.
		if !c.Restarts() && !c.AutoRemove {
			noPolicy = append(noPolicy, c.Name)
		}
	}
	if len(noPolicy) > 0 && in.Config.Resume != "running" {
		add(Item{Mark: Warn, Key: "check.d.nopolicy", Args: []any{len(noPolicy), strings.Join(noPolicy, ", "), in.Config.Resume}, Do: "check.d.do.resume"})
	}
	if p.SetUp() && len(p.Bring) > 0 {
		add(Item{Mark: Warn, Key: "check.d.bringpending", Args: []any{len(p.Bring)}, Do: "check.d.do.startunit"})
	}
	var notBack []string
	for _, r := range p.Resumed {
		if r.What == "failed" || r.What == "exited" {
			notBack = append(notBack, r.Name)
		}
	}
	if len(notBack) > 0 {
		add(Item{Mark: Warn, Key: "check.d.notback", Args: []any{strings.Join(notBack, ", "), stamp(p.ResumedAt)}})
	}
	// The distribution's clock against Windows': its probe read it between
	// being asked and answering, so a clock that agrees falls between the
	// two on Windows'. The time its probe took, or another's, is no clock
	// off.
	if !p.Now.IsZero() && !d.Asked.IsZero() && !d.Answered.IsZero() {
		if skew := offBy(p.Now, d.Asked, d.Answered); skew > 5*time.Second {
			add(Item{Mark: Warn, Key: "check.d.clock", Args: []any{secs(skew)}, Do: "check.d.do.clock", DoArgs: []any{d.Name}})
		}
	}
	return g
}

// offBy is how far t lies outside the moments from asked to answered, and
// none inside them. t is in whole seconds, the rest dropped: up to a
// second before asked is inside.
func offBy(t, asked, answered time.Time) time.Duration {
	from := asked.Add(-time.Second)
	switch {
	case t.Before(from):
		return from.Sub(t)
	case t.After(answered):
		return t.Sub(answered)
	}
	return 0
}

// stamp is a moment as the findings say it.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// errText is an error's first line, or "".
func errText(err error) string {
	if err == nil {
		return ""
	}
	first, _, _ := strings.Cut(strings.TrimSpace(err.Error()), "\n")
	return first
}

// signIn is the start at sign-in.
func signIn(in Inputs) Group {
	g := Group{Key: "check.g.signin"}
	add := func(it Item) { g.Items = append(g.Items, it) }
	l := in.Logon
	if l.Err != nil {
		add(Item{Mark: Warn, Key: "check.s.unread", Args: []any{l.Err.Error()}})
		return g
	}
	if len(in.Config.Logon) == 0 {
		if l.Present {
			add(Item{Mark: Warn, Key: "check.s.leftover", Do: "check.s.do.leftover"})
		} else {
			add(Item{Mark: Info, Key: "check.s.none"})
		}
		return g
	}
	switch {
	case !l.Present:
		add(Item{Mark: Bad, Key: "check.s.noentry", Do: "check.s.do.entry", DoArgs: []any{in.Config.Logon[0]}})
	case l.Command != l.Want && l.Want != "":
		add(Item{Mark: Warn, Key: "check.s.stale", Args: []any{l.Command}, Do: "check.s.do.entry", DoArgs: []any{in.Config.Logon[0]}})
	case l.Off:
		add(Item{Mark: Warn, Key: "check.s.off", Do: "check.s.do.on"})
	default:
		add(Item{Mark: OK, Key: "check.s.entry"})
	}
	for _, name := range in.Config.Logon {
		i := slices.IndexFunc(in.Distros, func(d distros.Distro) bool { return strings.EqualFold(d.Name, name) })
		if i < 0 {
			add(Item{Mark: Warn, Key: "check.s.missing", Args: []any{name}, Do: "check.s.do.missing"})
			continue
		}
		add(Item{Mark: Info, Key: "check.s.starts", Args: []any{in.Distros[i].Name}})
	}
	return g
}

// wslGroup is WSL's own version and settings.
func wslGroup(in Inputs) Group {
	g := Group{Key: "check.g.wsl"}
	if in.WSLVersion != "" {
		g.Items = append(g.Items, Item{Mark: Info, Key: "check.w.version", Args: []any{in.WSLVersion}})
	}
	settings := WSLConfigKeys(in.WSLConfig)
	if len(settings) == 0 {
		g.Items = append(g.Items, Item{Mark: Info, Key: "check.w.nowslconfig"})
		return g
	}
	g.Items = append(g.Items, Item{Mark: Info, Key: "check.w.wslconfig", Args: []any{strings.Join(settings, ", ")}})
	return g
}

// watchedKeys are the .wslconfig settings that change how long WSL keeps a
// distribution or its VM up, and how it reaches the network.
var watchedKeys = []string{"instanceidletimeout", "vmidletimeout", "networkingmode", "autoproxy", "firewall"}

// WSLConfigKeys reads the settings of .wslconfig that bear on wsl-spawner's
// work, as "key=value", in the file's order.
func WSLConfigKeys(text string) []string {
	var out []string
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' || line[0] == '[' {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if slices.Contains(watchedKeys, strings.ToLower(k)) {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// network is the ports the containers publish against those Windows keeps.
func network(in Inputs) Group {
	g := Group{Key: "check.g.network"}
	if in.ExcludedErr != nil {
		g.Items = append(g.Items, Item{Mark: Info, Key: "check.n.unread", Args: []any{in.ExcludedErr.Error()}})
		return g
	}
	checked := 0
	for _, d := range in.Distros {
		if d.Probe == nil {
			continue
		}
		for _, c := range d.Probe.Containers {
			for _, p := range c.Ports {
				num, proto, _ := strings.Cut(p, "/")
				port, err := strconv.Atoi(num)
				if err != nil {
					continue
				}
				checked++
				for _, r := range in.Excluded {
					if r.Proto == proto && port >= r.Start && port <= r.End {
						g.Items = append(g.Items, Item{Mark: Bad, Key: "check.n.excluded",
							Args: []any{c.Name, d.Name, p, fmt.Sprintf("%d-%d", r.Start, r.End)}, Do: "check.n.do.excluded"})
					}
				}
			}
		}
	}
	if len(g.Items) == 0 {
		g.Items = append(g.Items, Item{Mark: OK, Key: "check.n.ok", Args: []any{checked}})
	}
	return g
}

// ParseExcluded reads the ranges `netsh interface ipv4 show
// excludedportrange protocol=PROTO` lists: each row is two numbers, whatever
// language the words around them are in.
func ParseExcluded(out, proto string) []PortRange {
	var rs []PortRange
	for line := range strings.SplitSeq(strings.ReplaceAll(out, "\r", ""), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		a, err1 := strconv.Atoi(f[0])
		b, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil && a <= b {
			rs = append(rs, PortRange{Proto: proto, Start: a, End: b})
		}
	}
	return rs
}

// StopsWSL reports whether a sign-out script's text stops WSL itself: it
// runs wsl.exe with --shutdown or --terminate.
func StopsWSL(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "wsl") && (strings.Contains(low, "--shutdown") || strings.Contains(low, "--terminate"))
}
