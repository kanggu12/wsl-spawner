package setup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
)

// yes and no are a choice asked for.
var yes, no = func() *bool { b := true; return &b }(), func() *bool { b := false; return &b }()

func TestWantFollowsWhatEachChoiceNeeds(t *testing.T) {
	all := State{Managed: true, Keep: true, Logon: true}
	for _, c := range []struct {
		name string
		cur  State
		ask  Ask
		want State
	}{
		{"sign-in turns on what it needs", State{Skip: true}, Ask{Logon: yes}, all},
		{"keeping it running sets it up", State{}, Ask{Keep: yes}, State{Managed: true, Keep: true}},
		{"set up alone", State{}, Ask{Managed: yes}, State{Managed: true}},
		{"taking it out takes the rest", all, Ask{Managed: no}, State{}},
		{"not kept running does not start at sign-in", all, Ask{Keep: no}, State{Managed: true}},
		{"sign-in off keeps the rest", all, Ask{Logon: no}, State{Managed: true, Keep: true}},
		// Leaving it to WSL is asked of one set up: it wins over what is on.
		{"left to WSL takes everything", all, Ask{Skip: yes}, State{Skip: true}},
		{"back from WSL is just that", State{Skip: true}, Ask{Skip: no}, State{}},
		{"nothing asked, nothing changes", all, Ask{}, all},
	} {
		got, err := Want(c.cur, c.ask)
		if err != nil || got != c.want {
			t.Errorf("%s: %+v %v, want %+v", c.name, got, err, c.want)
		}
	}
}

func TestWantRefusesAChoiceAskedBothWays(t *testing.T) {
	for _, a := range []Ask{
		{Logon: yes, Keep: no},
		{Logon: yes, Managed: no},
		{Logon: yes, Skip: yes},
		{Keep: yes, Managed: no},
		{Keep: yes, Skip: yes},
		{Managed: yes, Skip: yes},
	} {
		if _, err := Want(State{}, a); err == nil {
			t.Errorf("%+v went through", a)
		}
	}
}

func TestInside(t *testing.T) {
	for _, c := range []struct {
		cur, w State
		want   bool
	}{
		{State{}, State{Managed: true}, true},
		{State{Managed: true}, State{}, true},
		{State{Managed: true}, State{Managed: true, Keep: true}, true},
		// Only the settings change: the distribution is not touched.
		{State{Managed: true, Keep: true}, State{Managed: true, Keep: true, Logon: true}, false},
		{State{}, State{Skip: true}, false},
	} {
		if got := Inside(c.cur, c.w); got != c.want {
			t.Errorf("%+v to %+v: %v", c.cur, c.w, got)
		}
	}
}

func TestInAndOutSayWhatIsMissing(t *testing.T) {
	f := &wsl.Fake{List: []wsl.Distro{{Name: "Dev", Version: 2, Running: true}}}
	answers := map[string]string{}
	f.Script = func(_, user, script string) (string, error) {
		if user != "root" {
			return "", errors.New("not as root")
		}
		for prefix, out := range answers {
			if strings.Contains(script, prefix) {
				if out == "fail" {
					return "error=systemd\n", errors.New("exit status 3")
				}
				return out, nil
			}
		}
		return "", errors.New("unexpected")
	}
	answers[inside.Signal("Dev")] = "ok=1\n"
	if err := In(context.Background(), f, "Dev", true); err != nil {
		t.Fatal(err)
	}
	answers = map[string]string{inside.StartUnit: "fail"}
	if err := In(context.Background(), f, "Dev", false); !errors.Is(err, ErrNoSystemd) {
		t.Fatalf("got %v", err)
	}
	answers = map[string]string{"rm -rf": "half\n"}
	if err := Out(context.Background(), f, "Dev"); err == nil || !strings.Contains(err.Error(), "did not end: half") {
		t.Fatalf("got %v", err)
	}
}
