package history

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAddAndCap(t *testing.T) {
	s := Open(t.TempDir())
	// No file yet: no history, no error.
	if all, err := s.All(); err != nil || len(all) != 0 {
		t.Fatalf("%v %v", all, err)
	}
	// A full history and then some, written as Add writes it, and one Add
	// more: every Add writes the file whole and waits for the drive, so
	// adding them one by one is what would take the time.
	var lines []byte
	for i := range maxEntries + 19 {
		b, err := json.Marshal(Entry{At: time.Unix(int64(i), 0), What: "stop", Took: float64(i)})
		if err != nil {
			t.Fatal(err)
		}
		lines = append(append(lines, b...), '\n')
	}
	if err := os.WriteFile(s.Path, lines, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(Entry{At: time.Unix(int64(maxEntries+19), 0), What: "stop", Took: float64(maxEntries + 19)}); err != nil {
		t.Fatal(err)
	}
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	// Only the newest maxEntries stay, oldest first.
	if len(all) != maxEntries || all[0].Took != 20 || all[len(all)-1].Took != maxEntries+19 {
		t.Fatalf("%d entries, first %v, last %v", len(all), all[0].Took, all[len(all)-1].Took)
	}
	// No temporary file stays beside it.
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(s.Path), "history-*.tmp")); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

