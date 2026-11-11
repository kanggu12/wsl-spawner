package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// write puts a settings file in a temporary folder.
func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsAndExample(t *testing.T) {
	// No file: the defaults.
	c, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || c.Language != "auto" || c.Resume != "running" || c.StopLimit != 300 {
		t.Fatalf("%+v %v", c, err)
	}
	// The example file loads to the same defaults, every key in it known.
	e, err := Load(write(t, Example))
	if err != nil || len(e.Skip) != 0 || len(e.Logon) != 0 || e.Mode != c.Mode || e.StopLimit != c.StopLimit || e.Resume != c.Resume {
		t.Fatalf("%+v %v", e, err)
	}
	// Every key the example sets is one SetKeys knows the place of.
	for _, line := range strings.Split(Example, "\n") {
		if m := keyLine.FindStringSubmatch(line); m != nil && m[1] == "" && !slices.Contains(keys, m[3]) {
			t.Errorf("the example sets %q, which SetKeys does not order", m[3])
		}
	}
}

func TestValues(t *testing.T) {
	c, err := Load(write(t, "language = \"ko\"\nskip = [\"Work\"]\nlogon = ['Ubuntu-24.04', \"Dev\"]\nresume = \"policy\"\nstop_limit = 900\n"))
	if err != nil || c.Language != "ko" || c.Resume != "policy" || c.StopLimit != 900 {
		t.Fatalf("%+v %v", c, err)
	}
	// Names are compared without case, as wsl.exe compares them.
	if !c.Skipped("work") || c.Skipped("Dev") || !c.AtLogon("ubuntu-24.04") || c.AtLogon("Work") {
		t.Fatalf("lookups: %+v", c)
	}
	// The order of logon is kept: it is the order they start in.
	if !slices.Equal(c.Logon, []string{"Ubuntu-24.04", "Dev"}) {
		t.Fatalf("logon order: %q", c.Logon)
	}
}

func TestRejects(t *testing.T) {
	for body, key := range map[string]string{
		`language = "fr"`:    "language",
		`theme = "blue"`:     "theme",
		`mode = "gui"`:       "mode",
		`resume = "all"`:     "resume",
		`stop_limit = 9`:     "stop_limit",
		`stop_limit = 3601`:  "stop_limit",
		`skip = [""]`:        "skip",
		`skip = ["a b"]`:     "skip",
		`logon = ["C:\\x"]`:  "logon",
		`protect = ["typo"]`: "protect",
		// A distribution left to WSL is not one to start at sign-in.
		"skip = [\"Dev\"]\nlogon = [\"dev\"]": "logon",
	} {
		_, err := Load(write(t, body))
		var ke *KeyError
		if !errors.As(err, &ke) || ke.Key != key || !strings.Contains(err.Error(), "config.toml") {
			t.Errorf("%q: %v", body, err)
		}
	}
	// A file that is no TOML at all says where, and is no KeyError.
	if _, err := Load(write(t, "language = ")); err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Fatalf("broken file: %v", err)
	}
	// A number where a name list goes is refused by the TOML reader.
	if _, err := Load(write(t, "skip = 3")); err == nil {
		t.Fatal("skip = 3 loaded")
	}
}

func TestStopLimitBoundsAreAllowed(t *testing.T) {
	for _, n := range []string{"10", "3600"} {
		if c, err := Load(write(t, "stop_limit = "+n)); err != nil || c.StopLimit == 0 {
			t.Errorf("stop_limit = %s: %+v %v", n, c, err)
		}
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{
		"Ubuntu-24.04":          true,
		"docker_desktop-data":   true,
		"a":                     true,
		"":                      false,
		"two words":             false,
		"한국어":                   false,
		strings.Repeat("a", 64): true,
		strings.Repeat("a", 65): false,
		`..\x`:                  false,
	} {
		if got := ValidName(name); got != want {
			t.Errorf("%q: %v, want %v", name, got, want)
		}
	}
}
