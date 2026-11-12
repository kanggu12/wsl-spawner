package inside

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/testutil/shtest"
)

// runAt runs a script under root, a made-up distribution in a temporary
// folder. These scripts write where a distribution keeps its services: a
// root that is not a temporary folder of the test's is refused before
// anything runs, so no test ever writes into this machine's own.
func runAt(t *testing.T, dir, root, script string) (string, error) {
	t.Helper()
	if root == "" || !strings.HasPrefix(root, os.TempDir()) {
		t.Fatalf("refusing to run with the root %q", root)
	}
	return shtest.Run(t, dir, "R='"+root+"'\n"+script)
}

// systemdStub is a made-up systemctl that notes every call.
func systemdStub(t *testing.T, dir string) (asked string) {
	t.Helper()
	asked = filepath.Join(dir, "systemctl.asked")
	shtest.Stub(t, dir, "systemctl", `printf '%s\n' "$*" >>'`+asked+`'`)
	return asked
}

// distro is a made-up distribution with systemd, interop on, and Windows'
// drive where WSL mounts it.
func distro(t *testing.T) string {
	files := proc("systemd")
	files["run/systemd/system/"] = ""
	files["proc/sys/fs/binfmt_misc/WSLInterop"] = "enabled\n"
	files["mnt/c/Windows/System32/waitfor.exe"] = "MZ"
	return tree(t, files)
}

// setupTools is what the set-up needs besides the shell, and wslpath.
func setupTools(t *testing.T) string {
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "rmdir", "date")
	shtest.Stub(t, dir, "wslpath", `[ "$1" = -u ] && echo /mnt/c/Windows/System32/waitfor.exe`)
	return dir
}

func TestSetUpWritesTheServicesAndStartsTheStartService(t *testing.T) {
	root, dir := distro(t), setupTools(t)
	asked := systemdStub(t, dir)
	signal := Signal("Ubuntu-24.04", "00ff")
	out, err := runAt(t, dir, root, SetupScript(true, signal))
	if e, ok := SetupError(out); err != nil || !ok || e != "" {
		t.Fatalf("%v %q\n%s", err, e, out)
	}
	// The start script, its version first, and the two services.
	if got := read(t, root, StartScriptPath); got != startScript || !strings.HasPrefix(got, StartScriptMark+StartScriptVersion+"\n") {
		t.Fatalf("start script %q", got[:min(len(got), 40)])
	}
	if got := read(t, root, StartUnitPath); got != startUnit {
		t.Fatalf("start unit %q", got)
	}
	keep := read(t, root, KeepAlivePath)
	if !strings.Contains(keep, "\nExecStart=/mnt/c/Windows/System32/waitfor.exe "+signal+"\n") {
		t.Fatalf("keep-running unit:\n%s", keep)
	}
	// The start service runs the start script for this start, as at each
	// boot: it notes this start, and brings back what the last stop noted.
	want := []string{"is-active " + KeepAliveUnit, "daemon-reload", "enable --quiet " + StartUnit, "enable --quiet --now " + KeepAliveUnit, "start --no-block " + StartUnit}
	if got := lines(t, asked); !slices.Equal(got, want) {
		t.Fatalf("systemctl asked %q", got)
	}
	// The person's folders are there for them.
	if _, err := os.Stat(filepath.Join(root, "etc/wsl-spawner/pre-stop.d")); err != nil {
		t.Fatal(err)
	}
}