func TestDamagedLinesAreSkipped(t *testing.T) {
	s := Open(t.TempDir())
	good, _ := json.Marshal(Entry{What: "stop", By: "signout"})
	// A line cut short by a crash, one far too long, and an empty one.
	long := `{"what":"stop","error":"` + strings.Repeat("x", maxLine*2) + `"}`
	body := string(good) + "\n{\"what\":\"st\n" + long + "\n\n" + string(good) + "\n"
	if err := os.WriteFile(s.Path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	all, err := s.All()
	if err != nil || len(all) != 2 || all[1].By != "signout" {
		t.Fatalf("%+v %v", all, err)
	}
}

func TestAddsAtTheSameTimeEachKeepTheirEntry(t *testing.T) {
	// Programs adding at once, as a stop going on without its terminal and
	// another command do: no entry is lost to another's write.
	s := Open(t.TempDir())
	const n = 40
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// All at once, as far as the scheduler allows.
			<-start
			errs <- s.Add(Entry{What: "stop", Took: float64(i)})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.All()
	if err != nil || len(all) != n {
		t.Fatalf("%d of %d entries kept, %v", len(all), n, err)
	}
}

func TestAnAddWaitsForAWriteUnderWay(t *testing.T) {
	// Another program's write is under way: an Add waits for it to end,
	// and then adds after it.
	dir := t.TempDir()
	s := Open(dir)
	under := filepath.Join(dir, "history-new.tmp")
	if err := os.WriteFile(under, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Add(Entry{What: "stop"}) }()
	select {
	case err := <-done:
		t.Fatalf("added beside a write under way: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	// The other write ends.
	if err := os.Remove(under); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still waiting")
	}
	if all, _ := s.All(); len(all) != 1 {
		t.Fatalf("%+v", all)
	}
}

func TestAWriteLeftBehindByAProgramThatEndedIsTakenOver(t *testing.T) {
	// A program that ended halfway left its write's file: the next Add
	// takes it over rather than waiting for ever.
	dir := t.TempDir()
	s := Open(dir)
	left := filepath.Join(dir, "history-new.tmp")
	if err := os.WriteFile(left, []byte("{\"what\":\"st"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(left, old, old); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(Entry{What: "stop"}); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.All(); len(all) != 1 {
		t.Fatalf("%+v", all)
	}
}

func TestAnAddKeepsTheLinesItCannotRead(t *testing.T) {
	// A line this version cannot read, one cut short or one a newer
	// version wrote, stays as it is: the history is left as it was, the
	// new entry after it.
	s := Open(t.TempDir())
	good, _ := json.Marshal(Entry{What: "stop", By: "signout"})
	newer := `{"what":"stop","took_seconds":"a newer version's"}`
	body := string(good) + "\n{\"what\":\"st\n" + newer + "\n"
	if err := os.WriteFile(s.Path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(Entry{What: "start", By: "logon"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	added, _ := json.Marshal(Entry{What: "start", By: "logon", Distros: []Distro{}})
	if string(b) != body+string(added)+"\n" {
		t.Fatalf("%q", b)
	}
}

func TestEntryJSONKeys(t *testing.T) {
	// The keys are what scripts read from history --json: they stay.
	e := Entry{At: time.Unix(0, 0).UTC(), Took: 1.5, What: "stop", By: "hand", Shutdown: true, Version: "0.1.0",
		Distros: []Distro{{Name: "Dev", Result: "clean", Took: 1, Steps: []Step{{Step: "docker", Count: 2, Took: 1}}}}}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"at":"1970-01-01T00:00:00Z","took_seconds":1.5,"what":"stop","by":"hand","shutdown":true,` +
		`"distributions":[{"name":"Dev","result":"clean","took_seconds":1,"steps":[{"step":"docker","count":2,"code":0,"took_seconds":1}]}],"version":"0.1.0"}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestClearRemovesTheEntriesAndWhatACutShortWriteLeft(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	if err := s.Add(Entry{What: "stop"}); err != nil {
		t.Fatal(err)
	}
	// What a crash between writing and renaming leaves.
	tmp := filepath.Join(dir, "history-123.tmp")
	if err := os.WriteFile(tmp, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Another program's file in the folder stays.
	other := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(other, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{s.Path, tmp} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s stayed", p)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("another file went")
	}
	// Nothing to clear is no error.
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
}

func TestAnEntryWithNothingStoppedHasAnEmptyList(t *testing.T) {
	// The sign-out hook with nothing running: its entry lists no
	// distribution, as [] in the JSON, never null; so does one an earlier
	// version wrote with null.
	s := Open(t.TempDir())
	if err := s.Add(Entry{What: "stop", By: "signout", Shutdown: true}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"distributions":[]`)) {
		t.Fatalf("%s", b)
	}
	if err := os.WriteFile(s.Path, []byte(`{"what":"stop","by":"hand","distributions":null}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	all, err := s.All()
	if err != nil || len(all) != 1 || all[0].Distros == nil {
		t.Fatalf("%v %#v", err, all)
	}
}

func TestAWriteLeftBehindDatedAheadIsTakenOver(t *testing.T) {
	// Left before the clock was set back, the write's file is dated ahead:
	// it is left over all the same, and no Add waits for it.
	dir := t.TempDir()
	s := Open(dir)
	left := filepath.Join(dir, "history-new.tmp")
	if err := os.WriteFile(left, []byte("{\"what\":\"st"), 0o600); err != nil {
		t.Fatal(err)
	}
	ahead := time.Now().Add(time.Hour)
	if err := os.Chtimes(left, ahead, ahead); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := s.Add(Entry{What: "stop"}); err != nil || time.Since(start) > 5*time.Second {
		t.Fatalf("%v after %s", err, time.Since(start))
	}
	if all, _ := s.All(); len(all) != 1 {
		t.Fatalf("%+v", all)
	}
}

func TestAnAddWaitsForAReaderToLetGo(t *testing.T) {
	// A program reads the history as a stop ends: on Windows the open file
	// cannot be replaced until it is let go, which takes a moment. The
	// stop's entry is written once it is.
	dir := t.TempDir()
	s := Open(dir)
	if err := s.Add(Entry{What: "start"}); err != nil {
		t.Fatal(err)
	}
	r, err := os.Open(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Add(Entry{What: "stop"}) }()
	// The reader lets go a moment later.
	time.Sleep(300 * time.Millisecond)
	r.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if all, _ := s.All(); len(all) != 2 || all[1].What != "stop" {
		t.Fatalf("%+v", all)
	}
}
