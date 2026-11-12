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

func TestHistoryNamesTheStepAStopsResultIsAbout(t *testing.T) {
	// The check and the docs send a person to the history for the step that
	// ran out of time or failed: it is said after the result, a hook by its
	// file, a failed one with its exit code; a start that failed did not
	// start, which is not a stop.
	at := time.Date(2026, 11, 8, 22, 41, 0, 0, time.UTC)
	entries := []history.Entry{
		{At: at, What: "stop", By: "signout", Distros: []history.Distro{
			{Name: "Dev", Result: "limit", Steps: []history.Step{{Step: "hook", Hook: "10-backup", Code: 0}, {Step: "docker", Code: 124}}},
			{Name: "Work", Result: "failed", Steps: []history.Step{{Step: "hook", Hook: "20-db", Code: 3}}},
			{Name: "Old", Result: "limit"},
		}},
		{At: at.Add(time.Minute), What: "start", By: "logon", Distros: []history.Distro{{Name: "Dev", Result: "error"}}},
	}
	var out strings.Builder
	writeHistory(&out, look{}, entries, true)
	for _, want := range []string{"Dev out of time: docker", "Work a step failed: hook 20-db, exit 3", "Old out of time", "Dev did not start"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in\n%s", want, out.String())
		}
	}
	// A stop whose steps were not kept says no step.
	if strings.Contains(out.String(), "Old out of time:") {
		t.Errorf("a step named for a stop without steps:\n%s", out.String())
	}
}
