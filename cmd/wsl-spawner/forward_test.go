//go:build !windows

package main

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestWSLENV(t *testing.T) {
	// Windows Terminal sets WSLENV already; the entry is added once.
	if got := appendWSLENV("WT_SESSION:WT_PROFILE_ID:", "WSL_DISTRO_NAME/w"); got != "WT_SESSION:WT_PROFILE_ID::WSL_DISTRO_NAME/w" {
		t.Fatalf("%q", got)
	}
	if got := appendWSLENV("WSL_DISTRO_NAME/w", "WSL_DISTRO_NAME/w"); got != "WSL_DISTRO_NAME/w" {
		t.Fatalf("%q", got)
	}
	// The environment holds one WSLENV, the new one.
	env := withWSLENV([]string{"A=1", "WSLENV=old", "B=2"}, "new")
	n := 0
	for _, e := range env {
		if len(e) > 7 && e[:7] == "WSLENV=" {
			n++
		}
	}
	if n != 1 || !slices.Contains(env, "WSLENV=new") || !slices.Contains(env, "B=2") {
		t.Fatalf("%v", env)
	}
}

func TestHelpAndVersionNeedNoWindows(t *testing.T) {
	// Without wsl-spawner.exe these still answer; anything else needs it.
	for args, want := range map[string]bool{"version": true, "help": true, "--help": true, "stop -h": true,
		"status": false, "": false, "stop Ubuntu": false} {
		var a []string
		if args != "" {
			a = strings.Fields(args)
		}
		if got := answersAlone(a); got != want {
			t.Errorf("%q: %v", args, got)
		}
	}
}

