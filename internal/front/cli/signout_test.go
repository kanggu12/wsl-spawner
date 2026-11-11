package cli

import (
	"testing"

	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
)

func TestAStopThatPrintsNoPlanProbesNothingFirst(t *testing.T) {
	// The probe is for the plan a stop says before it asks. The sign-out
	// hook says none, nor does a stop for a script: reading every
	// distribution first only spends the time Windows waits for the hook.
	for _, c := range []struct {
		args   []string
		probes int
	}{
		{[]string{"logoff"}, 0},
		{[]string{"stop", "--yes", "--json", "--all"}, 0},
		// A stop by hand says its plan, from what runs in each: one probe
		// of each running distribution, Work left to WSL included.
		{[]string{"stop", "--yes", "Ubuntu-24.04"}, 2},
	} {
		a, f := pc(t)
		// Every probe is counted; the fake runs one script at a time.
		probes, script := 0, f.Script
		f.Script = func(distro, user, s string) (string, error) {
			if s == inside.ProbeScript() {
				probes++
			}
			return script(distro, user, s)
		}
		if out, errOut, code := cli(t, a, false, c.args...); code != exitOK {
			t.Fatalf("%q: exit %d: %s%s", c.args, code, out, errOut)
		}
		if probes != c.probes {
			t.Errorf("%q: %d probes, want %d", c.args, probes, c.probes)
		}
	}
}
