//go:build windows

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestASettingIsWrittenWhileAnotherProgramReadsTheFile(t *testing.T) {
	// The screen reads the settings every few seconds: a change made then
	// waits for the reader to let go, as Windows keeps an open file from
	// being replaced, instead of failing.
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("mode = \"tui\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- SetKeys(p, map[string]string{"mode": `"cli"`}) }()
	// The reader lets go a moment later.
	time.Sleep(300 * time.Millisecond)
	r.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("still waiting")
	}
	if b, err := os.ReadFile(p); err != nil || !strings.Contains(string(b), `mode = "cli"`) {
		t.Fatalf("%v\n%s", err, b)
	}
}
