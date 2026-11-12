package cli

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/engine/inside"
	"github.com/kanggu12/wsl-spawner/internal/machine/wsl"
	"github.com/kanggu12/wsl-spawner/internal/settings/config"
)

func TestTheSignInStartWaitsForEveryDistributionAtOnce(t *testing.T) {
	// Three distributions start at sign-in, each with a start service that
	// does not finish in its time. One waited after the other, the third
	// started minutes late; each now waits its own limit, all at once, and
	// what is said and kept stays in the settings' order.
	defer func(w, p time.Duration) { startWait, startPoll = w, p }(startWait, startPoll)
	startWait, startPoll = time.Second, 20*time.Millisecond
	f := &wsl.Fake{List: []wsl.Distro{{Name: "A", Version: 2}, {Name: "B", Version: 2}, {Name: "C", Version: 2}}}
	// Set up in each, with a container to bring back, its service still
	// at work.
	f.Script = func(distro, user, script string) (string, error) {
		if script == inside.ProbeScript() {
			return "probe=1\nstart_script=" + inside.StartScriptMark + inside.StartScriptVersion + "\nunit=" + inside.StartUnit +
				"\tactivating\tenabled\nbring=docker aa11 web\nend=1\n", nil
		}
		return "", errors.New("unexpected script")
	}
	c := config.Default()
	c.Logon = []string{"C", "A", "B"}
	a := &app.App{Config: c, ConfigPath: filepath.Join(t.TempDir(), "config.toml"), WSL: f, Dir: t.TempDir(), PC: testPC()}
	began := time.Now()
	out, errOut, code := cli(t, a, false, "logon")
	took := time.Since(began)
	if code != exitOK {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	// About one limit, not three.
	if took > 2*startWait {
		t.Fatalf("took %s for three waits of %s each\n%s", took, startWait, out)
	}
	// Said in the settings' order, each once.
	if ci, ai, bi := strings.Index(out, "C started"), strings.Index(out, "A started"), strings.Index(out, "B started"); ci < 0 || !(ci < ai && ai < bi) {
		t.Fatalf("said:\n%s", out)
	}
	all, err := history.Open(a.Dir).All()
	if err != nil || len(all) != 1 {
		t.Fatalf("%+v %v", all, err)
	}
	var names []string
	for _, d := range all[0].Distros {
		names = append(names, d.Name)
	}
	if !slices.Equal(names, []string{"C", "A", "B"}) {
		t.Fatalf("kept %q", names)
	}
}
