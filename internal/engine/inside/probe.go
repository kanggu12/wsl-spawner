// Package inside holds the scripts wsl-spawner runs inside a distribution,
// as root, and reads what they print: the probe, which only reads, and the
// scripts that stop a distribution's services, set wsl-spawner up in it and
// take it out again.
package inside

import (
	_ "embed"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

//go:embed probe.sh
var probeScript string

// ProbeScript is the script that reads what runs in a distribution.
func ProbeScript() string { return probeScript }

// Probe is what the probe read in one running distribution.
type Probe struct {
	// BootID is the VM kernel's boot, which every distribution shares.
	BootID string
	// InitStart is when this distribution's init started, in clock ticks
	// after the kernel booted: a distribution stopped and started again in
	// the same VM keeps the boot and gets a new start.
	InitStart string
	// Init is the name of process 1: "systemd", or WSL's own "init".
	Init string
	// Uptime is how long the kernel has run.
	Uptime time.Duration
	// Now is the distribution's clock as the probe ran, or zero.
	Now time.Time
	// Systemd is how far systemd is ("running", "degraded", "starting"),
	// or "" when it is not this distribution's init.
	Systemd string
	// Failed are the units systemd lists as failed.
	Failed []string
	// Units are the units wsl-spawner reads or puts in place, by name; a
	// unit not loaded is not there.
	Units map[string]Unit
	// Interop is set while Windows programs can run from the distribution.
	Interop bool
	// Waitfor is where the distribution sees Windows' waitfor.exe, or "".
	Waitfor string
	// Engines says, for each container engine installed, whether it
	// answered: docker or podman.
	Engines map[string]bool
	// LiveRestore is set when Docker keeps containers running while its
	// daemon is stopped.
	LiveRestore bool
	// Containers are every container of every engine that answered.
	Containers []Container
	// StartScript is the first line of the script wsl-spawner set up to run
	// at each start, which names its version; "" when it is not set up.
	StartScript string
	// Hooks are the programs in /etc/wsl-spawner/pre-stop.d, by name.
	Hooks []string
	// StopUnits are the units /etc/wsl-spawner/stop-units names, to stop
	// before the containers, each with its own time.
	StopUnits []string
	// Runs are the starts the start script noted, oldest first.
	Runs []Run
	// LastStop is the last stop wsl-spawner made, or nil.
	LastStop *StopRecord
	// Bring are the containers to start again at the next start.
	Bring []Bring
	// Resumed is what the start script did with the containers the stop
	// before noted, at ResumedAt.
	Resumed   []Resumed
	ResumedAt time.Time
	// Complete is set when the probe printed its last line: one cut off
	// says less than it seems.
	Complete bool
}

// Unit is one systemd unit's state.
type Unit struct {
	// Active is "active", "inactive", "failed" and the like.
	Active string
	// Enabled is "enabled", "disabled", "static" and the like.
	Enabled string
}

// Container is one container of an engine.
type Container struct {
	// Engine is "docker" or "podman".
	Engine string
	// ID is the full id; Name the name without Docker's leading slash.
	ID, Name string
	// State is "running", "exited", "paused" and the like.
	State string
	// Policy is the restart policy: "no", "always", "unless-stopped",
	// "on-failure", or "" when none is set.
	Policy string
	// StopTimeout is how long its engine waits for it to stop before
	// killing it, or 0 for the engine's default.
	StopTimeout time.Duration
	// Image is the image it runs; Project its Compose project, or "".
	Image, Project string
	// Ports are the host ports it publishes, by protocol: "8080/tcp".
	Ports []string
	// AutoRemove is set when its engine removes it as it stops (run with
	// --rm): nothing can start it again.
	AutoRemove bool
}

// Running reports whether the container runs.
func (c Container) Running() bool { return c.State == "running" }

// Restarts reports whether its engine starts it again by itself, after its
// daemon starts again: what "always" and "unless-stopped" do for a
// container that was running.
func (c Container) Restarts() bool { return c.Policy == "always" || c.Policy == "unless-stopped" }

// DefaultStopTimeout is how long Docker and Podman wait for a container
// with no stop timeout of its own.
const DefaultStopTimeout = 10 * time.Second

// Wait is how long its engine waits for it to stop.
func (c Container) Wait() time.Duration {
	if c.StopTimeout > 0 {
		return c.StopTimeout
	}
	return DefaultStopTimeout
}

// Run is one start of the distribution, as the start script noted it.
type Run struct {
	// At is when it started.
	At time.Time
	// ID is the boot and init start, as RunID gives them.
	ID string
}

// StopRecord is the last stop wsl-spawner made in the distribution.
type StopRecord struct {
	// Run is the start it stopped.
	Run string
	// At is when it finished; Took how long it took.
	At   time.Time
	Took time.Duration
	// Result is "clean", every step done; "limit", a step cut off at the
	// stop limit; or "failed", a step that failed.
	Result string
	// By says who asked: "hand" (wsl-spawner stop) or "signout" (the
	// Windows sign-out hook).
	By string
}

// Bring is a container to start again at the next start.
type Bring struct {
	// Engine is "docker" or "podman"; ID and Name say which.
	Engine, ID, Name string
}

// Resumed is what became of one container to bring back.
type Resumed struct {
	Bring
	// What is "started", "running" (its engine brought it back), "failed"
	// (its start failed) or "gone" (removed since, or no engine).
	What string
}

// RunID is the distribution's current start: the kernel's boot and the
// start of its init.
func (p Probe) RunID() string { return RunID(p.BootID, p.InitStart) }

// RunID joins a boot and an init start the way the scripts write them.
func RunID(boot, initStart string) string { return boot + "@" + initStart }

// SetUp reports whether wsl-spawner is set up in the distribution.
func (p Probe) SetUp() bool { return strings.HasPrefix(p.StartScript, StartScriptMark) }

// StartScriptMark starts the first line of the start script; its version
// follows.
const StartScriptMark = "# wsl-spawner start "

// KeptRunning reports whether the service that keeps the distribution
// running is enabled.
func (p Probe) KeptRunning() bool {
	u, ok := p.Units[KeepAliveUnit]
	return ok && u.Enabled == "enabled"
}

// The units wsl-spawner puts in a distribution.
const (
	// StartUnit runs the start script at each start.
	StartUnit = "wsl-spawner.service"
	// KeepAliveUnit keeps the distribution running with nothing open in it.
	KeepAliveUnit = "wsl-spawner-keepalive.service"
)

// ErrNoProbe is output that is not the probe's: a distribution whose sh
// is not one, or WSL saying something instead.
var ErrNoProbe = errors.New("the distribution did not answer the probe")

// Parse reads what the probe printed.
func Parse(out string) (Probe, error) {
	p := Probe{Units: map[string]Unit{}, Engines: map[string]bool{}}
	seen := false
	for line := range strings.SplitSeq(strings.ReplaceAll(out, "\r", ""), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "probe":
			seen = true
		case "boot_id":
			p.BootID = value
		case "init_start":
			p.InitStart = value
		case "init":
			p.Init = value
		case "now":
			if s, err := strconv.ParseInt(value, 10, 64); err == nil {
				p.Now = time.Unix(s, 0)
			}
		case "uptime":
			if s, err := strconv.ParseInt(value, 10, 64); err == nil {
				p.Uptime = time.Duration(s) * time.Second
			}
		case "systemd":
			p.Systemd = value
		case "failed":
			p.Failed = append(p.Failed, value)
		case "unit":
			if f := strings.Split(value, "\t"); len(f) == 3 {
				p.Units[f[0]] = Unit{Active: f[1], Enabled: f[2]}
			}
		case "interop":
			p.Interop = value == "1"
		case "waitfor":
			p.Waitfor = value
		case "docker", "podman":
			p.Engines[key] = value == "up"
		case "docker_live_restore":
			p.LiveRestore = value == "true"
		case "container":
			if c, ok := parseContainer(value); ok {
				p.Containers = append(p.Containers, c)
			}
		case "start_script":
			p.StartScript = value
		case "hook":
			p.Hooks = append(p.Hooks, value)
		case "stop_unit":
			p.StopUnits = append(p.StopUnits, value)
		case "run":
			if r, ok := parseRun(value); ok {
				p.Runs = append(p.Runs, r)
			}
		case "stop":
			p.LastStop = addStopField(p.LastStop, value)
		case "bring":
			if f := strings.Fields(value); len(f) == 3 {
				p.Bring = append(p.Bring, Bring{Engine: f[0], ID: f[1], Name: f[2]})
			}
		case "resumed":
			if at, ok := strings.CutPrefix(value, "at "); ok {
				if s, err := strconv.ParseInt(at, 10, 64); err == nil {
					p.ResumedAt = time.Unix(s, 0)
				}
			} else if f := strings.Fields(value); len(f) == 4 {
				p.Resumed = append(p.Resumed, Resumed{Bring: Bring{Engine: f[0], ID: f[1], Name: f[2]}, What: f[3]})
			}
		case "end":
			p.Complete = true
		}
	}
	if !seen {
		return Probe{}, ErrNoProbe
	}
	return p, nil
}

