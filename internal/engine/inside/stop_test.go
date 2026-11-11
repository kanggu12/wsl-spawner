package inside

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kanggu12/wsl-spawner/internal/testutil/shtest"
)

// stopIn runs the stop script under root with the tools in dir, and
// returns its lines read, and what it printed.
func stopIn(t *testing.T, dir, root string, o StopOptions) ([]StopLine, string) {
	t.Helper()
	if o.Limit == 0 {
		o.Limit = 300 * time.Second
	}
	if o.Resume == "" {
		o.Resume = "running"
	}
	if o.By == "" {
		o.By = "hand"
	}
	if o.Now.IsZero() {
		o.Now = time.Unix(1700000000, 0)
	}
	script, err := StopScript(o)
	if err != nil {
		t.Fatal(err)
	}
	out, err := shtest.Run(t, dir, "R='"+root+"'\n"+script)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var ls []StopLine
	for _, line := range strings.Split(out, "\n") {
		if l, ok := ParseStopLine(line); ok {
			ls = append(ls, l)
		} else if strings.TrimSpace(line) != "" {
			// Every line it prints is one of its own: a tool's words
			// leaking into the output would be read as garbage.
			t.Errorf("not a line of the stop script: %q", line)
		}
	}
	return ls, out
}

// end is the result the lines end with, "" for none.
func end(ls []StopLine) string {
	if n := len(ls); n > 0 && ls[n-1].Kind == "end" {
		return ls[n-1].Result
	}
	return ""
}

// kinds is the lines as KIND:STEP words, for comparing their order.
func kinds(ls []StopLine) []string {
	var out []string
	for _, l := range ls {
		w := l.Kind + ":" + l.Step
		if l.Hook != "" {
			w += ":" + l.Hook
		}
		if l.Kind == "end" {
			w = "end:" + l.Result
		}
		out = append(out, w)
	}
	return out
}

// read is a file's text under root, "" when it is not there.
func read(t *testing.T, root, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, p))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func TestStopWithNothingToStopSyncsAndWritesNothing(t *testing.T) {
	// No systemd, no engine, not set up: the stop only syncs, writes no
	// file, and ends clean.
	root := tree(t, proc("init"))
	ls, out := stopIn(t, shtest.Tools(t), root, StopOptions{})
	if want := []string{"plan:sync", "start:sync", "done:sync", "end:clean"}; !slices.Equal(kinds(ls), want) {
		t.Fatalf("%q\n%s", kinds(ls), out)
	}
	if _, err := os.Stat(filepath.Join(root, "var/lib/wsl-spawner")); !os.IsNotExist(err) {
		t.Fatal("a folder was made where wsl-spawner is not set up")
	}
}

func TestStopPicksNoTextApartWithTools(t *testing.T) {
	if tool := shtest.TextTool(stopScript); tool != "" {
		t.Fatalf("the stop script calls %s: a small distribution may have none", tool)
	}
}

func TestStopRecordsItselfWhereItIsSetUp(t *testing.T) {
	files := proc("systemd")
	files["var/lib/wsl-spawner/"] = ""
	root := tree(t, files)
	ls, _ := stopIn(t, shtest.Tools(t, "mv"), root, StopOptions{By: "signout"})
	if end(ls) != "clean" {
		t.Fatalf("%q", kinds(ls))
	}
	// The record names this start, as the probe names it, so the next
	// start can tell this stop was the last one.
	want := "run=0f0e0d0c-0b0a-4908-8706-050403020100@4242\nat=1700000000\ntook=0\nresult=clean\nby=signout\n"
	if got := read(t, root, "var/lib/wsl-spawner/last-stop"); got != want {
		t.Fatalf("record %q", got)
	}
	// What the probe reads back from it.
	p := Probe{BootID: "0f0e0d0c-0b0a-4908-8706-050403020100", InitStart: "9999", StartScript: StartScriptMark + "1",
		Runs: []Run{{ID: "0f0e0d0c-0b0a-4908-8706-050403020100@4242"}, {ID: "0f0e0d0c-0b0a-4908-8706-050403020100@9999"}}}
	for _, line := range strings.Split(strings.TrimSpace(want), "\n") {
		p.LastStop = addStopField(p.LastStop, line)
	}
	if e, _ := p.LastEnd(); e != EndClean {
		t.Fatalf("read back as %v", e)
	}
}

