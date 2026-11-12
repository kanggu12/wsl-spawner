package inside

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// realPaths are where the scripts write in a distribution. The tests run
// them under a made-up root; one that reached this machine's own would
// leave one of these behind.
var realPaths = []string{"/var/lib/wsl-spawner", "/usr/local/lib/wsl-spawner", "/etc/wsl-spawner", "/run/wsl-spawner",
	"/etc/systemd/system/" + StartUnit, "/etc/systemd/system/" + KeepAliveUnit,
	"/run/systemd/system/docker.service.d/zz-wsl-spawner.conf", "/run/systemd/system/snap.docker.dockerd.service.d/zz-wsl-spawner.conf"}

// present lists which of realPaths are there.
func present() map[string]bool {
	out := map[string]bool{}
	for _, p := range realPaths {
		if _, err := os.Lstat(filepath.FromSlash(p)); err == nil {
			out[p] = true
		}
	}
	return out
}

// TestMain fails the run when a test left one of the real paths behind
// that was not there before: a script that wrote into this machine.
func TestMain(m *testing.M) {
	before := present()
	code := m.Run()
	for p := range present() {
		if !before[p] {
			fmt.Fprintf(os.Stderr, "a test wrote %s on this machine: a script ran without its made-up root\n", p)
			code = 1
		}
	}
	os.Exit(code)
}