// unset is what an engine's format prints for a value that is not there.
func unset(s string) string {
	if s == "<no value>" || s == "<nil>" {
		return ""
	}
	return s
}

// project reads a container line's Compose project: its label quoted as Go
// quotes it, as the probe asks the engine for it, or as it is, from a line
// made another way (the demo's). A name with a control character in it is
// none Compose gives, and is no project.
func project(s string) string {
	// Only a quoted field is unquoted: a plain one is the name itself.
	if q, err := strconv.Unquote(s); err == nil && strings.HasPrefix(s, `"`) {
		s = q
	} else {
		s = unset(s)
	}
	if strings.ContainsFunc(s, unicode.IsControl) {
		return ""
	}
	return s
}

// parseContainer reads a container line's fields: engine, id, name,
// state, restart policy, stop timeout, image, project, and its ports when
// a newer probe says them.
func parseContainer(v string) (Container, bool) {
	f := strings.Split(v, "\t")
	if len(f) < 8 || f[1] == "" {
		return Container{}, false
	}
	c := Container{Engine: f[0], ID: f[1], Name: strings.TrimPrefix(f[2], "/"), State: f[3],
		Policy: unset(f[4]), Image: unset(f[6]), Project: project(f[7])}
	// Seconds; anything else is the engine's default.
	if s, err := strconv.Atoi(unset(f[5])); err == nil && s > 0 {
		c.StopTimeout = time.Duration(s) * time.Second
	}
	// Removed as it stops, where the probe says so.
	if len(f) > 9 {
		c.AutoRemove = f[9] == "true"
	}
	// HOSTPORT:PORT/PROTO, each with a comma after; a binding with no host
	// port is the engine's to pick, and none of the host's to know.
	if len(f) > 8 {
		for _, b := range strings.Split(f[8], ",") {
			host, inner, ok := strings.Cut(b, ":")
			_, proto, _ := strings.Cut(inner, "/")
			if ok && host != "" && proto != "" {
				c.Ports = append(c.Ports, host+"/"+proto)
			}
		}
	}
	return c, true
}

