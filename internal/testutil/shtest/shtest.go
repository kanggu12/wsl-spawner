// Package shtest runs the scripts this program sends into a distribution
// under the sh of the machine a test runs on, with only named tools on the
// PATH: a distribution with nothing else installed. A script that grows to
// need one more tool fails there, as it would in such a distribution.
package shtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// Tools is a new PATH folder holding just the named tools, linked from
// where this machine has them. The test is skipped where one is missing,
// and on anything but Linux: the scripts read /proc and run Linux tools.
func Tools(t testing.TB, tools ...string) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the scripts are for Linux")
	}
	dir := t.TempDir()
	for _, tool := range tools {
		at, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s here", tool)
		}
		if err := os.Symlink(at, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// sh is this machine's sh, found before any PATH is cut down.
func sh(t testing.TB) string {
	t.Helper()
	at, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	return at
}

// Run runs script with sh, as wsl.exe does, with only the tools in dir on
// its PATH. It returns what the script printed, errors included.
func Run(t testing.TB, dir, script string) (string, error) {
	t.Helper()
	cmd := exec.Command(sh(t), "-c", script)
	// Nothing of the test's own environment reaches the script.
	cmd.Env = []string{"PATH=" + dir}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// textTool finds a call of a tool that picks text apart, at the start of a
// command (not inside an option such as --no-trunc).
var textTool = regexp.MustCompile(`(^|[^\w.-])(awk|sed|grep|xargs|wc|tr|head|tail|sort|uniq)\s`)

// TextTool names the first such tool a script calls, or nothing. The shell
// does that work in these scripts: a distribution made from a small image
// may have none of them, and the tests that run a script without them see
// only the branches they take.
func TextTool(script string) string {
	if m := textTool.FindStringSubmatch(script); m != nil {
		return m[2]
	}
	return ""
}

// Stub puts a made-up tool called name into dir, a folder Tools made: body
// is what it runs, with nothing but the shell.
func Stub(t testing.TB, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!"+sh(t)+"\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
