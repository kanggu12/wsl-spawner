// Package distros lists the WSL distributions with what wsl-spawner knows of
// each: whether it is wsl-spawner's to stop and start, what the settings
// choose for it, and, for one that runs, what runs in it.
package distros

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// Distro is one distribution as wsl-spawner sees it.
type Distro struct {
	wsl.Distro
	// Owner is the tool that runs this distribution for itself, such as
	// "Docker Desktop", which stops and starts it: wsl-spawner leaves it be.
	Owner string
	// Skipped is set when the settings leave it to WSL.
	Skipped bool
	// AtLogon is set when the settings start it at sign-in.
	AtLogon bool
	// Probe is what runs in it, read when it runs; nil when it does not,
	// or the probe failed (ProbeErr says why).
	Probe    *inside.Probe
	ProbeErr error
	// Asked and Answered are when its probe was asked and when it
	// answered, on the clock it was read by: the probe read the
	// distribution's own clock between the two. Zero when not probed.
	Asked, Answered time.Time
}

// Kind is what wsl-spawner does with a distribution.
type Kind int

const (
	// KindSetUp: wsl-spawner is set up in it: it is stopped with its
	// services' own time, its containers come back, its starts are noted.
	KindSetUp Kind = iota
	// KindStopOnly: read, and not set up in it: it is still stopped with
	// its services' own time.
	KindStopOnly
	// KindSkipped: the settings leave it to WSL.
	KindSkipped
	// KindOwned: another tool runs it (Owner says which).
	KindOwned
	// KindWSL1: WSL 1 runs no VM and no systemd: nothing to stop with time.
	KindWSL1
	// KindNotRead: wsl-spawner's to stop, but what is set up in it is not
	// known: it does not run, or did not answer, and nothing in it is read
	// without starting it. It is stopped with its services' own time all
	// the same.
	KindNotRead
)

// Kind says what wsl-spawner does with the distribution.
func (d Distro) Kind() Kind {
	switch {
	case d.Owner != "":
		return KindOwned
	case d.Version == 1:
		return KindWSL1
	case d.Skipped:
		return KindSkipped
	case d.Probe == nil:
		// Not read: set up or not, it is not known.
		return KindNotRead
	case d.Probe.SetUp():
		return KindSetUp
	}
	return KindStopOnly
}

// Ours reports whether wsl-spawner stops and starts the distribution.
func (d Distro) Ours() bool {
	k := d.Kind()
	return k == KindSetUp || k == KindStopOnly || k == KindNotRead
}

// owners are the distributions other tools register for themselves, by
// name: they start, stop and update them on their own.
var owners = []struct{ prefix, owner string }{
	{"docker-desktop", "Docker Desktop"},
	{"rancher-desktop", "Rancher Desktop"},
	{"podman-machine", "Podman"},
}

// Owner names the tool that registered a distribution of this name for
// itself, or "".
func Owner(name string) string {
	low := strings.ToLower(name)
	for _, o := range owners {
		// The tool's own name, alone or with its suffix ("-data", "-default").
		if low == o.prefix || strings.HasPrefix(low, o.prefix+"-") {
			return o.owner
		}
	}
	return ""
}

// ProbeLimit is how long the probe of one distribution may take: it waits
// on each container engine for 15 seconds at most, and inspects in 30.
var ProbeLimit = 90 * time.Second

// Read lists the distributions, the running ones probed at the same time.
// A distribution that does not run is never started to be read. Each
// probe is timed by this PC's clock.
func Read(ctx context.Context, sys wsl.System, c config.Config) ([]Distro, error) {
	return ReadWithClock(ctx, sys, c, time.Now)
}

// List lists the distributions as Read does, and probes none: for what
// needs only their names, whether they run and what wsl-spawner does with
// them, as a stop that says no plan. One set up reads as stop only, which
// wsl-spawner stops the same way.
func List(ctx context.Context, sys wsl.System, c config.Config) ([]Distro, error) {
	list, err := sys.Distros(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Distro, len(list))
	for i, d := range list {
		out[i] = Distro{Distro: d, Owner: Owner(d.Name), Skipped: c.Skipped(d.Name), AtLogon: c.AtLogon(d.Name)}
	}
	return out, nil
}

// ReadWithClock is Read with the clock each probe is timed by: the App's,
// for one whose clock is not this PC's, as the demo's is.
func ReadWithClock(ctx context.Context, sys wsl.System, c config.Config, now func() time.Time) ([]Distro, error) {
	out, err := List(ctx, sys, c)
	if err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	for i, d := range out {
		// Only a running WSL 2 distribution of no other tool is read.
		if !d.Running || d.Owner != "" || d.Version != 2 {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Its own moments, not the others': the probes run at once,
			// and the slowest would stand for every one.
			out[i].Asked = now()
			out[i].Probe, out[i].ProbeErr = Probe(ctx, sys, out[i].Name)
			out[i].Answered = now()
		}(i)
	}
	wg.Wait()
	return out, nil
}

// Probe reads what runs in one running distribution.
func Probe(ctx context.Context, sys wsl.System, name string) (*inside.Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeLimit)
	defer cancel()
	out, err := sys.Run(ctx, name, "root", inside.ProbeScript())
	if err != nil {
		return nil, err
	}
	p, err := inside.Parse(out)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