func TestHooksRunInTheOrderOfTheirNames(t *testing.T) {
	files := proc("init")
	log := ""
	root := tree(t, files)
	log = filepath.Join(root, "ran")
	hook := func(name, body string, mode os.FileMode) {
		p := filepath.Join(root, "etc/wsl-spawner/pre-stop.d", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!"+shPath(t)+"\necho "+name+" >>'"+log+"'\n"+body+"\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	hook("20-second", "echo plan=fake; exit 0", 0o755)
	hook("10-first", "exit 0", 0o755)
	hook("15-fails", "exit 3", 0o755)
	hook("README", "", 0o644)
	ls, out := stopIn(t, shtest.Tools(t), root, StopOptions{})
	if got := read(t, root, "ran"); got != "10-first\n15-fails\n20-second\n" {
		t.Fatalf("ran %q", got)
	}
	// A hook's own output never reaches the lines, even one that looks
	// like them.
	if strings.Contains(out, "plan=fake") {
		t.Fatalf("a hook's output leaked: %s", out)
	}
	want := []string{"plan:hook:10-first", "plan:hook:15-fails", "plan:hook:20-second", "plan:sync",
		"start:hook:10-first", "done:hook:10-first", "start:hook:15-fails", "done:hook:15-fails",
		"start:hook:20-second", "done:hook:20-second", "start:sync", "done:sync", "end:failed"}
	if !slices.Equal(kinds(ls), want) {
		t.Fatalf("%q", kinds(ls))
	}
	// The failed hook's code is said with it; the rest still ran.
	for _, l := range ls {
		if l.Kind == "done" && l.Hook == "15-fails" && l.Code != 3 {
			t.Fatalf("code %d", l.Code)
		}
	}
}

func TestAHooksNameIsTakenAsItIs(t *testing.T) {
	// A name with spaces in it, and one with a pattern's character: each
	// is one hook, run once, and said by its whole name. The tools first:
	// on anything but Linux the test stops there, before a name Windows
	// does not take is written.
	dir := shtest.Tools(t)
	root := tree(t, proc("init"))
	// Each hook notes its own name here as it runs.
	log := filepath.Join(root, "ran")
	for _, name := range []string{"10 dump db", "20-b", "2*"} {
		p := filepath.Join(root, "etc/wsl-spawner/pre-stop.d", name)
		// The folder, made once and found again for the others.
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		// The name in single quotes: the hook writes it as it is.
		if err := os.WriteFile(p, []byte("#!"+shPath(t)+"\necho '"+name+"' >>'"+log+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ls, out := stopIn(t, dir, root, StopOptions{})
	// In the order of their names, as the C locale puts them: "*" comes
	// before "0".
	if got := read(t, root, "ran"); got != "10 dump db\n2*\n20-b\n" {
		t.Fatalf("ran %q\n%s", got, out)
	}
	want := []string{"plan:hook:10 dump db", "plan:hook:2*", "plan:hook:20-b", "plan:sync",
		"start:hook:10 dump db", "done:hook:10 dump db", "start:hook:2*", "done:hook:2*",
		"start:hook:20-b", "done:hook:20-b", "start:sync", "done:sync", "end:clean"}
	if !slices.Equal(kinds(ls), want) {
		t.Fatalf("%q\n%s", kinds(ls), out)
	}
}

// shIsBusyBox reports whether this machine's sh is BusyBox's, which runs
// its own tools before any of the PATH's.
func shIsBusyBox(t *testing.T) bool {
	t.Helper()
	at, err := filepath.EvalSymlinks(shPath(t))
	return err == nil && filepath.Base(at) == "busybox"
}

// shPath is this machine's sh, for the made-up hooks' first line.
func shPath(t *testing.T) string {
	p, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	return p
}

// movingUptime makes the made-up root's uptime this machine's own, which
// moves: a stop times its steps by it, and one that must run out of time
// needs the time to pass. It is only read.
func movingUptime(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat("/proc/uptime"); err != nil {
		t.Skip("no /proc/uptime")
	}
	p := filepath.Join(root, "proc/uptime")
	// The made-up file goes, and a link to the machine's takes its place.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/proc/uptime", p); err != nil {
		t.Fatal(err)
	}
}

// timeouts are the timeout tools this machine has, by where they are:
// coreutils' says a command it ended as 124, BusyBox's as the TERM it
// sent, 143. A small distribution may have either.
func timeouts(t *testing.T) []string {
	t.Helper()
	var out []string
	if p, err := exec.LookPath("timeout"); err == nil {
		out = append(out, p)
	}
	// BusyBox runs as the tool its link is named for.
	if p, err := exec.LookPath("busybox"); err == nil {
		out = append(out, p)
	}
	if len(out) == 0 {
		t.Skip("no timeout here")
	}
	return out
}

func TestAHookThatRunsOutOfTimeEndsTheStopAsLimit(t *testing.T) {
	for _, tool := range timeouts(t) {
		root := tree(t, proc("init"))
		movingUptime(t, root)
		p := filepath.Join(root, "etc/wsl-spawner/pre-stop.d/10-slow")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!"+shPath(t)+"\nexec sleep 30\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		// This timeout, under the name the script calls it by.
		dir := shtest.Tools(t, "sleep")
		if err := os.Symlink(tool, filepath.Join(dir, "timeout")); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		ls, out := stopIn(t, dir, root, StopOptions{Limit: time.Second})
		if took := time.Since(start); took > 10*time.Second {
			t.Fatalf("%s: took %s", tool, took)
		}
		if end(ls) != "limit" {
			t.Fatalf("%s: %q\n%s", tool, kinds(ls), out)
		}
		// The hook's line says it ran out of time as coreutils says it,
		// whichever timeout ended it: the screen and the history read 124.
		for _, l := range ls {
			if l.Kind == "done" && l.Hook == "10-slow" && l.Code != 124 {
				t.Fatalf("%s: code %d\n%s", tool, l.Code, out)
			}
		}
	}
}

// systemd is a made-up systemctl for the stop: the units named active are,
// and every call it is asked to make that changes something is noted in
// the file it returns.
func systemd(t *testing.T, dir string, active ...string) (asked string) {
	t.Helper()
	asked = filepath.Join(dir, "systemctl.asked")
	shtest.Stub(t, dir, "systemctl", `case "$1" in
is-active) case ' `+strings.Join(active, " ")+` ' in *" $2 "*) echo active ;; *) echo inactive; exit 3 ;; esac ;;
daemon-reload | stop) printf '%s\n' "$*" >>'`+asked+`' ;;
*) echo "systemctl: not expected: $*" >&2; exit 64 ;;
esac`)
	return asked
}

// engine is a made-up docker or podman: its containers by id, each with a
// name and a restart policy, the ones running, live-restore, and every
// stop it is asked for noted in the file it returns.
func engineStub(t *testing.T, dir, name string, policies map[string]string, running []string, liveRestore bool) (asked string) {
	t.Helper()
	asked = filepath.Join(dir, name+".asked")
	// The loop below needs a command in it even with no container.
	inspect := ":\n"
	for _, id := range running {
		inspect += `[ "$i" = ` + id + ` ] && echo '` + id + ` /` + id + `-name ` + policies[id] + `'` + "\n"
	}
	shtest.Stub(t, dir, name, `case "$1" in
info) [ "$2" = --format ] && echo `+strconv.FormatBool(liveRestore)+`; exit 0 ;;
ps) for i in `+strings.Join(running, " ")+`; do echo "$i"; done ;;
inspect) shift 3; for i in "$@"; do
`+inspect+`done ;;
stop) printf '%s\n' "$*" >>'`+asked+`' ;;
*) echo "`+name+`: not expected: $*" >&2; exit 64 ;;
esac`)
	return asked
}

// lines is a file's lines, none for an empty or missing one.
func lines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestDockersDaemonStopsItsContainersWithTheTimeLeft(t *testing.T) {
	files := proc("systemd")
	files["run/systemd/system/"] = ""
	files["var/lib/wsl-spawner/"] = ""
	root := tree(t, files)
	dir := shtest.Tools(t, "mv", "mkdir")
	sysAsked := systemd(t, dir, "docker.service")
	dockAsked := engineStub(t, dir, "docker", map[string]string{"aaa": "unless-stopped", "bbb": "no", "ccc": "always"}, []string{"aaa", "bbb", "ccc"}, false)
	ls, out := stopIn(t, dir, root, StopOptions{Limit: 400 * time.Second})
	if end(ls) != "clean" {
		t.Fatalf("%q\n%s", kinds(ls), out)
	}
	// The daemon gets the whole limit from systemd, for this start only,
	// and its socket stops first so nothing starts it again.
	if got := read(t, root, "run/systemd/system/docker.service.d/zz-wsl-spawner.conf"); got != "[Service]\nTimeoutStopSec=400\n" {
		t.Fatalf("drop-in %q", got)
	}
	if got := lines(t, sysAsked); !slices.Equal(got, []string{"daemon-reload", "stop docker.socket docker.service"}) {
		t.Fatalf("systemctl asked %q", got)
	}
	// Without live-restore, no container is stopped one by one: that would
	// mark it stopped by hand, and its restart policy would not bring it
	// back.
	if got := lines(t, dockAsked); got != nil {
		t.Fatalf("docker asked %q", got)
	}
	// Every running one comes back at the next start, by default.
	if got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resume")); !slices.Equal(got, []string{"docker aaa aaa-name", "docker bbb bbb-name", "docker ccc ccc-name"}) {
		t.Fatalf("resume %q", got)
	}
	// The plan said it first, with the count.
	if ls[0] != (StopLine{Kind: "plan", Step: StepDocker, Count: 3}) {
		t.Fatalf("plan %+v", ls[0])
	}
}

func TestWithNoTimeLeftDockersDaemonGetsNoDropIn(t *testing.T) {
	// A hook takes the whole limit. systemd reads TimeoutStopSec=0 as no
	// limit at all, and the drop-in stays for the rest of this start: none
	// is written, and nothing is asked of systemd.
	files := proc("systemd")
	files["run/systemd/system/"] = ""
	root := tree(t, files)
	movingUptime(t, root)
	p := filepath.Join(root, "etc/wsl-spawner/pre-stop.d/10-slow")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!"+shPath(t)+"\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// mkdir is there, so a drop-in could be written.
	dir := shtest.Tools(t, "timeout", "sleep", "mkdir")
	sysAsked := systemd(t, dir, "docker.service")
	engineStub(t, dir, "docker", map[string]string{"aaa": "always"}, []string{"aaa"}, false)
	ls, out := stopIn(t, dir, root, StopOptions{Limit: time.Second})
	if _, err := os.Stat(filepath.Join(root, "run/systemd/system/docker.service.d/zz-wsl-spawner.conf")); !os.IsNotExist(err) {
		t.Fatalf("a drop-in was written: %q\n%s", read(t, root, "run/systemd/system/docker.service.d/zz-wsl-spawner.conf"), out)
	}
	if got := lines(t, sysAsked); got != nil {
		t.Fatalf("systemctl asked %q\n%s", got, out)
	}
	// The step ran out of time, and the stop says so.
	for _, l := range ls {
		if l.Kind == "done" && l.Step == StepDocker && l.Code != 124 {
			t.Fatalf("docker step code %d\n%s", l.Code, out)
		}
	}
	if end(ls) != "limit" {
		t.Fatalf("%q\n%s", kinds(ls), out)
	}
}

func TestNoQueryWaitsLongerThanTheLimitAndWhatComesBackIsNotedFirst(t *testing.T) {
	// Every wait the stop asks timeout for is within its limit, the
	// engines' queries before the steps too: a slow engine must not take
	// the stop past it, where wsl-spawner cuts it and nothing is recorded.
	// The containers to bring back are noted before anything stops, while
	// there is time for it.
	files := proc("systemd")
	files["run/systemd/system/"] = ""
	files["var/lib/wsl-spawner/"] = ""
	root := tree(t, files)
	dir := shtest.Tools(t, "mv", "mkdir")
	// A timeout that notes each limit it is given, then runs the command.
	// Asked what it takes, it says nothing; a kill's delay is not a limit.
	limits := filepath.Join(dir, "limits")
	shtest.Stub(t, dir, "timeout", `case "$1" in --help) exit 0 ;; -k) shift 2 ;; esac
printf '%s\n' "$1" >>'`+limits+`'; shift; exec "$@"`)
	// What runs, in the order it runs: the inspect that notes what comes
	// back, and the hook.
	order := filepath.Join(dir, "order")
	shtest.Stub(t, dir, "docker", `case "$1" in
info) [ "$2" = --format ] && echo false; exit 0 ;;
ps) echo aaa ;;
inspect) echo inspect >>'`+order+`'; echo 'aaa /web always' ;;
*) echo "docker: not expected: $*" >&2; exit 64 ;;
esac`)
	systemd(t, dir, "docker.service")
	p := filepath.Join(root, "etc/wsl-spawner/pre-stop.d/10-save")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!"+shPath(t)+"\necho hook >>'"+order+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The uptime does not move here: every step has the whole 5 s.
	ls, out := stopIn(t, dir, root, StopOptions{Limit: 5 * time.Second})
	if end(ls) != "clean" {
		t.Fatalf("%q\n%s", kinds(ls), out)
	}
	got := lines(t, limits)
	switch {
	case shIsBusyBox(t):
		// BusyBox's sh runs its own timeout, never one on the PATH: the
		// limits are not seen here.
		t.Log("sh is BusyBox's: the limits are not seen")
	case len(got) < 6:
		// Docker's three queries, its inspect, the hook and the daemon's
		// stop: fewer is a made-up timeout the script did not call.
		t.Fatalf("timeout asked %q\n%s", got, out)
	}
	for _, l := range got {
		if n, err := strconv.Atoi(l); err != nil || n < 1 || n > 5 {
			t.Fatalf("a wait of %q in a 5 s limit: %q", l, got)
		}
	}
	if got := lines(t, order); !slices.Equal(got, []string{"inspect", "hook"}) {
		t.Fatalf("order %q", got)
	}
	if got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resume")); !slices.Equal(got, []string{"docker aaa web"}) {
		t.Fatalf("resume %q", got)
	}
}

func TestLiveRestoreStopsTheContainersFirst(t *testing.T) {
	files := proc("systemd")
	files["run/systemd/system/"] = ""
	root := tree(t, files)
	dir := shtest.Tools(t, "mkdir")
	sysAsked := systemd(t, dir, "docker.service")
	dockAsked := engineStub(t, dir, "docker", map[string]string{"aaa": "no", "bbb": "no"}, []string{"aaa", "bbb"}, true)
	ls, _ := stopIn(t, dir, root, StopOptions{})
	if end(ls) != "clean" {
		t.Fatalf("%q", kinds(ls))
	}
	if got := lines(t, dockAsked); !slices.Equal(got, []string{"stop aaa bbb"}) {
		t.Fatalf("docker asked %q", got)
	}
	if got := lines(t, sysAsked); !slices.Equal(got, []string{"daemon-reload", "stop docker.socket docker.service"}) {
		t.Fatalf("systemctl asked %q", got)
	}
}

func TestWhatComesBackFollowsTheSetting(t *testing.T) {
	policies := map[string]string{"aaa": "unless-stopped", "bbb": "no", "ccc": "always", "ddd": "on-failure"}
	for resume, want := range map[string][]string{
		"running": {"docker aaa aaa-name", "docker bbb bbb-name", "docker ccc ccc-name", "docker ddd ddd-name"},
		"policy":  {"docker aaa aaa-name", "docker ccc ccc-name"},
		"off":     nil,
	} {
		files := proc("init")
		files["var/lib/wsl-spawner/"] = ""
		root := tree(t, files)
		dir := shtest.Tools(t, "mv")
		engineStub(t, dir, "docker", policies, []string{"aaa", "bbb", "ccc", "ddd"}, false)
		stopIn(t, dir, root, StopOptions{Resume: resume})
		if got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resume")); !slices.Equal(got, want) {
			t.Errorf("%s: %q", resume, got)
		}
	}
}

func TestWithoutSystemdTheDaemonIsAskedToStopAndWaitedFor(t *testing.T) {
	// A real process stands for the daemon: the stop sends it TERM and
	// waits until it is gone.
	daemon := exec.Command("sleep", "60")
	if err := daemon.Start(); err != nil {
		t.Skip("no sleep")
	}
	done := make(chan error, 1)
	go func() { done <- daemon.Wait() }()
	t.Cleanup(func() { daemon.Process.Kill() })
	files := proc("init")
	files["var/run/docker.pid"] = strconv.Itoa(daemon.Process.Pid) + "\n"
	// The made-up root's /proc names it the daemon.
	files["proc/"+strconv.Itoa(daemon.Process.Pid)+"/comm"] = "dockerd\n"
	root := tree(t, files)
	dir := shtest.Tools(t, "sleep")
	engineStub(t, dir, "docker", nil, nil, false)
	ls, out := stopIn(t, dir, root, StopOptions{})
	if end(ls) != "clean" {
		t.Fatalf("%q\n%s", kinds(ls), out)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("the daemon still runs\n%s", out)
	}
}

func TestWithoutSystemdADaemonThatCannotBeAskedFailsTheStep(t *testing.T) {
	// Docker answers, but there is no pid to ask its daemon by, or the pid
	// is of no process: the daemon was never asked to stop, and WSL ends it
	// with its containers.
	for name, pidFile := range map[string]string{"no pid file": "", "a pid of no process": "2147483646\n"} {
		files := proc("init")
		if pidFile != "" {
			files["var/run/docker.pid"] = pidFile
		}
		root := tree(t, files)
		dir := shtest.Tools(t)
		engineStub(t, dir, "docker", nil, nil, false)
		ls, out := stopIn(t, dir, root, StopOptions{})
		if end(ls) != "failed" {
			t.Fatalf("%s: %q\n%s", name, kinds(ls), out)
		}
		for _, l := range ls {
			if l.Kind == "done" && l.Step == StepDocker && l.Code == 0 {
				t.Fatalf("%s: the docker step said it went well\n%s", name, out)
			}
		}
	}
}

func TestAPidFileLeftByAnotherProcessIsNotSignalled(t *testing.T) {
	// The pid file names a process that is not dockerd: its number was
	// given since to another program. That one is not sent TERM, and the
	// step fails, as the daemon was never asked.
	other := exec.Command("sleep", "60")
	if err := other.Start(); err != nil {
		t.Skip("no sleep")
	}
	done := make(chan error, 1)
	go func() { done <- other.Wait() }()
	t.Cleanup(func() { other.Process.Kill() })
	files := proc("init")
	files["var/run/docker.pid"] = strconv.Itoa(other.Process.Pid) + "\n"
	files["proc/"+strconv.Itoa(other.Process.Pid)+"/comm"] = "sleep\n"
	root := tree(t, files)
	dir := shtest.Tools(t, "sleep")
	engineStub(t, dir, "docker", nil, nil, false)
	ls, out := stopIn(t, dir, root, StopOptions{})
	if end(ls) != "failed" {
		t.Fatalf("%q\n%s", kinds(ls), out)
	}
	select {
	case err := <-done:
		t.Fatalf("the other process was ended: %v\n%s", err, out)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestPodmansContainersAreStoppedEachWithItsTime(t *testing.T) {
	files := proc("init")
	files["var/lib/wsl-spawner/"] = ""
	root := tree(t, files)
	dir := shtest.Tools(t, "mv")
	asked := engineStub(t, dir, "podman", map[string]string{"ppp": "always"}, []string{"ppp"}, false)
	ls, _ := stopIn(t, dir, root, StopOptions{})
	if end(ls) != "clean" || !slices.Contains(kinds(ls), "done:podman") {
		t.Fatalf("%q", kinds(ls))
	}
	if got := lines(t, asked); !slices.Equal(got, []string{"stop ppp"}) {
		t.Fatalf("podman asked %q", got)
	}
	if got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resume")); !slices.Equal(got, []string{"podman ppp ppp-name"}) {
		t.Fatalf("resume %q", got)
	}
}

func TestTheUnitsListedStopFirstAndOnlyThoseThatRun(t *testing.T) {
	files := proc("systemd")
	files["run/systemd/system/"] = ""
	files["etc/wsl-spawner/stop-units"] = "# the game server first\nminecraft.service\n\nnot-running.service\npostgresql.service"
	root := tree(t, files)
	dir := shtest.Tools(t)
	asked := systemd(t, dir, "minecraft.service", "postgresql.service")
	ls, _ := stopIn(t, dir, root, StopOptions{})
	if got := lines(t, asked); !slices.Equal(got, []string{"stop minecraft.service postgresql.service"}) {
		t.Fatalf("systemctl asked %q", got)
	}
	if ls[0] != (StopLine{Kind: "plan", Step: StepUnits, Count: 2}) {
		t.Fatalf("plan %+v", ls[0])
	}
}

// windowsUnits is a stop-units file as an editor on Windows saves it, a
// carriage return ending each line, with blanks around a name.
const windowsUnits = "# the game server first\r\n  minecraft.service \r\n\r\nnot-running.service\t\r\npostgresql.service\r\n"

func TestStopUnitsSavedOnWindowsAreReadByTheirNames(t *testing.T) {
	files := proc("systemd")
	files["run/systemd/system/"] = ""
	files["etc/wsl-spawner/stop-units"] = windowsUnits
	root := tree(t, files)
	dir := shtest.Tools(t)
	// systemd knows the names only as they are, with nothing around them.
	asked := systemd(t, dir, "minecraft.service", "postgresql.service")
	ls, out := stopIn(t, dir, root, StopOptions{})
	if got := lines(t, asked); !slices.Equal(got, []string{"stop minecraft.service postgresql.service"}) {
		t.Fatalf("systemctl asked %q\n%s", got, out)
	}
	if ls[0] != (StopLine{Kind: "plan", Step: StepUnits, Count: 2}) {
		t.Fatalf("plan %+v", ls[0])
	}
}

func TestStopScriptTakesOnlyPlainValues(t *testing.T) {
	ok := StopOptions{Limit: time.Minute, Resume: "running", By: "hand", Now: time.Unix(5, 0)}
	s, err := StopScript(ok)
	if err != nil || !strings.HasPrefix(s, "LIMIT=60\nRESUME=running\nBY=hand\nNOW=5\n") {
		t.Fatalf("%q %v", s[:min(len(s), 60)], err)
	}
	for _, o := range []StopOptions{
		{Limit: 0, Resume: "running", By: "hand"},
		{Limit: time.Minute, Resume: "running; reboot", By: "hand"},
		{Limit: time.Minute, Resume: "running", By: "$(x)"},
		{Limit: time.Minute, Resume: "", By: "hand"},
	} {
		if _, err := StopScript(o); err == nil {
			t.Errorf("%+v went into the script", o)
		}
	}
}

func TestParseStopLine(t *testing.T) {
	for in, want := range map[string]StopLine{
		"plan=docker\t3":             {Kind: "plan", Step: "docker", Count: 3},
		"plan=hook\t10-save":         {Kind: "plan", Step: "hook", Hook: "10-save"},
		"plan=sync\t":                {Kind: "plan", Step: "sync"},
		"step=docker\tstart\t3":      {Kind: "start", Step: "docker", Count: 3},
		"step=docker\tdone\t0\t21":   {Kind: "done", Step: "docker", Took: 21 * time.Second},
		"hook=10-save\tdone\t3\t2\r": {Kind: "done", Step: "hook", Hook: "10-save", Code: 3, Took: 2 * time.Second},
		"end=limit":                  {Kind: "end", Result: "limit"},
	} {
		if got, ok := ParseStopLine(in); !ok || got != want {
			t.Errorf("%q: %+v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "plain words", "step=docker\tmaybe", "end=", "plan=", "other=1"} {
		if l, ok := ParseStopLine(in); ok {
			t.Errorf("%q read as %+v", in, l)
		}
	}
}

func TestTheContainersOfAnEngineThatDoesNotAnswerStayToBringBack(t *testing.T) {
	// The last start kept these until their engine answers; a stop while
	// it still does not knows nothing new of them, and keeps them. A line
	// of anything but the two engines goes, as the next start would run
	// its first word.
	for _, tc := range []struct {
		name, resume string
		answers      bool
		want         []string
	}{
		{"nothing answers", "running", false, []string{"docker abc web", "podman ppp cache"}},
		// Docker answers: its lines are what runs now; Podman's stay.
		{"docker answers", "running", true, []string{"docker aaa aaa-name", "podman ppp cache"}},
		// Nothing comes back at all: nothing is kept.
		{"resume off", "off", false, nil},
	} {
		files := proc("init")
		files["var/lib/wsl-spawner/resume"] = "docker abc web\npodman ppp cache\nrm -rf /tmp/x\n"
		root := tree(t, files)
		dir := shtest.Tools(t, "mv")
		if tc.answers {
			engineStub(t, dir, "docker", map[string]string{"aaa": "no"}, []string{"aaa"}, false)
		} else {
			shtest.Stub(t, dir, "docker", `echo 'Cannot connect to the Docker daemon' >&2; exit 1`)
		}
		// Without systemd and a pid file, an answering daemon's own stop
		// fails: only the list is looked at here.
		ls, out := stopIn(t, dir, root, StopOptions{Resume: tc.resume})
		if got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resume")); !slices.Equal(got, tc.want) {
			t.Errorf("%s: resume %q\n%q\n%s", tc.name, got, kinds(ls), out)
		}
	}
}

func TestContainersRemovedBeforeTheEngineStopsAreNotBroughtBack(t *testing.T) {
	// A hook removes db, as docker compose down does; tmp runs with --rm,
	// so its engine removes it as it stops. Neither is there to start
	// again: the next start would find them gone.
	files := proc("init")
	files["var/lib/wsl-spawner/"] = ""
	root := tree(t, files)
	dir := shtest.Tools(t, "mv")
	removed := filepath.Join(root, "db-removed")
	shtest.Stub(t, dir, "docker", `case "$1 $2" in
'info ') exit 0 ;;
'info --format') echo false ;;
'ps -q') printf '%s\n' aaa bbb ccc ;;
'ps -aq') echo aaa; [ -f '`+removed+`' ] || echo bbb; echo ccc ;;
'inspect --format') echo 'aaa /web no false'; echo 'bbb /db no false'; echo 'ccc /tmp no true' ;;
*) echo "docker: not expected: $*" >&2; exit 64 ;;
esac`)
	p := filepath.Join(root, "etc/wsl-spawner/pre-stop.d/10-down")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!"+shPath(t)+"\n: >'"+removed+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ls, out := stopIn(t, dir, root, StopOptions{})
	if got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resume")); !slices.Equal(got, []string{"docker aaa web"}) {
		t.Fatalf("resume %q\n%q\n%s", got, kinds(ls), out)
	}
	// An engine that does not say which containers it has leaves the list
	// as it is: none is dropped on a guess.
	files = proc("init")
	files["var/lib/wsl-spawner/"] = ""
	root = tree(t, files)
	dir = shtest.Tools(t, "mv")
	shtest.Stub(t, dir, "docker", `case "$1 $2" in
'info ') exit 0 ;;
'info --format') echo false ;;
'ps -q') echo aaa ;;
'ps -aq') exit 1 ;;
'inspect --format') echo 'aaa /web no false' ;;
*) echo "docker: not expected: $*" >&2; exit 64 ;;
esac`)
	p = filepath.Join(root, "etc/wsl-spawner/pre-stop.d/10-noop")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!"+shPath(t)+"\n:\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stopIn(t, dir, root, StopOptions{})
	if got := lines(t, filepath.Join(root, "var/lib/wsl-spawner/resume")); !slices.Equal(got, []string{"docker aaa web"}) {
		t.Fatalf("resume %q", got)
	}
}

func TestAStepThatGoesOnPastItsTimeRanOutOfIt(t *testing.T) {
	// The hook ignores the TERM timeout sends and ends by itself later:
	// whatever it returns, it went on past its time. BusyBox's timeout
	// said 0, which made the stop clean.
	for _, tool := range timeouts(t) {
		root := tree(t, proc("init"))
		movingUptime(t, root)
		p := filepath.Join(root, "etc/wsl-spawner/pre-stop.d/10-deaf")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!"+shPath(t)+"\ntrap '' TERM\nsleep 3\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		dir := shtest.Tools(t, "sleep")
		if err := os.Symlink(tool, filepath.Join(dir, "timeout")); err != nil {
			t.Fatal(err)
		}
		ls, out := stopIn(t, dir, root, StopOptions{Limit: time.Second})
		if end(ls) != "limit" {
			t.Fatalf("%s: %q\n%s", tool, kinds(ls), out)
		}
		for _, l := range ls {
			if l.Kind == "done" && l.Hook == "10-deaf" && l.Code != 124 {
				t.Fatalf("%s: code %d\n%s", tool, l.Code, out)
			}
		}
	}
}

func TestAStepThatIgnoresTermIsKilledWhereTimeoutCan(t *testing.T) {
	// Where timeout takes -k, a hook that never ends at TERM is killed a
	// few seconds after its time: the stop goes on to its other steps and
	// its record, within the margin wsl-spawner gives it.
	at, err := exec.LookPath("timeout")
	if err != nil {
		t.Skip("no timeout here")
	}
	// BusyBox's sh runs its own timeout, never the PATH's: that one is
	// asked what it takes.
	help, _ := exec.Command(at, "--help").CombinedOutput()
	if shIsBusyBox(t) {
		help, _ = exec.Command(shPath(t), "-c", "timeout --help").CombinedOutput()
	}
	if !strings.Contains(string(help), "-k") {
		t.Skip("the timeout this sh runs takes no -k")
	}
	files := proc("init")
	files["var/lib/wsl-spawner/"] = ""
	root := tree(t, files)
	movingUptime(t, root)
	p := filepath.Join(root, "etc/wsl-spawner/pre-stop.d/10-stuck")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!"+shPath(t)+"\ntrap '' TERM\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := shtest.Tools(t, "sleep", "timeout", "mv")
	start := time.Now()
	ls, out := stopIn(t, dir, root, StopOptions{Limit: time.Second})
	if took := time.Since(start); took > 15*time.Second {
		t.Fatalf("took %s\n%s", took, out)
	}
	if end(ls) != "limit" || !strings.Contains(read(t, root, "var/lib/wsl-spawner/last-stop"), "result=limit") {
		t.Fatalf("%q\n%s", kinds(ls), out)
	}
}

func TestAHookNamedWithATabOrALineBreakIsSaidAsALineHoldsIt(t *testing.T) {
	// The hook runs by its own name; its lines, and the probe's, say it
	// with a question mark for the tab or line break, which would end a
	// field or a line. The tools first: on anything but Linux the test
	// stops there, before a name Windows does not take is written.
	dir := shtest.Tools(t)
	root := tree(t, proc("init"))
	log := filepath.Join(root, "ran")
	for _, name := range []string{"10\tdump", "20\nstop=forged"} {
		p := filepath.Join(root, "etc/wsl-spawner/pre-stop.d", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!"+shPath(t)+"\necho ran >>'"+log+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ls, out := stopIn(t, dir, root, StopOptions{})
	if got := read(t, root, "ran"); got != "ran\nran\n" {
		t.Fatalf("ran %q\n%s", got, out)
	}
	want := []string{"plan:hook:10?dump", "plan:hook:20?stop=forged", "plan:sync",
		"start:hook:10?dump", "done:hook:10?dump", "start:hook:20?stop=forged", "done:hook:20?stop=forged",
		"start:sync", "done:sync", "end:clean"}
	if !slices.Equal(kinds(ls), want) {
		t.Fatalf("%q\n%s", kinds(ls), out)
	}
	_, pr := probe(t, dir, root)
	if !slices.Equal(pr.Hooks, []string{"10?dump", "20?stop=forged"}) {
		t.Fatalf("probe hooks %q", pr.Hooks)
	}
}

func TestTheUnitsAreStoppedByTheirNamesAlone(t *testing.T) {
	// A pattern in stop-units is a pattern systemd reads: it is never
	// matched against the files of the folder the program ran from, here
	// this package's, which holds .go files.
	files := proc("systemd")
	files["run/systemd/system/"] = ""
	files["etc/wsl-spawner/stop-units"] = "*.go\n"
	root := tree(t, files)
	dir := shtest.Tools(t)
	asked := systemd(t, dir, "*.go")
	stopIn(t, dir, root, StopOptions{})
	if got := lines(t, asked); !slices.Equal(got, []string{"stop *.go"}) {
		t.Fatalf("systemctl asked %q", got)
	}
}