// parseRun reads a line of the start script's log: the Unix time of the
// start, then its run id.
func parseRun(v string) (Run, bool) {
	at, id, ok := strings.Cut(v, " ")
	sec, err := strconv.ParseInt(at, 10, 64)
	if !ok || err != nil || id == "" {
		return Run{}, false
	}
	return Run{At: time.Unix(sec, 0), ID: id}, true
}

// addStopField reads one KEY=VALUE line of the last stop's record into r,
// made when the first line comes.
func addStopField(r *StopRecord, line string) *StopRecord {
	if r == nil {
		r = &StopRecord{}
	}
	k, v, _ := strings.Cut(line, "=")
	switch k {
	case "run":
		r.Run = v
	case "at":
		if s, err := strconv.ParseInt(v, 10, 64); err == nil {
			r.At = time.Unix(s, 0)
		}
	case "took":
		if s, err := strconv.ParseInt(v, 10, 64); err == nil {
			r.Took = time.Duration(s) * time.Second
		}
	case "result":
		r.Result = v
	case "by":
		r.By = v
	}
	return r
}

// Ending is how the distribution's start before this one ended.
type Ending int

const (
	// EndUnknown: wsl-spawner is not set up, so no start was noted.
	EndUnknown Ending = iota
	// EndFirst: this is the first start noted since it was set up.
	EndFirst
	// EndClean: wsl-spawner stopped it, every step done.
	EndClean
	// EndLimit: wsl-spawner stopped it, a step cut off at the stop limit.
	EndLimit
	// EndFailed: wsl-spawner stopped it, a step failed.
	EndFailed
	// EndCut: it ended without wsl-spawner's stop: WSL or Windows ended
	// it, with whatever time WSL gives a distribution's services.
	EndCut
)

// LastEnd judges how the start before this one ended, and returns it with
// that start. The start script notes every start; a stop wsl-spawner made
// names the start it stopped. This start may not be noted yet, while the
// start script is still to run. Nor may the one before: a stop can come
// before the script ran, right after a start or while the script waits
// for an engine. That stop's start came after the last one noted when it
// ended after that one began, and is then the start before this one, its
// moment not known.
func (p Probe) LastEnd() (Ending, Run) {
	if !p.SetUp() {
		return EndUnknown, Run{}
	}
	runs := p.Runs
	if n := len(runs); n > 0 && runs[n-1].ID == p.RunID() {
		runs = runs[:n-1]
	}
	if len(runs) == 0 {
		return EndFirst, Run{}
	}
	prev := runs[len(runs)-1]
	if p.LastStop == nil {
		return EndCut, prev
	}
	if p.LastStop.Run != prev.ID {
		// A start never noted: not this one, none of those noted, and
		// stopped after the last one noted began.
		s := p.LastStop
		noted := slices.ContainsFunc(p.Runs, func(r Run) bool { return r.ID == s.Run })
		if s.Run == "" || s.Run == p.RunID() || noted || !s.At.After(prev.At) {
			return EndCut, prev
		}
		prev = Run{ID: s.Run}
	}
	switch p.LastStop.Result {
	case "clean":
		return EndClean, prev
	case "limit":
		return EndLimit, prev
	}
	return EndFailed, prev
}