// fakeExe writes the smallest header that reads as a Windows program for
// machine: "MZ", the PE header's place at 0x3c, "PE\0\0" there, then the
// machine type. Like every fake program here, it has no execute bit: a
// mistake could not start it.
func fakeExe(t *testing.T, dir, name string, machine uint16) string {
	t.Helper()
	p := filepath.Join(dir, name)
	b := make([]byte, 0x50)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[0x3c:], 0x40)
	copy(b[0x40:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(b[0x44:], machine)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// onHost makes the header check see a PC of machine type host, whose
// Windows says build (ok false: it could not be read); asked counts how
// often it was asked.
func onHost(t *testing.T, host uint16, build int, ok bool) (asked *int) {
	t.Helper()
	h, w := hostMachine, windowsBuild
	t.Cleanup(func() { hostMachine, windowsBuild = h, w })
	asked = new(int)
	hostMachine = func() uint16 { return host }
	windowsBuild = func() (int, bool) { *asked++; return build, ok }
	return asked
}

func TestAProgramForAnotherProcessorIsNeverStarted(t *testing.T) {
	// Windows would stop it with a dialog on the desktop: the launcher reads
	// the header first and says why in the terminal.
	dir := t.TempDir()
	arm, x64, x86 := fakeExe(t, dir, "arm.exe", machineARM64), fakeExe(t, dir, "x64.exe", machineAMD64), fakeExe(t, dir, "x86.exe", machineI386)
	if m, ok := peMachine(arm); !ok || m != machineARM64 {
		t.Fatalf("read %#x %v", m, ok)
	}
	// An x64 PC runs x64 and 32-bit x86 programs, never ARM64 ones; nothing
	// asks Windows which it is.
	asked := onHost(t, machineAMD64, 0, false)
	if why := wrongBuild(arm); !strings.Contains(why, "built for ARM64 Windows; this PC runs x64 Windows") {
		t.Fatalf("%q", why)
	}
	if wrongBuild(x64) != "" || wrongBuild(x86) != "" || *asked != 0 {
		t.Fatalf("x64 or x86 refused, or Windows asked %d times", *asked)
	}
	// An ARM64 PC runs its own and x86 programs without asking; x64 ones
	// from Windows 11 on ARM, which only its build tells apart.
	asked = onHost(t, machineARM64, 19045, true)
	if wrongBuild(arm) != "" || wrongBuild(x86) != "" || *asked != 0 {
		t.Fatalf("ARM64 or x86 refused, or Windows asked %d times", *asked)
	}
	if why := wrongBuild(x64); !strings.Contains(why, "built for x64 Windows; this PC runs Windows 10 on ARM") || *asked != 1 {
		t.Fatalf("%q, asked %d", why, *asked)
	}
	onHost(t, machineARM64, 22631, true)
	if why := wrongBuild(x64); why != "" {
		t.Fatalf("Windows 11 on ARM: %q", why)
	}
	// A Windows that does not say is let through: Windows answers then.
	onHost(t, machineARM64, 0, false)
	if why := wrongBuild(x64); why != "" {
		t.Fatalf("unknown build: %q", why)
	}
	// 32-bit Arm ran on ARM64 Windows until a Windows 11 build stopped it.
	arm32 := fakeExe(t, dir, "arm32.exe", machineARMNT)
	onHost(t, machineARM64, 26100, true)
	if why := wrongBuild(arm32); !strings.Contains(why, "built for 32-bit Arm Windows; this PC runs a Windows on ARM that no longer runs 32-bit Arm programs") {
		t.Fatalf("%q", why)
	}
	onHost(t, machineARM64, 19045, true)
	if why := wrongBuild(arm32); why != "" {
		t.Fatalf("Windows 10 on ARM: %q", why)
	}
	// What runs where, as Windows has it.
	at := func(b int) func() (int, bool) { return func() (int, bool) { return b, true } }
	for _, c := range []struct {
		program, host uint16
		build         int
		runs          bool
	}{
		{machineAMD64, machineAMD64, 0, true}, {machineI386, machineAMD64, 0, true}, {machineARM64, machineAMD64, 0, false},
		{machineARM64, machineARM64, 0, true}, {machineI386, machineARM64, 0, true}, {0x0200, machineARM64, 0, false},
		{machineARMNT, machineARM64, 22631, true}, {machineARMNT, machineARM64, firstWithoutARM32, false},
		{machineAMD64, machineARM64, firstX64Emulation - 1, false}, {machineAMD64, machineARM64, firstX64Emulation, true},
		{machineARM64, 0, 0, true},
	} {
		if got := runsOn(c.program, c.host, at(c.build)); got != c.runs {
			t.Errorf("%#x on %#x, build %d: %v", c.program, c.host, c.build, got)
		}
	}
	// A file that is no Windows program is let through: the header does not
	// say, and Windows itself answers.
	text := filepath.Join(dir, "text.exe")
	if err := os.WriteFile(text, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if why := wrongBuild(text); why != "" {
		t.Fatalf("%q", why)
	}
}

func TestTheWindowsBuildIsReadFromVer(t *testing.T) {
	// ver's line as cmd.exe prints it, the same in Korean Windows: "Version"
	// and a blank line before it, the build third.
	for out, want := range map[string]int{
		"\r\nMicrosoft Windows [Version 10.0.22631.4317]\r\n": 22631,
		"\r\nMicrosoft Windows [Version 10.0.19045.3803]\r\n": 19045,
	} {
		if got, ok := parseVer(out); !ok || got != want {
			t.Errorf("%q: %d %v", out, got, ok)
		}
	}
	if _, ok := parseVer("'cmd.exe' is not recognized"); ok {
		t.Fatal("read a build from nothing")
	}
	// This PC's own processor is one the check knows.
	if m := unameMachine(); m != machineAMD64 && m != machineARM64 && m != 0 {
		t.Fatalf("%#x", m)
	}
}

// launchedEnv is the environment launch would have started the .exe with.
var launchedEnv []string

// stopped is how a test's exit ends forward: with the code it was given.
type stopped int

// launch runs forward on args with the .exe at exe, with nothing reaching
// the PC: it returns the exit code (-1 when forward returned) and what
// would have started.
func launch(t *testing.T, exe string, terminal bool, args ...string) (code int, started []string) {
	t.Helper()
	t.Setenv("WSL_DISTRO_NAME", "Test")
	// What the screen drawn here is told, kept to this test.
	t.Setenv("WSL_SPAWNER_EXE", "")
	t.Setenv("WSLENV", os.Getenv("WSLENV"))
	saved := []any{os.Args, locate, execve, exit, finish, terminals}
	t.Cleanup(func() {
		os.Args = saved[0].([]string)
		locate, execve, exit = saved[1].(func() string), saved[2].(func(string, []string, []string) error), saved[3].(func(int))
		finish, terminals = saved[4].(func()), saved[5].(func() string)
	})
	os.Args = append([]string{"wsl-spawner"}, args...)
	locate = func() string { return exe }
	terminals = func() string {
		if terminal {
			return "012"
		}
		return ""
	}
	execve = func(argv0 string, _ []string, env []string) error {
		launchedEnv = env
		started = append(started, "exec "+argv0)
		return errors.New("not started in a test")
	}
	exit = func(c int) { panic(stopped(c)) }
	defer func() {
		if r := recover(); r != nil {
			c, ok := r.(stopped)
			if !ok {
				panic(r)
			}
			code = int(c)
		}
	}()
	forward()
	return -1, started
}

func TestTheLauncherStartsNothingBeforeItReadTheHeader(t *testing.T) {
	// The order that keeps the dialog away: the header is read before
	// anything can start the .exe in this process's place.
	dir := t.TempDir()
	// An x64 PC with an ARM64 build beside it, and the right one.
	onHost(t, machineAMD64, 0, false)
	wrong, right := fakeExe(t, dir, "arm.exe", machineARM64), fakeExe(t, dir, "x64.exe", machineAMD64)
	for _, terminal := range []bool{false, true} {
		for _, args := range [][]string{nil, {"status"}, {"stop", "Ubuntu"}} {
			code, started := launch(t, wrong, terminal, args...)
			if code != 1 || len(started) != 0 {
				t.Errorf("%v in a terminal %v: exit %d, started %v", args, terminal, code, started)
			}
		}
	}
	// The version is answered here, the .exe untouched.
	if code, started := launch(t, wrong, false, "version"); code != -1 || len(started) != 0 {
		t.Fatalf("version: exit %d, started %v", code, started)
	}
	// The right build goes on: the screen is drawn here, in a terminal,
	// and asks it each command; anything else starts it in this process's
	// place.
	if code, started := launch(t, right, true); code != -1 || len(started) != 0 || os.Getenv("WSL_SPAWNER_EXE") != right {
		t.Fatalf("exit %d, started %v, exe %q", code, started, os.Getenv("WSL_SPAWNER_EXE"))
	}
	if _, started := launch(t, right, false, "status"); strings.Join(started, " ") != "exec "+right {
		t.Fatalf("started %v", started)
	}
}

func TestAWindowsProgramThatCannotStartSaysWhy(t *testing.T) {
	// With WSL's interop off, starting the .exe fails with "exec format
	// error": the hint says how to turn it on. Other errors have none.
	err := &os.PathError{Op: "fork/exec", Path: "/mnt/c/x/wsl-spawner.exe", Err: syscall.ENOEXEC}
	if h := startHint(err); !strings.Contains(h, "[interop]") || !strings.Contains(h, "stop and start the distribution") {
		t.Fatalf("%q", h)
	}
	if h := startHint(&os.PathError{Op: "fork/exec", Path: "x", Err: syscall.EACCES}); h != "" {
		t.Fatalf("%q", h)
	}
}

func TestATerminalIsRelayedToTheExe(t *testing.T) {
	// Started from a terminal, the .exe is told which of its ends a person
	// is at, and the width; from a pipe, nothing.
	onHost(t, machineAMD64, 0, false)
	exe := fakeExe(t, t.TempDir(), "x64.exe", machineAMD64)
	for _, terminal := range []bool{true, false} {
		launchedEnv = nil
		launch(t, exe, terminal, "status")
		joined := strings.Join(launchedEnv, "\n")
		has := strings.Contains(joined, "WSL_SPAWNER_TTY=012") && strings.Contains(joined, "WSL_SPAWNER_TTY/w") && strings.Contains(joined, "WSL_SPAWNER_COLUMNS=")
		if has != terminal {
			t.Errorf("terminal %v: relayed %v in %q", terminal, has, joined)
		}
	}
	// One end alone: a script's output to a file, typed at a terminal.
	env := relayEnv([]string{"WSLENV=A/w"}, "A/w", "02", 120)
	if !slices.Contains(env, "WSL_SPAWNER_TTY=02") || !slices.Contains(env, "WSL_SPAWNER_COLUMNS=120") || !slices.Contains(env, "WSLENV=A/w:WSL_SPAWNER_TTY/w:WSL_SPAWNER_COLUMNS/w") {
		t.Fatalf("%q", env)
	}
	if got := relayEnv([]string{"WSLENV=A/w"}, "A/w", "", 120); !slices.Equal(got, []string{"WSLENV=A/w"}) {
		t.Fatalf("%q", got)
	}
}

func TestATestStartsNoProgram(t *testing.T) {
	// The .exe as the launcher starts it, left as it is: in a test it is
	// refused before anything runs, so a stand-in never reaches Windows,
	// which would show its dialog on the person's screen. The program here
	// only leaves a mark, should it ever run.
	dir := t.TempDir()
	mark := filepath.Join(dir, "ran")
	prog := filepath.Join(dir, "prog")
	if err := os.WriteFile(prog, []byte("#!/bin/sh\ntouch '"+mark+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := execve(prog, []string{prog}, nil); !errors.Is(err, errNoProgramInTests) {
		t.Errorf("execve: %v", err)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("the program ran")
	}
}

func TestWhatOpensTheScreenHere(t *testing.T) {
	for args, want := range map[string]bool{
		"": true, "tui": true, "--lang ko": true, "--theme light tui": true, "--lang=ko": true,
		"status": false, "stop Ubuntu": false, "--lang ko status": false, "--help": false, "tui --help": false,
		// The wizard is drawn here too; setup with a flag writes a setting,
		// which the Windows program does.
		"setup": true, "--lang ko setup": true, "setup --mode cli": false, "setup --stop-limit 600": false, "setup --help": false,
	} {
		if got := screenAsked(strings.Fields(args)); got != want {
			t.Errorf("%q: %v", args, got)
		}
	}
}
