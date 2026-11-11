package cli

import (
	"os"
	"strings"
	"testing"
)

func TestSetupFlagsSayAWrongValueByTheFlag(t *testing.T) {
	a, _ := pc(t)
	// A value out of range is the flag's mistake: said by the flag, with
	// no file named, as none was written.
	_, errOut, code := cli(t, a, false, "setup", "--stop-limit", "5")
	if code != exitUsage || !strings.Contains(errOut, "--stop-limit must be from 10 to 3600 seconds") || strings.Contains(errOut, "config.toml") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if _, err := os.Stat(a.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("a file was written: %v", err)
	}
	// A word no setting takes, the same way.
	if _, errOut, code := cli(t, a, false, "setup", "--resume", "always"); code != exitUsage || !strings.Contains(errOut, "--resume must be running, policy or off") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	// A right value is written, and where is said.
	out, errOut, code := cli(t, a, false, "setup", "--stop-limit", "600")
	if code != exitOK || !strings.Contains(out, "saved: "+a.ConfigPath) {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if b, err := os.ReadFile(a.ConfigPath); err != nil || !strings.Contains(string(b), "stop_limit = 600") {
		t.Fatalf("%v\n%s", err, b)
	}
}
