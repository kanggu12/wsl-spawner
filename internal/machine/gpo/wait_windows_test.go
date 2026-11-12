//go:build windows

package gpo

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The registry's own store, in keys of the test's own under the user's
// hive: it writes the registry, so it runs on GitHub's runners alone, never
// on a person's PC.
func TestTheRegistryStoreRaisesAndPutsBack(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Skip("writes the registry: only on GitHub's runners")
	}
	top := fmt.Sprintf(`Software\wsl-spawner-test-%d`, os.Getpid())
	s := regStore{registry.CURRENT_USER, top + `\Policy`, top + `\Note`}
	t.Cleanup(func() {
		registry.DeleteKey(registry.CURRENT_USER, top+`\Policy`)
		registry.DeleteKey(registry.CURRENT_USER, top+`\Note`)
		registry.DeleteKey(registry.CURRENT_USER, top)
	})
	// None set: raised, its note written, then both taken out.
	if w, err := s.Wait(); err != nil || w.Set {
		t.Fatalf("got %+v %v", w, err)
	}
	if _, err := Raise(s, 420); err != nil {
		t.Fatal(err)
	}
	if w, _ := s.Wait(); !w.Set || w.Seconds != 420 {
		t.Fatalf("wait %+v", w)
	}
	if n, ok, err := s.Note(); err != nil || !ok || n.To != 420 || n.Before.Set {
		t.Fatalf("note %+v %v %v", n, ok, err)
	}
	if back, done, err := PutBack(s); err != nil || !done || back.Set {
		t.Fatalf("put back %+v %v %v", back, done, err)
	}
	if w, _ := s.Wait(); w.Set {
		t.Fatalf("wait %+v", w)
	}
	// The note's key goes with the note.
	if _, err := registry.OpenKey(registry.CURRENT_USER, top+`\Note`, registry.QUERY_VALUE); err != registry.ErrNotExist {
		t.Fatalf("the note's key: %v", err)
	}
	// One set: back to it.
	if err := s.SetWait(Wait{Seconds: 300, Set: true}); err != nil {
		t.Fatal(err)
	}
	Raise(s, 480)
	if back, done, err := PutBack(s); err != nil || !done || back.Seconds != 300 {
		t.Fatalf("put back %+v %v %v", back, done, err)
	}
	if w, _ := s.Wait(); w.Seconds != 300 {
		t.Fatalf("wait %+v", w)
	}
}

func TestAReadOfThisPCsWaitOnlyReads(t *testing.T) {
	// Read only: whatever this PC's policy holds, a number or none.
	w, err := machine.Wait()
	if err != nil || (w.Set && w.Seconds < 0) {
		t.Fatalf("got %+v %v", w, err)
	}
	// Changes are refused in a test, whoever runs it.
	if _, err := RaiseWait(420); err != errNotInTests {
		t.Fatalf("got %v", err)
	}
	if _, _, err := PutBackWait(); err != errNotInTests {
		t.Fatalf("got %v", err)
	}
}