func TestSetUpWithoutKeepingRunningTakesThatServiceOut(t *testing.T) {
	root, dir := distro(t), setupTools(t)
	asked := systemdStub(t, dir)
	if out, _ := runAt(t, dir, root, SetupScript(true, "x")); !strings.Contains(out, "ok=1") {
		t.Fatal(out)
	}
	if out, _ := runAt(t, dir, root, SetupScript(false, "x")); !strings.Contains(out, "ok=1") {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(root, KeepAlivePath)); !os.IsNotExist(err) {
		t.Fatal("the keep-running service stayed")
	}
	got := lines(t, asked)
	if !slices.Contains(got, "disable --quiet --now "+KeepAliveUnit) {
		t.Fatalf("systemctl asked %q", got)
	}
	// Each set-up starts the start service, which notes a start once.
	n := 0
	for _, c := range got {
		if c == "start --no-block "+StartUnit {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("systemctl asked %q", got)
	}
}

func TestSetUpStartsTheStartServiceAndDoesNotWaitForIt(t *testing.T) {
	// The start script may wait a minute for an engine still starting, and
	// what it asks the engines has no limit of its own: run inside the
	// set-up, it outlived the minute wsl.exe's call gives the set-up, which
	// set then said was WSL not answering, and no sign-in program was made.
	// The set-up starts the service, and does not wait for it.
	root, dir := distro(t), setupTools(t)
	if err := os.MkdirAll(filepath.Join(root, "var/lib/wsl-spawner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "var/lib/wsl-spawner/resume"), []byte("docker bbb db\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sysAsked := systemdStub(t, dir)
	// Docker notes each question it is asked.
	dockAsked := filepath.Join(dir, "docker.asked")
	shtest.Stub(t, dir, "docker", `printf '%s\n' "$*" >>'`+dockAsked+`'`)
	out, err := runAt(t, dir, root, SetupScript(false, "x"))
	if _, ok := SetupError(out); err != nil || !ok {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := lines(t, sysAsked); !slices.Contains(got, "start --no-block "+StartUnit) {
		t.Fatalf("systemctl asked %q", got)
	}
	if got := lines(t, dockAsked); got != nil {
		t.Fatalf("the set-up asked docker %q", got)
	}
}

func TestWhereSystemdDoesNotTakeTheStartTheSetUpRunsItWithinALimit(t *testing.T) {
	// systemd refuses the start job: the script runs in the set-up as
	// before, given 40 s, well inside the minute wsl.exe's call gives the
	// set-up, and the start is noted.
	root, dir := distro(t), setupTools(t)
	asked := filepath.Join(dir, "systemctl.asked")
	shtest.Stub(t, dir, "systemctl", `printf '%s\n' "$*" >>'`+asked+`'
case "$1" in start) echo 'Failed to start wsl-spawner.service' >&2; exit 1 ;; is-active) echo inactive; exit 3 ;; esac`)
	// A timeout that notes the limit it is given, then runs the command.
	limits := filepath.Join(dir, "limits")
	shtest.Stub(t, dir, "timeout", `printf '%s\n' "$1" >>'`+limits+`'; shift; exec "$@"`)
	out, err := runAt(t, dir, root, SetupScript(false, "x"))
	if _, ok := SetupError(out); err != nil || !ok {
		t.Fatalf("%v\n%s", err, out)
	}
	if runs := lines(t, filepath.Join(root, "var/lib/wsl-spawner/runs")); len(runs) != 1 {
		t.Fatalf("runs %q\n%s", runs, out)
	}
	// BusyBox's sh runs its own timeout, never one on the PATH.
	if got := lines(t, limits); !shIsBusyBox(t) && !slices.Equal(got, []string{"40"}) {
		t.Fatalf("timeout given %q", got)
	}
}

func TestSetUpSaysWhatIsMissingBeforeWritingAnything(t *testing.T) {
	dir := setupTools(t)
	systemdStub(t, dir)
	// No systemd.
	files := proc("init")
	root := tree(t, files)
	out, err := runAt(t, dir, root, SetupScript(false, "x"))
	if e, ok := SetupError(out); ok || e != "systemd" || err == nil {
		t.Fatalf("%q %v %v\n%s", e, ok, err, out)
	}
	// Systemd, but no Windows programs run: nothing keeps it running.
	files = proc("systemd")
	files["run/systemd/system/"] = ""
	files["proc/sys/fs/binfmt_misc/WSLInterop"] = "disabled\n"
	root = tree(t, files)
	out, _ = runAt(t, dir, root, SetupScript(true, "x"))
	if e, _ := SetupError(out); e != "interop" {
		t.Fatalf("%q\n%s", e, out)
	}
	if _, err := os.Stat(filepath.Join(root, "usr/local/lib/wsl-spawner")); !os.IsNotExist(err) {
		t.Fatal("written before the check")
	}
}

func TestRemoveTakesOutWhatWslSpawnerPutAndLeavesThePersonsOwn(t *testing.T) {
	root, dir := distro(t), setupTools(t)
	asked := systemdStub(t, dir)
	if out, _ := runAt(t, dir, root, SetupScript(true, "x")); !strings.Contains(out, "ok=1") {
		t.Fatal(out)
	}
	// A hook of the person's own.
	hook := filepath.Join(root, "etc/wsl-spawner/pre-stop.d/10-mine")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runAt(t, dir, root, RemoveScript())
	if _, ok := SetupError(out); err != nil || !ok {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, p := range []string{StartScriptPath, StartUnitPath, KeepAlivePath, "var/lib/wsl-spawner"} {
		if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
			t.Errorf("%s stayed", p)
		}
	}
	if _, err := os.Stat(hook); err != nil {
		t.Fatal("the person's hook went")
	}
	if got := lines(t, asked); !slices.Contains(got, "disable --quiet --now "+KeepAliveUnit+" "+StartUnit) {
		t.Fatalf("systemctl asked %q", got)
	}
	// With no hook left, the folders go too; and removing twice is fine.
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if out, _ := runAt(t, dir, root, RemoveScript()); !strings.Contains(out, "ok=1") {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/wsl-spawner")); !os.IsNotExist(err) {
		t.Fatal("the empty folder stayed")
	}
}

func TestTheStartScriptKeepsTheLastTwentyStarts(t *testing.T) {
	files := proc("systemd")
	var old []string
	for i := range 25 {
		old = append(old, "17000000"+string(rune('a'+i))+" b@"+string(rune('a'+i)))
	}
	files["var/lib/wsl-spawner/runs"] = strings.Join(old, "\n") + "\n"
	root := tree(t, files)
	out, err := runAt(t, shtest.Tools(t, "mkdir", "mv", "rm", "date"), root, startScript)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	runs := lines(t, filepath.Join(root, "var/lib/wsl-spawner/runs"))
	if len(runs) != 20 || runs[0] != old[6] || !strings.HasSuffix(runs[19], " 0f0e0d0c-0b0a-4908-8706-050403020100@4242") {
		t.Fatalf("%d runs: first %q, last %q", len(runs), runs[0], runs[len(runs)-1])
	}
}

func TestTheStartScriptBringsBackWhatIsNotRunning(t *testing.T) {
	files := proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "docker aaa web\ndocker bbb db\ndocker ccc gone-one\npodman ddd cache\n"
	root := tree(t, files)
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date", "sleep")
	asked := filepath.Join(dir, "docker.asked")
	// web came back by its policy; db did not, and runs once started;
	// gone-one was removed. No podman here. The daemon answers.
	shtest.Stub(t, dir, "docker", `case "$1 $4" in
'info ') ;;
'inspect aaa') echo true ;;
'inspect bbb') if [ -f '`+asked+`' ]; then echo true; else echo false; fi ;;
'inspect ccc') echo 'Error: No such object: ccc' >&2; exit 1 ;;
'start ') printf '%s\n' "$*" >>'`+asked+`' ;;
*) echo "docker: not expected: $*" >&2; exit 64 ;;
esac`)
	if out, err := runAt(t, dir, root, startScript); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := lines(t, asked); !slices.Equal(got, []string{"start bbb"}) {
		t.Fatalf("docker asked %q", got)
	}
	// What became of each, for a look; the list is done with.
	got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resumed"))
	want := []string{"docker aaa web running", "docker bbb db started", "docker ccc gone-one gone", "podman ddd cache gone"}
	if len(got) != 5 || !strings.HasPrefix(got[0], "at ") || !slices.Equal(got[1:], want) {
		t.Fatalf("resumed %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "var/lib/wsl-spawner/resume")); !os.IsNotExist(err) {
		t.Fatal("the list stayed")
	}
	// The probe reads what became of them.
	p, err := Parse("probe=1\nresumed=" + strings.Join(got, "\nresumed=") + "\nend=1\n")
	if err != nil || len(p.Resumed) != 4 || p.Resumed[1].What != "started" || p.ResumedAt.IsZero() {
		t.Fatalf("%+v %v", p.Resumed, err)
	}
}

func TestAContainerThatEndsAtOnceDidNotComeBack(t *testing.T) {
	// Its start answers, and it ends a moment later: it is said as exited.
	files := proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "docker bbb db\n"
	root := tree(t, files)
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date", "sleep")
	shtest.Stub(t, dir, "docker", `case "$1" in inspect) echo false ;; start) ;; esac`)
	if out, err := runAt(t, dir, root, startScript); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resumed"))
	if len(got) != 2 || !strings.HasPrefix(got[0], "at ") || got[1] != "docker bbb db exited" {
		t.Fatalf("resumed %q", got)
	}
}

func TestPodmanContainersStartInAScopeOfTheirOwn(t *testing.T) {
	// Started from the service as they are, systemd would end them with it.
	files := proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "podman ddd cache\ndocker bbb db\n"
	root := tree(t, files)
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date")
	asked := filepath.Join(dir, "asked")
	for _, e := range []string{"docker", "podman"} {
		shtest.Stub(t, dir, e, `case "$1" in inspect) echo false ;; start) printf '%s\n' "`+e+` $*" >>'`+asked+`' ;; esac`)
	}
	shtest.Stub(t, dir, "systemd-run", `printf '%s\n' "systemd-run $*" >>'`+asked+`'; while [ "$1" != -- ]; do shift; done; shift; exec "$@"`)
	if out, err := runAt(t, dir, root, startScript); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := []string{"systemd-run --scope --quiet --collect -- podman start ddd", "podman start ddd", "docker start bbb"}
	if got := lines(t, asked); !slices.Equal(got, want) {
		t.Fatalf("asked %q", got)
	}
}

func TestTheScriptsLookForToolsWhereADistributionKeepsThem(t *testing.T) {
	// The PATH each script sets in a distribution in place of wsl.exe's:
	// snap's programs are in /snap/bin, its Docker among them.
	for name, s := range map[string]string{"probe": ProbeScript(), "stop": stopScript, "start": startScript,
		"setup": SetupScript(true, "x"), "remove": RemoveScript()} {
		var dirs []string
		for line := range strings.SplitSeq(s, "\n") {
			// The line that sets it where no test gave a made-up root.
			if v, ok := strings.CutPrefix(line, `[ -n "$R" ] || PATH=`); ok {
				dirs = strings.Split(v, ":")
			}
		}
		for _, want := range []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin", "/snap/bin"} {
			if !slices.Contains(dirs, want) {
				t.Errorf("the %s script's PATH %q has no %s", name, dirs, want)
			}
		}
	}
}

func TestTheStartServiceComesAfterEveryEngine(t *testing.T) {
	// It starts after the engines it brings containers back with, so it
	// finds them answering: snap's Docker is a unit of its own.
	var after []string
	for line := range strings.SplitSeq(startUnit, "\n") {
		if v, ok := strings.CutPrefix(line, "After="); ok {
			after = strings.Fields(v)
		}
	}
	for _, u := range []string{"docker.service", "snap.docker.dockerd.service", "podman-restart.service"} {
		if !slices.Contains(after, u) {
			t.Errorf("After=%q has no %s", after, u)
		}
	}
}

func TestTheStartScriptWaitsForAnEngineStillStarting(t *testing.T) {
	// snap's Docker: its unit has started, and the daemon answers from the
	// third question on, as one still starting does. The container is
	// brought back, not taken for gone.
	files := proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "docker bbb db\n"
	root := tree(t, files)
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date", "sleep")
	shtest.Stub(t, dir, "systemctl", `case "$1 $2" in
'is-active snap.docker.dockerd.service') echo active ;;
*) echo inactive; exit 3 ;;
esac`)
	// Each question to the daemon is counted here.
	calls := filepath.Join(dir, "calls")
	started := filepath.Join(dir, "started")
	shtest.Stub(t, dir, "docker", `echo x >>'`+calls+`'
n=0; while read -r _; do n=$((n + 1)); done <'`+calls+`'
[ "$n" -ge 3 ] || { echo 'Cannot connect to the Docker daemon' >&2; exit 1; }
case "$1" in
info) ;;
inspect) if [ -f '`+started+`' ]; then echo true; else echo false; fi ;;
start) : >'`+started+`' ;;
*) echo "docker: not expected: $*" >&2; exit 64 ;;
esac`)
	if out, err := runAt(t, dir, root, startScript); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resumed"))
	if len(got) != 2 || got[1] != "docker bbb db started" {
		t.Fatalf("resumed %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "var/lib/wsl-spawner/resume")); !os.IsNotExist(err) {
		t.Fatal("the list stayed")
	}
}

func TestTheStartScriptKeepsWhatWaitsForAnEngineThatDoesNotAnswer(t *testing.T) {
	// Docker is there and its daemon does not answer, nor does systemd
	// start it: nothing is known of its containers, which stay to bring
	// back once it does, and it is not waited for. What has no engine at
	// all is gone.
	files := proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "docker bbb db\ndocker ccc web\npodman ddd cache\n"
	root := tree(t, files)
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date", "sleep")
	shtest.Stub(t, dir, "systemctl", `echo failed; exit 3`)
	shtest.Stub(t, dir, "docker", `echo 'Cannot connect to the Docker daemon' >&2; exit 1`)
	start := time.Now()
	out, err := runAt(t, dir, root, startScript)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// No minute's wait for a daemon systemd does not start: only the look
	// a moment later, 2 s, takes time.
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("took %s", took)
	}
	if got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resume")); !slices.Equal(got, []string{"docker bbb db", "docker ccc web"}) {
		t.Fatalf("resume %q\n%s", got, out)
	}
	got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resumed"))
	if len(got) != 2 || !strings.HasPrefix(got[0], "at ") || got[1] != "podman ddd cache gone" {
		t.Fatalf("resumed %q", got)
	}
	// Its log says why, for systemctl status.
	if !strings.Contains(out, "docker") {
		t.Fatalf("said %q", out)
	}
}

func TestTheScriptsPickNoTextApartWithTools(t *testing.T) {
	for name, s := range map[string]string{"start": startScript, "setup": SetupScript(true, "x"), "remove": RemoveScript()} {
		if tool := shtest.TextTool(s); tool != "" {
			t.Errorf("the %s script calls %s", name, tool)
		}
	}
}

func TestSignal(t *testing.T) {
	// Letters and digits only, and ending with the token: two set-ups of one
	// distribution name different signals, so the name cannot be worked out
	// from the distribution's.
	a, b := Signal("Ubuntu-24.04", "00ff"), Signal("Ubuntu-24.04", "11ee")
	if a == b || !strings.HasPrefix(a, "WslSpawnerKeepUbuntu2404") || !strings.HasSuffix(a, "00ff") || strings.ContainsAny(a, "-._") {
		t.Fatalf("%q %q", a, b)
	}
}

func TestAStopDuringTheStartKeepsTheListItWrote(t *testing.T) {
	// The start waits for Podman, which does not answer, while a stop runs
	// and writes a list of its own: that list stands, and the start's
	// leftover does not take its place.
	files := proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "docker aaa web\npodman ddd cache\n"
	root := tree(t, files)
	state := filepath.Join(root, "var/lib/wsl-spawner")
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date", "sleep", "ln")
	// Docker answers; as aaa is looked at, a stop writes its list.
	shtest.Stub(t, dir, "docker", `case "$1" in
info) ;;
inspect) printf 'docker zzz new\n' >'`+state+`/resume.new'; mv -f '`+state+`/resume.new' '`+state+`/resume'; echo true ;;
*) echo "docker: not expected: $*" >&2; exit 64 ;;
esac`)
	shtest.Stub(t, dir, "podman", `exit 1`)
	if out, err := runAt(t, dir, root, startScript); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := lines(t, filepath.Join(state, "resume")); !slices.Equal(got, []string{"docker zzz new"}) {
		t.Fatalf("resume %q", got)
	}
	// With no stop between, what waits for Podman stays on the list, as
	// before; the second name is gone.
	files = proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "docker aaa web\npodman ddd cache\n"
	root = tree(t, files)
	state = filepath.Join(root, "var/lib/wsl-spawner")
	dir = shtest.Tools(t, "mkdir", "mv", "rm", "date", "sleep", "ln")
	shtest.Stub(t, dir, "docker", `case "$1" in info) ;; inspect) echo true ;; *) exit 64 ;; esac`)
	shtest.Stub(t, dir, "podman", `exit 1`)
	if out, err := runAt(t, dir, root, startScript); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := lines(t, filepath.Join(state, "resume")); !slices.Equal(got, []string{"podman ddd cache"}) {
		t.Fatalf("resume %q", got)
	}
	if _, err := os.Stat(filepath.Join(state, "resume.taken")); !os.IsNotExist(err) {
		t.Fatalf("the second name stayed: %v", err)
	}
}

func TestTwoStartScriptsAtOnceBringEachContainerBackOnce(t *testing.T) {
	// The service at a boot a set-up caused, and a run by hand or the
	// set-up's own: both read the list, and each container was started
	// twice and listed twice. One run at a time: the second leaves it to
	// the first. Docker answers slowly, so the two would meet.
	for way, tools := range lockWays(t) {
		files := proc("systemd")
		files["var/lib/wsl-spawner/resume"] = "docker aa11 web\ndocker bb22 db\n"
		root := tree(t, files)
		dir := shtest.Tools(t, append(tools, "rm", "date", "ln")...)
		// Each start the engine is asked for is noted.
		asked := filepath.Join(dir, "asked")
		shtest.Stub(t, dir, "docker", `case "$1" in
info) sleep 1 ;;
inspect) echo false ;;
start) printf '%s\n' "$2" >>'`+asked+`' ;;
esac`)
		// Two at once, each in its own goroutine; what each printed comes
		// back.
		outs := make(chan string, 2)
		for range 2 {
			go func() {
				out, _ := shtest.Run(t, dir, "R='"+root+"'\n"+startScript)
				outs <- out
			}()
		}
		out := <-outs + <-outs
		if got := lines(t, asked); !slices.Equal(got, []string{"aa11", "bb22"}) {
			t.Errorf("%s: docker started %q\n%s", way, got, out)
		}
		got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resumed"))
		if len(got) != 3 || got[1] != "docker aa11 web exited" || got[2] != "docker bb22 db exited" {
			t.Errorf("%s: resumed %q\n%s", way, got, out)
		}
		// The start is noted once.
		if runs := lines(t, filepath.Join(root, "var/lib/wsl-spawner/runs")); len(runs) != 1 {
			t.Errorf("%s: runs %q", way, runs)
		}
	}
}

func TestAContainerAStartBringsBackHoldsNoStartLock(t *testing.T) {
	// Podman's start leaves a program of its own watching the container,
	// as long as the container runs: it is not handed the lock, or no
	// start would run again until the container ends.
	for way, tools := range lockWays(t) {
		files := proc("systemd")
		files["var/lib/wsl-spawner/resume"] = "podman dd44 cache\n"
		root := tree(t, files)
		dir := shtest.Tools(t, append(tools, "rm", "date")...)
		// Its start leaves a program running, its output closed so the
		// test does not wait for it; it ends by itself a moment later.
		shtest.Stub(t, dir, "podman", `case "$1" in info) ;; inspect) echo false ;; start) sleep 3 >/dev/null 2>&1 & ;; esac`)
		if out, err := runAt(t, dir, root, startScript); err != nil {
			t.Fatalf("%s: %v\n%s", way, err, out)
		}
		// A start right after runs: it notes its look at what came back.
		if err := os.WriteFile(filepath.Join(root, "var/lib/wsl-spawner/resume"), []byte("podman dd44 cache\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, "var/lib/wsl-spawner/resumed")); err != nil {
			t.Fatal(err)
		}
		out, err := runAt(t, dir, root, startScript)
		if got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resumed")); err != nil || len(got) != 2 {
			t.Errorf("%s: resumed %q %v\n%s", way, got, err, out)
		}
	}
}

func TestTheStartScriptRunsFromTheRoot(t *testing.T) {
	// Run by hand or by the set-up through wsl.exe, it would start in
	// Windows' own folder: the engines it runs start from the root, here
	// the made-up one; the test runs from this package's folder.
	files := proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "docker aaa web\n"
	root := tree(t, files)
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date")
	// The engine notes the folder it was started in as it starts the
	// container.
	where := filepath.Join(dir, "where")
	shtest.Stub(t, dir, "docker", `case "$1" in info) ;; inspect) echo false ;; start) pwd -P >>'`+where+`' ;; esac`)
	if out, err := runAt(t, dir, root, startScript); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// The made-up root as the system names it, links followed.
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := lines(t, where); !slices.Equal(got, []string{want}) {
		t.Fatalf("docker started in %q, not %q", got, want)
	}
}

func TestTheStartScriptBringsBackWhatItCanWhereANoteCannotBeMade(t *testing.T) {
	// A note of its own that cannot be made (a file system gone read-only;
	// here a folder stands in its place) does not end the script before it
	// starts anything, as the shell ends a script whose : cannot redirect.
	files := proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "docker bbb db\n"
	files["var/lib/wsl-spawner/resume.left/"] = ""
	root := tree(t, files)
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date")
	// What the engine is asked to start is noted here.
	asked := filepath.Join(dir, "asked")
	shtest.Stub(t, dir, "docker", `case "$1" in info) ;; inspect) echo false ;; start) printf '%s\n' "$*" >>'`+asked+`' ;; esac`)
	out, _ := runAt(t, dir, root, startScript)
	if got := lines(t, asked); !slices.Equal(got, []string{"start bbb"}) {
		t.Fatalf("docker asked %q\n%s", got, out)
	}
}

func TestTheStartScriptRunsOnlyTheTwoEngines(t *testing.T) {
	// A line of the list naming anything else is not run: each first word
	// would run as root.
	files := proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "evil xxx yyy\ndocker aaa web\n"
	root := tree(t, files)
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date", "sleep")
	ran := filepath.Join(dir, "evil.ran")
	shtest.Stub(t, dir, "evil", `: >'`+ran+`'`)
	shtest.Stub(t, dir, "docker", `case "$1" in info) ;; inspect) echo true ;; *) exit 64 ;; esac`)
	if out, err := runAt(t, dir, root, startScript); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Fatal("the line's first word ran")
	}
	got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resumed"))
	if len(got) != 2 || got[1] != "docker aaa web running" {
		t.Fatalf("resumed %q", got)
	}
}

func TestTheStartScriptHandsAnEngineOnlyIds(t *testing.T) {
	// The list is a file of root's: still, what it names as an id goes to the
	// engine as an argument, and `--latest` or `-f` would be an option. Only
	// an id, which is hex, is handed over.
	files := proc("systemd")
	files["var/lib/wsl-spawner/resume"] = "podman --latest x\ndocker -f y\ndocker ABC upper\ndocker aaa web\n"
	root := tree(t, files)
	dir := shtest.Tools(t, "mkdir", "mv", "rm", "date", "sleep")
	asked := filepath.Join(dir, "asked")
	shtest.Stub(t, dir, "docker", `echo "docker $*" >>'`+asked+`'
case "$1" in info) ;; inspect) echo true ;; *) exit 64 ;; esac`)
	shtest.Stub(t, dir, "podman", `echo "podman $*" >>'`+asked+`'; exit 0`)
	if out, err := runAt(t, dir, root, startScript); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := strings.Join(lines(t, asked), "\n")
	if strings.Contains(got, "--latest") || strings.Contains(got, " -f") || strings.Contains(got, "ABC") || strings.Contains(got, "podman") {
		t.Fatalf("an engine was handed what is not an id:\n%s", got)
	}
	if !strings.Contains(got, "docker inspect --format {{.State.Running}} aaa") {
		t.Fatalf("the id was not asked about:\n%s", got)
	}
}

func TestASetUpRestartsAKeepRunningServiceThatRunsForItsNewSignal(t *testing.T) {
	// The service that runs holds the signal of the file it had: set up
	// again, with a new signal in the file, it is started again, so the
	// name that is waited for is the new one. One that did not run is only
	// started, once.
	for _, tc := range []struct {
		name, active string
		restarts     bool
	}{
		{"running", "active", true},
		{"not running", "inactive", false},
		// A first set-up: the service is not there at all.
		{"not there", "not-found", false},
	} {
		root, dir := distro(t), setupTools(t)
		asked := filepath.Join(dir, "systemctl.asked")
		// As the real one: 3 for a unit that does not run, 4 for one that is
		// not there yet (a first set-up), 0 for one that runs.
		code := map[string]string{"active": "0", "inactive": "3", "not-found": "4"}[tc.active]
		shtest.Stub(t, dir, "systemctl", `case "$1" in is-active) echo `+tc.active+`; exit `+code+` ;; esac
printf '%s\n' "$*" >>'`+asked+`'`)
		if out, err := runAt(t, dir, root, SetupScript(true, Signal("Ubuntu-24.04", "00ff"))); err != nil || !strings.Contains(out, "ok=1") {
			t.Fatalf("%s: %v\n%s", tc.name, err, out)
		}
		got := lines(t, asked)
		if slices.Contains(got, "restart "+KeepAliveUnit) != tc.restarts {
			t.Errorf("%s: systemctl asked %q", tc.name, got)
		}
	}
}
