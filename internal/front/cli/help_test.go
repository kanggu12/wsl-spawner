package cli

import (
	"strings"
	"testing"
)

func TestEveryHelpFitsAnEightyColumnConsole(t *testing.T) {
	// A Windows console is 80 columns wide unless made wider: every line of
	// every help, the commands' list and the flags included, fits, wrapped
	// under its words where it is long.
	a, _ := pc(t)
	for _, args := range [][]string{{}, {"status"}, {"stop"}, {"start"}, {"set"}, {"check"}, {"history"}, {"hook"},
		{"hook", "status"}, {"hook", "install"}, {"hook", "remove"}, {"setup"}, {"uninstall"}, {"version"}, {"help"}} {
		out, errOut, code := cli(t, a, false, append(args, "--help")...)
		if code != exitOK {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
		for _, l := range strings.Split(out, "\n") {
			if n := len([]rune(l)); n > 80 {
				t.Errorf("%v: %d wide: %q", args, n, l)
			}
		}
	}
}
