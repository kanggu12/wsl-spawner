package cli

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/front/api"
)

// client is api's Client over the commands run in this process on a.
func client(a *app.App) api.Client {
	return api.Client{Run: InProcess(func() (*app.App, error) { return a, nil }), Lang: "en"}
}

func TestTheClientReadsWhatTheCommandsPrint(t *testing.T) {
	a, f := pc(t)
	c := client(a)
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil || len(st.List) != 4 || st.List[0].Probe == nil || len(st.List[0].Probe.Containers) != 2 {
		t.Fatalf("%+v %v", st, err)
	}
	// The steps of a stop come as they happen, the results after.
	var steps []string
	res, err := c.Stop(ctx, []string{"Ubuntu-24.04"}, false, func(e api.StopEvent) { steps = append(steps, e.Event+":"+e.Step) })
	if err != nil || len(res.Results) != 1 || !res.Results[0].Clean || !slices.Contains(steps, "line:docker") || steps[len(steps)-1] != "finished:" {
		t.Fatalf("%+v %v %q", res, err, steps)
	}
	if !slices.Contains(f.CallList(), "terminate:Ubuntu-24.04") {
		t.Fatal("not stopped")
	}
	hist, err := c.History(ctx)
	if err != nil || len(hist) != 1 || hist[0].What != "stop" {
		t.Fatalf("%+v %v", hist, err)
	}
}

func TestAClientErrorSaysWhatTheCommandSaid(t *testing.T) {
	a, _ := pc(t)
	c := client(a)
	// A name WSL does not have: no JSON, the command's own words, code 2.
	_, err := c.Stop(context.Background(), []string{"nosuch"}, false, func(api.StopEvent) {})
	var ce *api.CommandError
	if !errors.As(err, &ce) || !ce.Usage() || !strings.Contains(ce.Error(), "no such distribution: nosuch") || strings.Contains(ce.Error(), "✗") {
		t.Fatalf("%v", err)
	}
	// A check with a grave finding exits 3, and still gives its JSON.
	a.Config.Logon = []string{"Ubuntu-24.04"}
	checkPC(t, a, "")
	ch, err := c.Check(context.Background())
	if err != nil || ch.Worst != "bad" {
		t.Fatalf("%+v %v", ch.Worst, err)
	}
}
