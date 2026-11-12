package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
)

// probed is the made-up PC with Ubuntu-24.04's look answering lines, after
// the probe's first line and before its last.
func probed(t *testing.T, lines ...string) string {
	t.Helper()
	a, f := pc(t)
	f.Script = func(_, _, script string) (string, error) {
		if script == inside.ProbeScript() {
			return "probe=1\nsystemd=running\n" + strings.Join(lines, "\n") + "\nend=1\n", nil
		}
		return "", errNotHere
	}
	out, errOut, code := cli(t, a, false, "status", "--json")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	return out
}

// ubuntu is the status of Ubuntu-24.04 in a status --json.
func ubuntu(t *testing.T, out string) api.Probe {
	t.Helper()
	var st api.Status
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}
	for _, d := range st.List {
		if d.Name == "Ubuntu-24.04" && d.Probe != nil {
			return *d.Probe
		}
	}
	t.Fatalf("no probe of Ubuntu-24.04 in %s", out)
	return api.Probe{}
}

func TestStatusSaysEachEnginesStateAndTheUnitsAStopTakes(t *testing.T) {
	// A Docker asleep behind its socket, a stop-units with a unit that
	// runs, one that does not and a typo: the JSON says each engine by its
	// word, and tells the units a stop takes from those it passes by.
	p := ubuntu(t, probed(t, "docker=asleep", "podman=up",
		"stop_unit=postgresql.service\tloaded\tactive", "stop_unit=redis.service\tloaded\tinactive", "stop_unit=postgre.service\tnot-found\tinactive"))
	if p.EngineStates["docker"] != "asleep" || p.EngineStates["podman"] != "up" || p.Engines["docker"] || !p.Engines["podman"] {
		t.Errorf("engines %v, states %v", p.Engines, p.EngineStates)
	}
	if strings.Join(p.StopUnits, ",") != "postgresql.service" || strings.Join(p.StopUnitsSkipped, ",") != "redis.service,postgre.service" {
		t.Errorf("taken %v, skipped %v", p.StopUnits, p.StopUnitsSkipped)
	}
	// A docker whose daemon is another's is not among the engines, and is
	// said by its word.
	p = ubuntu(t, probed(t, "docker=foreign"))
	if _, found := p.Engines["docker"]; found || p.EngineStates["docker"] != "foreign" {
		t.Errorf("engines %v, states %v", p.Engines, p.EngineStates)
	}
}

func TestTheStatusTableSaysAnEngineAsleepOrAnothersQuietly(t *testing.T) {
	// Neither is something wrong: "docker down" in the color of what is
	// worth a look is for a daemon that does not answer.
	for lines, want := range map[string]string{
		"docker=asleep":  "docker asleep",
		"docker=foreign": "docker not its own",
		"docker=down":    "docker down",
	} {
		a, f := pc(t)
		f.Script = func(_, _, script string) (string, error) {
			if script == inside.ProbeScript() {
				return "probe=1\nsystemd=running\n" + lines + "\nend=1\n", nil
			}
			return "", errNotHere
		}
		out, errOut, code := cli(t, a, false, "status")
		if code != exitOK || !strings.Contains(out, want) {
			t.Errorf("%s: exit %d: %s%s", lines, code, out, errOut)
		}
	}
}

func TestAStopsPlanCountsTheUnitsThatRun(t *testing.T) {
	// The plan says what the stop will do: of three names in stop-units,
	// the one that runs.
	a, f := pc(t)
	f.Script = func(_, _, script string) (string, error) {
		if script == inside.ProbeScript() {
			return "probe=1\nsystemd=running\nstop_unit=postgresql.service\tloaded\tactive\n" +
				"stop_unit=redis.service\tloaded\tinactive\nstop_unit=postgre.service\tnot-found\tinactive\nend=1\n", nil
		}
		return "", errNotHere
	}
	out, errOut, _ := cliWithInput(t, a, "n\n", "stop", "Ubuntu-24.04")
	if !strings.Contains(out+errOut, "1 unit") || strings.Contains(out+errOut, "3 units") {
		t.Errorf("%s%s", out, errOut)
	}
}
