// Package api is the shape of what wsl-spawner's commands print with --json:
// the program's own API. The commands print these, and the screen reads
// them, from the program in its own process or from wsl-spawner.exe for a
// screen in a WSL terminal, so the screen does nothing a command cannot.
package api

import (
	"encoding/json"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/engine/history"
)

// Schema is the version of the JSON the commands print. Every output is an
// object that starts with it, so a later change can say so instead of
// breaking a script quietly.
const Schema = 1

// The JSON status prints: every field is there in every distribution, so a
// script reads the same keys whatever runs.
type (
	Status struct {
		Schema int      `json:"schema"`
		WSL    string   `json:"wsl_version"`
		List   []Distro `json:"distributions"`
		// StopLimit and Resume are the settings a stop goes by.
		StopLimit int    `json:"stop_limit_seconds"`
		Resume    string `json:"resume"`
	}
	Distro struct {
		Name       string `json:"name"`
		Default    bool   `json:"default"`
		Version    int    `json:"wsl"`
		Running    bool   `json:"running"`
		Kind       string `json:"kind"`
		Owner      string `json:"owner"`
		AtLogon    bool   `json:"at_logon"`
		Probe      *Probe `json:"probe"`
		ProbeError string `json:"probe_error"`
	}
	Probe struct {
		Systemd     string          `json:"systemd"`
		Interop     bool            `json:"interop"`
		KeptRunning bool            `json:"kept_running"`
		SetUp       bool            `json:"set_up"`
		LiveRestore bool            `json:"docker_live_restore"`
		Engines     map[string]bool `json:"engines"`
		// EngineStates says each engine found by a word: "up", "down",
		// "asleep" (Docker stopped by hand, its socket listening) or
		// "foreign" (a docker whose daemon is not this distribution's,
		// left alone and not among Engines).
		EngineStates map[string]string `json:"engine_states"`
		Containers   []Container       `json:"containers"`
		Hooks        []string          `json:"hooks"`
		Failed       []string          `json:"failed_units"`
		LastEnd      End               `json:"last_end"`
		Bring        []string          `json:"to_bring_back"`
		// StopUnits are the units it stops before the containers: those
		// stop-units names that run now. StopUnitsSkipped are the ones it
		// names that a stop passes by: not running, or not systemd's.
		StopUnits        []string `json:"stop_units"`
		StopUnitsSkipped []string `json:"stop_units_skipped"`
		// LastStop is wsl-spawner's last stop of it, nil when there was none.
		LastStop *LastStop `json:"last_stop"`
		// Resumed is what came back at its last start, at ResumedAt.
		Resumed   []Resumed  `json:"resumed"`
		ResumedAt *time.Time `json:"resumed_at"`
		// Complete is false for a probe cut off before its end: what it
		// holds is what it read by then, and probe_error says why.
		Complete bool `json:"complete"`
	}
	// LastStop is wsl-spawner's last stop of a distribution.
	LastStop struct {
		At     time.Time `json:"at"`
		Took   float64   `json:"took_seconds"`
		Result string    `json:"result"`
		By     string    `json:"by"`
	}
	// Resumed is one container to come back, and what became of it.
	Resumed struct {
		Engine string `json:"engine"`
		Name   string `json:"name"`
		What   string `json:"what"`
	}
	Container struct {
		Engine      string `json:"engine"`
		ID          string `json:"id"`
		Name        string `json:"name"`
		State       string `json:"state"`
		Policy      string `json:"restart_policy"`
		StopSeconds int    `json:"stop_timeout_seconds"`
		Image       string `json:"image"`
		Project     string `json:"compose_project"`
		// AutoRemove: its engine removes it as it stops (--rm).
		AutoRemove bool `json:"auto_remove"`
	}
	End struct {
		How   string     `json:"how"`
		Start *time.Time `json:"start"`
		// Cause is what ended a start that was cut, as the
		// distribution's journal and Windows' event logs tell it together:
		// "crash" (Windows did not shut down cleanly), "update" (WSL was
		// updated), "shutdown" (Windows shut down or restarted), "signout"
		// (the person signed out) or "outside" (WSL was stopped outside
		// wsl-spawner); absent when they do not tell. CauseAt is when it
		// happened.
		Cause   string     `json:"cause,omitempty"`
		CauseAt *time.Time `json:"cause_at,omitempty"`
	}
)

// StopEvent is one thing that happened, as --events prints it.
type StopEvent struct {
	Event  string  `json:"event"`
	Distro string  `json:"distro"`
	Kind   string  `json:"kind,omitempty"`
	Step   string  `json:"step,omitempty"`
	Hook   string  `json:"hook,omitempty"`
	Count  int     `json:"count,omitempty"`
	Code   int     `json:"code,omitempty"`
	Took   float64 `json:"took_seconds,omitempty"`
	Done   bool    `json:"done,omitempty"`
	Result string  `json:"result,omitempty"`
	Err    string  `json:"error,omitempty"`
}

