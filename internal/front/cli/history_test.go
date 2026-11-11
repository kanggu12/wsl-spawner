package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/engine/history"
)

func TestHistoryListsNewestFirstAndClears(t *testing.T) {
	a, _ := pc(t)
	if out, _, code := cli(t, a, false, "history"); code != exitOK || !strings.Contains(out, "no stop or start yet") {
		t.Fatalf("exit %d: %s", code, out)
	}
	s := history.Open(a.Dir)
	at := time.Date(2026, 11, 10, 22, 36, 0, 0, time.Local)
	for _, e := range []history.Entry{
		{At: at, What: "stop", By: "signout", Shutdown: true, Took: 41.4, Distros: []history.Distro{{Name: "Dev", Result: "clean"}, {Name: "Work", Result: "limit"}}},
		{At: at.Add(time.Hour), What: "start", By: "logon", Took: 2, Distros: []history.Distro{{Name: "Dev", Result: "started"}}},
	} {
		if err := s.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	out, _, _ := cli(t, a, false, "history")
	start, stop := strings.Index(out, "2026-11-10 23:36  start"), strings.Index(out, "2026-11-10 22:36  stop + WSL  sign-out")
	if start < 0 || stop < 0 || start > stop || !strings.Contains(out, "Dev clean, Work out of time") || !strings.Contains(out, "41 s") {
		t.Fatalf("%s", out)
	}
	out, _, _ = cli(t, a, false, "history", "--json")
	var got struct {
		Schema  int             `json:"schema"`
		Entries []history.Entry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Entries) != 2 || got.Entries[0].By != "signout" {
		t.Fatalf("%v %+v", err, got)
	}
	// Clearing asks; a script says --yes.
	if _, errOut, code := cli(t, a, false, "history", "--clear"); code != exitUsage || !strings.Contains(errOut, "add --yes") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if out, _, code := cli(t, a, false, "history", "--clear", "--yes"); code != exitOK || !strings.Contains(out, "history removed") {
		t.Fatalf("exit %d: %s", code, out)
	}
	if all, _ := s.All(); len(all) != 0 {
		t.Fatalf("%+v", all)
	}
	// With --json, what is left is JSON too: no entries.
	if out, _, code := cli(t, a, false, "history", "--clear", "--yes", "--json"); code != exitOK || strings.TrimSpace(out) != "{\n  \"schema\": 1,\n  \"entries\": []\n}" {
		t.Fatalf("exit %d: %q", code, out)
	}
}
