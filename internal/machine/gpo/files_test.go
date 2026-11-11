package gpo

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// policyIn is a made-up policy folder: the paths of its scripts.ini and
// gpt.ini, which are not there yet.
func policyIn(t *testing.T) (scripts, gpt string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "User", "Scripts", "scripts.ini"), filepath.Join(dir, "gpt.ini")
}

// adding and removing are the hook's changes, as Install and Uninstall make
// them.
func adding(text string) (string, error)   { return WithHook(text, exe, "logoff") }
func removing(text string) (string, error) { return WithoutHook(text, exe) }

// read is the file at p, failing the test when it cannot be read.
func read(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAChangeKeepsWhatWasThereOnceAndForWhatItWas(t *testing.T) {
	scripts, gpt := policyIn(t)
	var made []string
	mark := func(p string) { made = append(made, p) }
	// No policy files yet: both are made, and the copies from before are
	// the none they were: empty.
	if err := changeFiles(scripts, gpt, adding, os.WriteFile, mark); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{scripts, gpt} {
		if b := read(t, p+".before-wsl-spawner"); len(b) != 0 {
			t.Fatalf("%s kept as %q", p, b)
		}
	}
	text, err := decodeScripts(read(t, scripts))
	if got := Logoff(text); err != nil || len(got) != 1 || !Ours(got[0], exe) || !slices.Equal(made, []string{scripts}) {
		t.Fatalf("%+v %v, marked %q", got, err, made)
	}
	// Taken out again: the copies stay the ones from before wsl-spawner,
	// not its own files.
	if err := changeFiles(scripts, gpt, removing, os.WriteFile, mark); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{scripts, gpt} {
		if b := read(t, p+".before-wsl-spawner"); len(b) != 0 {
			t.Fatalf("%s kept as %q after the second change", p, b)
		}
	}
	if !strings.Contains(string(read(t, gpt)), "Version=131072\r\n") || len(made) != 1 {
		t.Fatalf("%q, marked %q", read(t, gpt), made)
	}
	// The person's own files are kept as they were, byte for byte.
	scripts, gpt = policyIn(t)
	theirs := encodeScripts("\r\n[Logoff]\r\n0CmdLine=C:\\Tools\\notes.cmd\r\n0Parameters=\r\n")
	for p, b := range map[string][]byte{scripts: theirs, gpt: []byte("[General]\r\nVersion=3\r\n")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := changeFiles(scripts, gpt, adding, os.WriteFile, mark); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(read(t, scripts+".before-wsl-spawner"), theirs) || string(read(t, gpt+".before-wsl-spawner")) != "[General]\r\nVersion=3\r\n" {
		t.Fatal("the files from before were not kept as they were")
	}
}

func TestAChangeIsMadeWholeOrNotAtAll(t *testing.T) {
	scripts, gpt := policyIn(t)
	theirs := encodeScripts("[Logoff]\r\n0CmdLine=C:\\Tools\\notes.cmd\r\n0Parameters=\r\n")
	gptBefore := []byte("[General]\r\nVersion=65537\r\n")
	put := func() {
		t.Helper()
		for p, b := range map[string][]byte{scripts: theirs, gpt: gptBefore} {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	asBefore := func(what string) {
		t.Helper()
		if !slices.Equal(read(t, scripts), theirs) || !slices.Equal(read(t, gpt), gptBefore) {
			t.Fatalf("%s: scripts.ini %q, gpt.ini %q", what, read(t, scripts), read(t, gpt))
		}
	}
	// gpt.ini cannot be written: scripts.ini, written already, goes back.
	put()
	failed := false
	failGpt := func(p string, b []byte, m os.FileMode) error {
		if p == gpt && !failed {
			// As a write cut short, once: the file emptied, then the error.
			failed = true
			_ = os.WriteFile(p, nil, m)
			return errors.New("the disk is full")
		}
		return os.WriteFile(p, b, m)
	}
	err := changeFiles(scripts, gpt, adding, failGpt, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "writing "+gpt+": the disk is full; the policy is as it was") {
		t.Fatalf("%v", err)
	}
	asBefore("gpt.ini not written")
	// scripts.ini cut short as it is written, once: it goes back too.
	failed = false
	cutShort := func(always bool) func(string, []byte, os.FileMode) error {
		return func(p string, b []byte, m os.FileMode) error {
			if p == scripts && (always || !failed) {
				failed = true
				_ = os.WriteFile(p, b[:3], m)
				return errors.New("the disk is full")
			}
			return os.WriteFile(p, b, m)
		}
	}
	if err := changeFiles(scripts, gpt, adding, cutShort(false), func(string) {}); err == nil || !strings.Contains(err.Error(), "the policy is as it was") {
		t.Fatalf("%v", err)
	}
	asBefore("scripts.ini not written")
	// Cut short each time: what could not be put back is named, with where
	// it is kept as it was.
	if err := changeFiles(scripts, gpt, adding, cutShort(true), func(string) {}); err == nil || !strings.Contains(err.Error(), scripts+" could not be put back as it was: "+scripts+".before-wsl-spawner keeps it as it was before wsl-spawner first changed it") {
		t.Fatalf("%v", err)
	}
	if !slices.Equal(read(t, scripts+".before-wsl-spawner"), theirs) {
		t.Fatal("the copy from before is not the person's file")
	}
}

func TestBothFilesLeftAreNamedEachWithItsOwnCopy(t *testing.T) {
	// gpt.ini cannot be written, then neither file can be put back: the
	// words name both, each with the copy that keeps it, not the first
	// one's copy for both.
	scripts, gpt := policyIn(t)
	theirs := encodeScripts("[Logoff]\r\n0CmdLine=C:\\Tools\\notes.cmd\r\n0Parameters=\r\n")
	gptBefore := []byte("[General]\r\nVersion=65537\r\n")
	for p, b := range map[string][]byte{scripts: theirs, gpt: gptBefore} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	changed := false
	stuck := func(p string, b []byte, m os.FileMode) error {
		switch {
		case p == scripts && !changed:
			// The change of scripts.ini itself goes through, whole.
			changed = true
			return os.WriteFile(p, b, m)
		case p == scripts || p == gpt:
			// Every later write of either file is cut short.
			_ = os.WriteFile(p, b[:min(len(b), 3)], m)
			return errors.New("the disk is full")
		}
		// The copies from before are written as asked.
		return os.WriteFile(p, b, m)
	}
	err := changeFiles(scripts, gpt, adding, stuck, func(string) {})
	// gpt.ini first: it is the one whose write failed, put back first.
	want := gpt + " and " + scripts + " could not be put back as they were: " + gpt + ".before-wsl-spawner and " + scripts +
		".before-wsl-spawner keep them as they were before wsl-spawner first changed them"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%v", err)
	}
	// Each copy named holds its own file as the person had it.
	if !slices.Equal(read(t, scripts+".before-wsl-spawner"), theirs) || !slices.Equal(read(t, gpt+".before-wsl-spawner"), gptBefore) {
		t.Fatal("a copy from before is not the person's file")
	}
}

func TestAChangeWithNothingToChangeOrThatIsUnsureWritesNothing(t *testing.T) {
	scripts, gpt := policyIn(t)
	nothing := func(p string, b []byte, m os.FileMode) error {
		t.Fatalf("%s written", p)
		return nil
	}
	// No hook to take out of no file: nothing written, nothing kept.
	if err := changeFiles(scripts, gpt, removing, nothing, func(string) {}); err != nil {
		t.Fatal(err)
	}
	// A scripts.ini in a code page: refused, with where it is.
	if err := os.MkdirAll(filepath.Dir(scripts), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scripts, []byte("[Logoff]\r\n0CmdLine=C:\\Users\\\xb1\xe8\\x.cmd\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var u *Unsure
	if err := changeFiles(scripts, gpt, adding, nothing, func(string) {}); !errors.As(err, &u) || u.File != scripts {
		t.Fatalf("%v", err)
	}
	// So with a gpt.ini it cannot count on, named too.
	if err := os.WriteFile(scripts, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gpt, []byte("[General]\r\nVersion=x\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := changeFiles(scripts, gpt, adding, nothing, func(string) {}); !errors.As(err, &u) || u.File != gpt {
		t.Fatalf("%v", err)
	}
	if _, err := os.Lstat(scripts + ".before-wsl-spawner"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a copy was kept of a change that was not made")
	}
}