// MarshalJSON leaves out what an event has nothing to say for, but for a
// step that is done: its exit code and its time are answers even at 0.
func (e StopEvent) MarshalJSON() ([]byte, error) {
	// The same fields without this method, so Marshal does not come back.
	type plain StopEvent
	if e.Kind != "done" {
		return json.Marshal(plain(e))
	}
	// The outer fields win over the embedded ones of the same name.
	return json.Marshal(struct {
		plain
		Code int     `json:"code"`
		Took float64 `json:"took_seconds"`
	}{plain(e), e.Code, e.Took})
}

// The JSON stop prints at its end.
type (
	StopResult struct {
		Schema   int          `json:"schema"`
		Results  []DistroStop `json:"results"`
		Shutdown bool         `json:"shutdown"`
		Err      string       `json:"error"`
		// Detached is set when the terminal it was asked from runs in a
		// distribution it stops: it goes on without it, and Results stay
		// empty; the history says how it went.
		Detached string `json:"detached_from"`
	}
	DistroStop struct {
		Name   string         `json:"name"`
		Result string         `json:"result"`
		Clean  bool           `json:"clean"`
		Took   float64        `json:"took_seconds"`
		Steps  []history.Step `json:"steps"`
		Err    string         `json:"error"`
	}
)

// The JSON start prints.
type (
	StartResult struct {
		Schema  int           `json:"schema"`
		Results []DistroStart `json:"results"`
	}
	DistroStart struct {
		Name   string     `json:"name"`
		Result string     `json:"result"`
		Err    string     `json:"error"`
		Back   []CameBack `json:"came_back"`
	}
	CameBack struct {
		Engine string `json:"engine"`
		Name   string `json:"name"`
		What   string `json:"what"`
	}
)

// SetResult is the JSON set prints: the distribution's choices after.
type SetResult struct {
	Schema  int    `json:"schema"`
	Name    string `json:"name"`
	Changed bool   `json:"changed"`
	Managed bool   `json:"managed"`
	Keep    bool   `json:"keep"`
	Logon   bool   `json:"logon"`
	Skip    bool   `json:"skip"`
}

// The JSON check prints: each finding's words in English, with its key, so
// a script can tell findings apart however the words change.
type (
	Check struct {
		Schema int          `json:"schema"`
		Worst  string       `json:"worst"`
		Groups []CheckGroup `json:"groups"`
	}
	CheckGroup struct {
		Name  string    `json:"name"`
		Key   string    `json:"key"`
		Items []Finding `json:"findings"`
	}
	Finding struct {
		Mark string `json:"mark"`
		Key  string `json:"key"`
		Text string `json:"text"`
		Do   string `json:"do"`
		// Args, DoKey and DoArgs are the values in Text and Do, and the key
		// of Do, for a front that says them in another language.
		Args   []any  `json:"args"`
		DoKey  string `json:"do_key"`
		DoArgs []any  `json:"do_args"`
	}
)

// The JSON hook status prints. Last is the hook's place among the sign-out
// scripts of scripts.ini; PowerShell are those of psscripts.ini, which
// Windows runs after them unless it is set to run them first.
// PowerShellFirst is psscripts.ini's own choice of that order: true before
// them, false after, null when it makes none and Windows' policy decides.
// Copies are the hooks of other copies of the program, among the others.
type Hook struct {
	Schema          int        `json:"schema"`
	Installed       bool       `json:"installed"`
	Last            bool       `json:"last"`
	Command         string     `json:"command"`
	Others          []string   `json:"other_scripts"`
	Copies          []HookCopy `json:"other_copies"`
	PowerShell      []string   `json:"powershell_scripts"`
	PowerShellFirst *bool      `json:"powershell_first"`
	MaxWait         int        `json:"max_wait_seconds"`
	// MaxWaitBefore is, where wsl-spawner hook install raised that wait and
	// it is still the one it set, what Windows waited before, which hook
	// remove puts back; absent otherwise.
	MaxWaitBefore *int `json:"max_wait_before_seconds,omitempty"`
}

// HookCopy is another copy's sign-out hook, as a moved install or a second
// one leaves it. Program is "gone" when its program is gone for certain,
// which every change of the hook takes out; "there" when it starts for this
// user; "unknown" when it cannot be looked at from here, as another
// account's. Before is set when it runs before this copy's hook, or this
// copy has none.
type HookCopy struct {
	Command string `json:"command"`
	Program string `json:"program"`
	Before  bool   `json:"before"`
}
