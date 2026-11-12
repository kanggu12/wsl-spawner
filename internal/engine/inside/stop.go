package inside

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/core/plain"
)

//go:embed stop.sh
var stopScript string

// StopOptions is what the stop script is told.
type StopOptions struct {
	// Limit is how long every step together may take.
	Limit time.Duration
	// Resume is which running containers come back: "running", "policy" or
	// "off", as the settings say.
	Resume string
	// By is who asked: "hand" or "signout".
	By string
	// Now is the time on Windows as the stop starts, for its record.
	Now time.Time
}

// StopScript is the stop script with its options in front of it. The
// values are the program's own, never a person's text: each is checked to
// be a plain word or number before it goes into the script.
func StopScript(o StopOptions) (string, error) {
	secs := int(o.Limit / time.Second)
	if secs < 1 {
		return "", fmt.Errorf("a stop needs a limit of a second or more, not %s", o.Limit)
	}
	for _, w := range []string{o.Resume, o.By} {
		if !plainWord(w) {
			return "", fmt.Errorf("%q cannot go into the stop script", w)
		}
	}
	return fmt.Sprintf("LIMIT=%d\nRESUME=%s\nBY=%s\nNOW=%d\n", secs, o.Resume, o.By, o.Now.Unix()) + stopScript, nil
}

// plainWord reports whether s is lower-case letters only: a value the
// script takes as it is.
func plainWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// The steps the stop script runs, by the names its lines give them.
const (
	StepHook   = "hook"
	StepUnits  = "units"
	StepDocker = "docker"
	StepPodman = "podman"
	StepSync   = "sync"
	// StepRecord is a file of the stop's own that could not be written:
	// the list of what comes back, or the record of the stop. It is said
	// only then, as a step that failed.
	StepRecord = "record"
)

// StopLine is one line the stop script printed.
type StopLine struct {
	// Kind is "plan" (a step to come), "start", "done" (a step under way or
	// ended) or "end" (the whole stop).
	Kind string
	// Step is the step's name, as the constants above; Hook the hook's file
	// name when Step is StepHook.
	Step, Hook string
	// Count is how many containers or units a plan or start line names.
	Count int
	// Code is a done step's exit code, Took how long it ran.
	Code int
	Took time.Duration
	// Result is the end's: "clean", "limit" or "failed".
	Result string
}

// ParseStopLine reads one line of the stop script; ok is false for a line
// that is not one of its own.
func ParseStopLine(line string) (StopLine, bool) {
	key, value, ok := strings.Cut(strings.TrimRight(line, "\r"), "=")
	if !ok {
		return StopLine{}, false
	}
	// What a distribution says is shown: a hook's name or an end it makes
	// up may hold what a terminal obeys. The tabs are the fields': made safe
	// after they are split.
	f := strings.Split(value, "\t")
	for i := range f {
		f[i] = plain.Text(f[i])
	}
	value = plain.Text(value)
	num := func(i int) int {
		if i >= len(f) {
			return 0
		}
		n, _ := strconv.Atoi(f[i])
		return n
	}
	switch key {
	case "plan":
		// plan=STEP<tab>COUNT, or plan=hook<tab>NAME.
		if f[0] == StepHook && len(f) == 2 {
			return StopLine{Kind: "plan", Step: StepHook, Hook: f[1]}, true
		}
		return StopLine{Kind: "plan", Step: f[0], Count: num(1)}, f[0] != ""
	case "step", "hook":
		// step=NAME<tab>start[<tab>COUNT], step=NAME<tab>done<tab>CODE<tab>SECONDS;
		// hook=FILE in the same shape.
		if len(f) < 2 || (f[1] != "start" && f[1] != "done") {
			return StopLine{}, false
		}
		l := StopLine{Kind: f[1], Step: f[0]}
		if key == "hook" {
			l.Step, l.Hook = StepHook, f[0]
		}
		if l.Kind == "start" {
			l.Count = num(2)
		} else {
			l.Code, l.Took = num(2), time.Duration(num(3))*time.Second
		}
		return l, true
	case "end":
		return StopLine{Kind: "end", Result: value}, value != ""
	}
	return StopLine{}, false
}
