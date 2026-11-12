//go:build unix

package inside

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/testutil/shtest"
)

func TestAPipeWhereTheListIsExpectedDoesNotHoldTheStartScript(t *testing.T) {
	// Only a file is read: a named pipe planted as the list would block a
	// read for ever, and the service with it.
	files := proc("systemd")
	root := tree(t, files)
	state := filepath.Join(root, "var/lib/wsl-spawner")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(state, "resume"), 0o600); err != nil {
		t.Skipf("no pipes here: %v", err)
	}
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date", "sleep")
	done := make(chan error, 1)
	go func() { _, err := runAt(t, dir, root, startScript); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the start script waits on a pipe")
	}
}
