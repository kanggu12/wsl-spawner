//go:build windows

package winapi

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary play another copy of the program: started
// with WAIT_CHILD set, it runs until its stdin closes.
func TestMain(m *testing.M) {
	if os.Getenv("WAIT_CHILD") != "" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRunningKnowsThisProgramByName(t *testing.T) {
	// The journal's check: this process runs, under its own name; the same id
	// under another name is another program; an id no process has is none.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(self)
	if !Running(os.Getpid(), name) || !Running(os.Getpid(), strings.ToUpper(name)) {
		t.Fatalf("this process (%s) does not read as running", name)
	}
	if Running(os.Getpid(), "another.exe") {
		t.Fatal("another name read as this program")
	}
	if Running(0x7ffffff0, name) {
		t.Fatal("an id no process has read as running")
	}
}

func TestStartingThisProgramAgainRefusesInATest(t *testing.T) {
	// Whatever a test does, it starts no copy of the program on its own or
	// as an administrator, which could ask Windows' question on the screen.
	if err := StartDetached([]string{"version"}); !errors.Is(err, errNoProgramInTests) {
		t.Fatalf("StartDetached: %v", err)
	}
	if _, err := RunElevated(`C:\x\wsl-spawnerw.exe`, []string{"version"}); !errors.Is(err, errNoProgramInTests) {
		t.Fatalf("RunElevated: %v", err)
	}
}

func TestQuotedArgsArriveWholeOrAreRefused(t *testing.T) {
	// As a program started as an administrator reads its command line back.
	line, err := quotedArgs([]string{"hook", "install", "--report", `C:\Users\Ann Lee\AppData\Local\Temp\wsl-spawner-hook-1.txt`})
	if err != nil || line != `hook install --report "C:\Users\Ann Lee\AppData\Local\Temp\wsl-spawner-hook-1.txt"` {
		t.Fatalf("%q %v", line, err)
	}
	for _, a := range []string{`q"`, "a\rb", "a\nb"} {
		if _, err := quotedArgs([]string{a}); err == nil {
			t.Errorf("%q accepted", a)
		}
	}
}

func TestProcessStartIsWhenThisTestStarted(t *testing.T) {
	// The journal's check for an id given again: this process started a
	// moment ago, and an id no process has says nothing.
	got, ok := ProcessStart(os.Getpid())
	if !ok || got.After(time.Now()) || time.Since(got) > time.Hour {
		t.Fatalf("%v %v", got, ok)
	}
	if _, ok := ProcessStart(0x7ffffff0); ok {
		t.Fatal("an id no process has said when it started")
	}
}
