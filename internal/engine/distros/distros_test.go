package distros

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

// madeUp is a PC with one of each kind of distribution.
func madeUp() *wsl.Fake {
	return &wsl.Fake{List: []wsl.Distro{
		{Name: "Ubuntu-24.04", Version: 2, Running: true, Default: true},
		{Name: "Debian", Version: 2},
		{Name: "docker-desktop", Version: 2, Running: true},
		{Name: "Old", Version: 1, Running: true},
		{Name: "Work", Version: 2, Running: true},
	}}
}

func TestReadProbesOnlyWhatRunsAndIsOurs(t *testing.T) {
	f := madeUp()
	f.Script = func(distro, user, script string) (string, error) {
		// Every probe runs as root, and is the probe.
		if user != "root" || script != inside.ProbeScript() {
			return "", errors.New("not the probe")
		}
		if distro == "Ubuntu-24.04" {
			return "probe=1\nstart_script=" + inside.StartScriptMark + inside.StartScriptVersion + "\nend=1\n", nil
		}
		return "probe=1\nend=1\n", nil
	}
	c := config.Default()
	c.Skip = []string{"work"}
	c.Logon = []string{"Ubuntu-24.04"}
	ds, err := Read(context.Background(), f, c)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]Kind{}
	for _, d := range ds {
		kinds[d.Name] = d.Kind()
	}
	// Debian does not run, so what is set up in it is not read.
	want := map[string]Kind{"Ubuntu-24.04": KindSetUp, "Debian": KindNotRead, "docker-desktop": KindOwned, "Old": KindWSL1, "Work": KindSkipped}
	for name, k := range want {
		if kinds[name] != k {
			t.Errorf("%s: kind %v, want %v", name, kinds[name], k)
		}
	}
	// Only the running WSL 2 ones of no other tool were read: a stopped
	// one is never started to be read, and one left to WSL is still read.
	var probed []string
	for _, call := range f.CallList() {
		probed = append(probed, strings.TrimPrefix(call, "run:"))
	}
	slices.Sort(probed)
	if !slices.Equal(probed, []string{"Ubuntu-24.04", "Work"}) {
		t.Fatalf("probed %q", probed)
	}
	if !ds[0].AtLogon || ds[1].AtLogon || ds[2].Owner != "Docker Desktop" {
		t.Fatalf("%+v", ds)
	}
}

func TestAProbeThatFailsIsSaidAndTheRestGoesOn(t *testing.T) {
	f := madeUp()
	f.Script = func(distro, _, _ string) (string, error) {
		if distro == "Ubuntu-24.04" {
			return "", errors.New("boom")
		}
		return "not the probe", nil
	}
	ds, err := Read(context.Background(), f, config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if ds[0].Probe != nil || ds[0].ProbeErr == nil || !strings.Contains(ds[0].ProbeErr.Error(), "boom") {
		t.Fatalf("%+v", ds[0])
	}
	// What is not the probe's output is said as such.
	if !errors.Is(ds[4].ProbeErr, inside.ErrNoProbe) {
		t.Fatalf("%+v", ds[4])
	}
	// Not knowing what runs inside, it is still one wsl-spawner stops.
	if ds[0].Kind() != KindNotRead || !ds[0].Ours() {
		t.Fatalf("kind %v", ds[0].Kind())
	}
}

func TestEachProbeIsTimedByItsOwnAskAndAnswer(t *testing.T) {
	// A clock that moves a second each time it is read: the probe reads it
	// too, between its ask and its answer.
	var mu sync.Mutex
	tick := time.Unix(1800000000, 0)
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		tick = tick.Add(time.Second)
		return tick
	}
	during := map[string]time.Time{}
	f := madeUp()
	f.Script = func(distro, _, _ string) (string, error) {
		// The fake calls one script at a time: no lock of its own needed.
		during[distro] = clock()
		return "probe=1\nend=1\n", nil
	}
	ds, err := ReadWithClock(context.Background(), f, config.Default(), clock)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		at, probed := during[d.Name]
		switch {
		case !probed && (!d.Asked.IsZero() || !d.Answered.IsZero()):
			t.Errorf("%s was not probed, and has moments %s %s", d.Name, d.Asked, d.Answered)
		case probed && !(d.Asked.Before(at) && at.Before(d.Answered)):
			t.Errorf("%s: asked %s, read at %s, answered %s", d.Name, d.Asked, at, d.Answered)
		}
	}
	// The running ones of no other tool were probed.
	if len(during) != 2 {
		t.Fatalf("probed %v", during)
	}
}

func TestOwner(t *testing.T) {
	for name, want := range map[string]string{
		"docker-desktop": "Docker Desktop", "docker-desktop-data": "Docker Desktop", "Docker-Desktop": "Docker Desktop",
		"rancher-desktop": "Rancher Desktop", "rancher-desktop-data": "Rancher Desktop",
		"podman-machine-default": "Podman", "podman-machine-work": "Podman",
		// Names that only start the same way are the person's own.
		"docker-desktopper": "", "Ubuntu": "", "podman": "", "my-docker-desktop": "",
	} {
		if got := Owner(name); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

func TestAWSLThatDoesNotListIsAnError(t *testing.T) {
	f := madeUp()
	f.DistrosErr = wsl.ErrNoAnswer
	if _, err := Read(context.Background(), f, config.Default()); !errors.Is(err, wsl.ErrNoAnswer) {
		t.Fatalf("got %v", err)
	}
}

func TestAProbeCutOffAtItsLimitKeepsWhatItRead(t *testing.T) {
	// The probe runs out of time after it read systemd and before its end,
	// as one waiting on a daemon that hangs does: what it read is kept, not
	// complete, with the error beside it.
	f := madeUp()
	f.Script = func(distro, user, script string) (string, error) {
		return "probe=1\nsystemd=running\nengine=docker\n", fmt.Errorf("%w: wsl ran for 1m30s", wsl.ErrNoAnswer)
	}
	p, err := Probe(context.Background(), f, "Ubuntu-24.04")
	if p == nil || p.Complete || p.Systemd != "running" || !errors.Is(err, ErrReadInPart) || !errors.Is(err, wsl.ErrNoAnswer) {
		t.Fatalf("%+v %v", p, err)
	}
	// Out of time with nothing of the probe's own: nothing is made up.
	f.Script = func(distro, user, script string) (string, error) {
		return "", fmt.Errorf("%w: wsl ran for 1m30s", wsl.ErrNoAnswer)
	}
	if p, err := Probe(context.Background(), f, "Ubuntu-24.04"); p != nil || err == nil || errors.Is(err, ErrReadInPart) {
		t.Fatalf("%+v %v", p, err)
	}
	// Any other failure keeps what it printed out of it.
	f.Script = func(distro, user, script string) (string, error) {
		return "probe=1\nsystemd=running\n", errors.New("exit status 1")
	}
	if p, err := Probe(context.Background(), f, "Ubuntu-24.04"); p != nil || err == nil {
		t.Fatalf("%+v %v", p, err)
	}
}
