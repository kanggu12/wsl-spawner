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

// tree makes a made-up distribution's files under a temporary root: each
// key is a path below the root, each value the file's text.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, body := range files {
		full := filepath.Join(root, p)
		// A key that ends in a slash is a folder, made empty.
		if strings.HasSuffix(p, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// proc is the /proc of a made-up distribution whose init is name and
// started 4242 clock ticks after the kernel booted, 98765 seconds ago.
func proc(name string) map[string]string {
	return map[string]string{
		"proc/sys/kernel/random/boot_id": "0f0e0d0c-0b0a-4908-8706-050403020100\n",
		// Fields 3 to 22 of /proc/1/stat: the start is the last here.
		"proc/1/stat":   "1 (" + name + ") S 0 1 1 0 -1 4194560 100 200 3 4 5 6 7 8 20 0 1 0 4242 1000 30\n",
		"proc/1/comm":   name + "\n",
		"proc/uptime":   "98765.43 1234.56\n",
		"proc/stat":     "cpu 1 2 3\n",
		"etc/hostname":  "made-up\n",
		"var/lib/none/": "",
	}
}

// probe runs the probe script under root with only the tools in dir.
func probe(t *testing.T, dir, root string) (string, Probe) {
	t.Helper()
	out, err := shtest.Run(t, dir, "R='"+root+"'\n"+ProbeScript())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	p, err := Parse(out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return out, p
}

func TestProbeNeedsNothingButTheShell(t *testing.T) {
	// A distribution with no tool at all: no systemd, no engine, no
	// wslpath. The probe still says what it can, and says it finished.
	dir := shtest.Tools(t)
	out, p := probe(t, dir, tree(t, proc("init")))
	if !p.Complete || p.Init != "init" || p.Systemd != "" || p.Interop || len(p.Engines) != 0 {
		t.Fatalf("%+v\n%s", p, out)
	}
	if p.RunID() != "0f0e0d0c-0b0a-4908-8706-050403020100@4242" || p.Uptime != 98765*time.Second {
		t.Fatalf("run %q, uptime %s", p.RunID(), p.Uptime)
	}
	// Not set up: nothing of wsl-spawner's is there, and nothing is judged.
	if p.SetUp() || p.KeptRunning() {
		t.Fatalf("set up: %+v", p)
	}
	if end, _ := p.LastEnd(); end != EndUnknown {
		t.Fatalf("ending %v", end)
	}
}

func TestProbeReadsAnInitNameWithSpacesAndParentheses(t *testing.T) {
	// The start is counted after the last closing parenthesis.
	_, p := probe(t, shtest.Tools(t), tree(t, proc("odd) (name")))
	if p.InitStart != "4242" {
		t.Fatalf("start %q", p.InitStart)
	}
}

func TestProbePicksNoTextApartWithTools(t *testing.T) {
	if tool := shtest.TextTool(ProbeScript()); tool != "" {
		t.Fatalf("the probe calls %s: a small distribution may have none", tool)
	}
}

// systemctl is a made-up systemctl that answers the probe's questions: the
// state, two failed units, and the units given as loaded.
func systemctl(t *testing.T, dir string, units map[string][2]string) {
	t.Helper()
	cases := ""
	for name, st := range units {
		cases += "'show -p LoadState --value " + name + "') echo loaded ;;\n"
		cases += "'is-active " + name + "') echo " + st[0] + " ;;\n"
		cases += "'is-enabled " + name + "') echo " + st[1] + " ;;\n"
	}
	shtest.Stub(t, dir, "systemctl", `case "$*" in
is-system-running) echo degraded; exit 1 ;;
'list-units --failed --plain --no-legend') printf '%s\n' 'getty@tty1.service loaded failed failed Getty on tty1' 'made-up.service loaded failed failed A made-up unit' ;;
`+cases+`'show -p LoadState --value '*) echo not-found ;;
*) echo "systemctl: not expected: $*" >&2; exit 64 ;;
esac`)
}

func TestProbeReadsSystemdAndTheUnits(t *testing.T) {
	dir := shtest.Tools(t)
	systemctl(t, dir, map[string][2]string{
		"docker.service":     {"active", "enabled"},
		KeepAliveUnit:        {"active", "enabled"},
		StartUnit:            {"inactive", "enabled"},
		"podman.socket":      {"inactive", "disabled"},
		"containerd.service": {"active", "enabled"},
	})
	files := proc("systemd")
	files["run/systemd/system/"] = ""
	files["proc/sys/fs/binfmt_misc/WSLInterop"] = "enabled\ninterpreter /init\n"
	out, p := probe(t, dir, tree(t, files))
	if p.Systemd != "degraded" || !slices.Equal(p.Failed, []string{"getty@tty1.service", "made-up.service"}) {
		t.Fatalf("%+v\n%s", p, out)
	}
	if len(p.Units) != 5 || p.Units["docker.service"] != (Unit{"active", "enabled"}) || !p.KeptRunning() {
		t.Fatalf("units %+v", p.Units)
	}
	// A unit systemd does not have is left out, not listed as off.
	if _, ok := p.Units["snap.docker.dockerd.service"]; ok {
		t.Fatal("a missing unit was listed")
	}
	if !p.Interop {
		t.Fatal("interop not read")
	}
}

func TestProbeReadsInteropOnlyWhenItIsOn(t *testing.T) {
	files := proc("systemd")
	files["proc/sys/fs/binfmt_misc/WSLInterop"] = "disabled\n"
	_, p := probe(t, shtest.Tools(t), tree(t, files))
	if p.Interop {
		t.Fatal("a handler that is off read as on")
	}
	// The name newer WSL releases register works as well.
	files["proc/sys/fs/binfmt_misc/WSLInterop-late"] = "enabled\n"
	_, p = probe(t, shtest.Tools(t), tree(t, files))
	if !p.Interop {
		t.Fatal("WSLInterop-late not read")
	}
}

func TestProbeFindsWaitforWhereTheDriveIsMounted(t *testing.T) {
	dir := shtest.Tools(t)
	files := proc("systemd")
	files["win/c/Windows/System32/waitfor.exe"] = "MZ"
	root := tree(t, files)
	shtest.Stub(t, dir, "wslpath", `[ "$1" = -u ] && echo /win/c/Windows/System32/waitfor.exe`)
	_, p := probe(t, dir, root)
	if p.Waitfor != "/win/c/Windows/System32/waitfor.exe" {
		t.Fatalf("waitfor %q", p.Waitfor)
	}
	// Where the file is not, nothing is said.
	_, p = probe(t, dir, tree(t, proc("systemd")))
	if p.Waitfor != "" {
		t.Fatalf("waitfor %q with no file", p.Waitfor)
	}
}

// docker is a made-up docker whose daemon answers when up, with the given
// inspect lines for its containers.
func docker(t *testing.T, dir string, up bool, liveRestore string, inspect []string) {
	t.Helper()
	info := "exit 0"
	if !up {
		info = "echo 'Cannot connect to the Docker daemon' >&2; exit 1"
	}
	ids, lines := "", ""
	for _, l := range inspect {
		ids += strings.SplitN(l, "\t", 2)[0] + " "
		lines += "'" + l + "' "
	}
	shtest.Stub(t, dir, "docker", `case "$1 $2" in
'info ') `+info+` ;;
'info --format') echo `+liveRestore+` ;;
'ps -aq') for i in `+ids+`; do echo "$i"; done ;;
'inspect --format') case "$3" in *'index .Config "StopTimeout"'*'index .HostConfig "AutoRemove"'*) ;; *) echo "wrong format: $3" >&2; exit 64 ;; esac
	[ -n "`+strings.TrimSpace(ids)+`" ] && printf '%s\n' `+lines+` ;;
*) echo "docker: not expected: $*" >&2; exit 64 ;;
esac`)
}

func TestProbeReadsDockerContainers(t *testing.T) {
	dir := shtest.Tools(t)
	docker(t, dir, true, "false", []string{
		"aaa111\t/web\trunning\tunless-stopped\t<no value>\tnginx:1\tsite\t8080:80/tcp,:443/tcp,5353:53/udp,",
		"bbb222\t/db\trunning\tno\t60\tpostgres:17\tsite",
		"ccc333\t/job\texited\t\t<no value>\tbusybox\t<no value>",
		// Run with --rm: its engine removes it as it stops.
		"ddd444\t/once\trunning\tno\t<no value>\tbusybox\t<no value>\t\ttrue",
	})
	out, p := probe(t, dir, tree(t, proc("systemd")))
	if !p.Engines["docker"] || p.LiveRestore || len(p.Containers) != 4 || !p.Containers[3].AutoRemove || p.Containers[0].AutoRemove {
		t.Fatalf("%+v\n%s", p, out)
	}
	web, db, job := p.Containers[0], p.Containers[1], p.Containers[2]
	if web.Name != "web" || !web.Running() || !web.Restarts() || web.Wait() != DefaultStopTimeout || web.Project != "site" ||
		!slices.Equal(web.Ports, []string{"8080/tcp", "5353/udp"}) {
		t.Fatalf("web %+v", web)
	}
	if db.Restarts() || db.StopTimeout != time.Minute || db.Wait() != time.Minute || db.Image != "postgres:17" {
		t.Fatalf("db %+v", db)
	}
	// Nothing set reads as empty, not as the engine's placeholder.
	if job.Running() || job.Policy != "" || job.Project != "" || job.StopTimeout != 0 {
		t.Fatalf("job %+v", job)
	}
}

func TestProbeReadsAContainerWhoseProjectLabelHoldsATabOrALineBreak(t *testing.T) {
	// A label is free text. One with a tab or a line break must not move
	// the container's other fields, or split it in two.
	dir := shtest.Tools(t)
	// Docker as it renders the probe's format: the label quoted as Go
	// quotes it where the format asks printf for that, as it is otherwise.
	shtest.Stub(t, dir, "docker", `case "$1 $2" in
'info ') exit 0 ;;
'info --format') echo false ;;
'ps -aq') printf '%s\n' aaa111 bbb222 ccc333 ;;
'inspect --format') case "$3" in
	*'printf "%q"'*)
		printf '%s\t%s\t%s\t%s\t\t%s\t%s\t%s\n' aaa111 /web running always nginx:1 '"a\tb"' '8080:80/tcp,'
		printf '%s\t%s\t%s\t%s\t\t%s\t%s\t%s\n' bbb222 /db running no postgres:17 '"x\ny"' '5432:5432/tcp,'
		printf '%s\t%s\t%s\t%s\t\t%s\t%s\t%s\n' ccc333 /cache running always redis:7 '"site"' '' ;;
	*)
		printf 'aaa111\t/web\trunning\talways\t\tnginx:1\ta\tb\t8080:80/tcp,\n'
		printf 'bbb222\t/db\trunning\tno\t\tpostgres:17\tx\ny\t5432:5432/tcp,\n'
		printf 'ccc333\t/cache\trunning\talways\t\tredis:7\tsite\t\n' ;;
	esac ;;
*) echo "docker: not expected: $*" >&2; exit 64 ;;
esac`)
	out, p := probe(t, dir, tree(t, proc("systemd")))
	if len(p.Containers) != 3 {
		t.Fatalf("%+v\n%s", p.Containers, out)
	}
	web, db, cache := p.Containers[0], p.Containers[1], p.Containers[2]
	// Each keeps its ports; a name with a control character is none
	// Compose gives, and is no project.
	if web.Name != "web" || !slices.Equal(web.Ports, []string{"8080/tcp"}) || web.Project != "" {
		t.Fatalf("web %+v", web)
	}
	if db.Name != "db" || !slices.Equal(db.Ports, []string{"5432/tcp"}) || db.Project != "" {
		t.Fatalf("db %+v", db)
	}
	if cache.Project != "site" {
		t.Fatalf("cache %+v", cache)
	}
}

func TestProbeSaysADockerThatDoesNotAnswer(t *testing.T) {
	dir := shtest.Tools(t)
	docker(t, dir, false, "false", []string{"aaa111\t/web\trunning\tno\t\tx\t"})
	_, p := probe(t, dir, tree(t, proc("systemd")))
	if up, ok := p.Engines["docker"]; !ok || up || len(p.Containers) != 0 {
		t.Fatalf("%+v", p)
	}
}

func TestProbeReadsLiveRestoreAndNoContainers(t *testing.T) {
	dir := shtest.Tools(t)
	docker(t, dir, true, "true", nil)
	_, p := probe(t, dir, tree(t, proc("systemd")))
	if !p.Engines["docker"] || !p.LiveRestore || len(p.Containers) != 0 {
		t.Fatalf("%+v", p)
	}
}

func TestProbeUsesPodmansOwnFormat(t *testing.T) {
	// Podman reads the format against its own types: the stop timeout is a
	// field, never a key to index.
	dir := shtest.Tools(t)
	shtest.Stub(t, dir, "podman", `case "$1 $2" in
'info ') exit 0 ;;
'ps -aq') echo ddd444 ;;
'inspect --format') case "$3" in *'{{.Config.StopTimeout}}'*'{{.HostConfig.AutoRemove}}'*) printf 'ddd444\tcache\trunning\talways\t10\tredis:8\t\t\tfalse\n' ;; *) exit 64 ;; esac ;;
*) exit 64 ;;
esac`)
	_, p := probe(t, dir, tree(t, proc("systemd")))
	if len(p.Containers) != 1 || p.Containers[0].Engine != "podman" || p.Containers[0].Name != "cache" || !p.Containers[0].Restarts() {
		t.Fatalf("%+v", p.Containers)
	}
}

func TestProbeReadsWhatWslSpawnerKeeps(t *testing.T) {
	files := proc("systemd")
	files["usr/local/lib/wsl-spawner/start.sh"] = StartScriptMark + "1\n"
	files["etc/wsl-spawner/pre-stop.d/10-save-world"] = "#!/bin/sh\n"
	files["etc/wsl-spawner/pre-stop.d/README"] = "not run: not a program\n"
	files["var/lib/wsl-spawner/runs"] = "1700000000 0f0e0d0c-0b0a-4908-8706-050403020100@100\n1700000500 0f0e0d0c-0b0a-4908-8706-050403020100@4242\n"
	files["var/lib/wsl-spawner/last-stop"] = "run=0f0e0d0c-0b0a-4908-8706-050403020100@100\nat=1700000400\ntook=41\nresult=clean\nby=signout\n"
	files["var/lib/wsl-spawner/resume"] = "docker bbb222 db\n"
	files["etc/wsl-spawner/stop-units"] = "# first\nminecraft.service\n\npostgresql.service"
	root := tree(t, files)
	// The README is not a program: only what can run is a hook.
	if err := os.Chmod(filepath.Join(root, "etc/wsl-spawner/pre-stop.d/README"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, p := probe(t, shtest.Tools(t), root)
	if !p.SetUp() || !slices.Equal(p.Hooks, []string{"10-save-world"}) || len(p.Runs) != 2 ||
		!slices.Equal(p.StopUnits, []string{"minecraft.service", "postgresql.service"}) {
		t.Fatalf("%+v", p)
	}
	if s := p.LastStop; s == nil || s.Took != 41*time.Second || s.By != "signout" || s.At.Unix() != 1700000400 {
		t.Fatalf("last stop %+v", p.LastStop)
	}
	if len(p.Bring) != 1 || p.Bring[0] != (Bring{"docker", "bbb222", "db"}) {
		t.Fatalf("bring %+v", p.Bring)
	}
	// The start before this one ended in wsl-spawner's stop, every step done.
	if end, run := p.LastEnd(); end != EndClean || run.At.Unix() != 1700000000 {
		t.Fatalf("ending %v %+v", end, run)
	}
}

func TestProbeReadsStopUnitsSavedOnWindowsByTheirNames(t *testing.T) {
	// The file the stop reads, as the stop reads it: each name without the
	// blanks and carriage return around it, and no empty line as a name.
	files := proc("systemd")
	files["etc/wsl-spawner/stop-units"] = windowsUnits
	_, p := probe(t, shtest.Tools(t), tree(t, files))
	if want := []string{"minecraft.service", "not-running.service", "postgresql.service"}; !slices.Equal(p.StopUnits, want) {
		t.Fatalf("%q", p.StopUnits)
	}
}

func TestLastEnd(t *testing.T) {
	const boot = "b"
	now := RunID(boot, "300")
	base := Probe{BootID: boot, InitStart: "300", StartScript: StartScriptMark + "1"}
	// Each start noted 100 s after the one before it, the first at 0.
	runs := func(ids ...string) []Run {
		var rs []Run
		for i, id := range ids {
			rs = append(rs, Run{At: time.Unix(int64(i)*100, 0), ID: id})
		}
		return rs
	}
	stop := func(run, result string) *StopRecord { return &StopRecord{Run: run, Result: result} }
	// A stop that ended at the given second.
	stopAt := func(run, result string, at int64) *StopRecord {
		return &StopRecord{Run: run, Result: result, At: time.Unix(at, 0)}
	}
	for _, c := range []struct {
		name string
		runs []Run
		stop *StopRecord
		want Ending
	}{
		{"the first start noted", runs(now), nil, EndFirst},
		{"this start not noted yet, none before", nil, nil, EndFirst},
		{"stopped by wsl-spawner", runs("b@100", now), stop("b@100", "clean"), EndClean},
		{"this start not noted yet", runs("b@100"), stop("b@100", "clean"), EndClean},
		{"a step cut at the limit", runs("b@100", now), stop("b@100", "limit"), EndLimit},
		{"a step failed", runs("b@100", now), stop("b@100", "failed"), EndFailed},
		// An older stop does not speak for the start after it.
		{"ended another way after an older stop", runs("b@100", "b@200", now), stop("b@100", "clean"), EndCut},
		{"never stopped by wsl-spawner", runs("b@100", now), nil, EndCut},
		// A start stopped before the start script noted it (stopped right
		// after it began, or with the script still waiting for an engine)
		// came after the one noted before it: its stop speaks for it.
		{"stopped before its start was noted", runs("b@100", now), stopAt("b@200", "clean", 50), EndClean},
		{"stopped before its start was noted, cut at the limit", runs("b@100", now), stopAt("b@200", "limit", 50), EndLimit},
		// One that ended before the start noted last began does not.
		{"an older start, never noted", runs("b@100", "b@150", now), stopAt("b@120", "clean", 50), EndCut},
		// Nor does a stop of this very start, or a record that names none.
		{"a stop of this start", runs("b@100", now), stopAt(now, "clean", 150), EndCut},
		{"a record naming no start", runs("b@100", now), stopAt("", "clean", 50), EndCut},
	} {
		p := base
		p.Runs, p.LastStop = c.runs, c.stop
		if got, _ := p.LastEnd(); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// The start judged is the one the stop names, when it was not noted.
	p := base
	p.Runs, p.LastStop = runs("b@100", now), stopAt("b@200", "clean", 50)
	if _, run := p.LastEnd(); run.ID != "b@200" {
		t.Fatalf("judged %+v", run)
	}
}

func TestParseRefusesWhatIsNotTheProbe(t *testing.T) {
	// WSL's own message, or a shell that ran something else.
	for _, out := range []string{"", "The system cannot find the file specified.\r\n", "boot_id=x\n"} {
		if _, err := Parse(out); err != ErrNoProbe {
			t.Errorf("%q: %v", out, err)
		}
	}
	// A probe cut off says so.
	p, err := Parse("probe=1\r\nboot_id=x\r\n")
	if err != nil || p.Complete || p.BootID != "x" {
		t.Fatalf("%+v %v", p, err)
	}
	// Lines it does not know, and broken records, are skipped.
	p, err = Parse("probe=1\nnew_key=1\ncontainer=docker\tonly\ttwo\nrun=x\nbring=docker id\nend=1\n")
	if err != nil || !p.Complete || len(p.Containers) != 0 || len(p.Runs) != 0 || len(p.Bring) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
}

func FuzzParse(f *testing.F) {
	// The probe's own lines, and lines it never prints, as seeds.
	f.Add("probe=1\nboot_id=b\ninit_start=7\ncontainer=docker\tid\t/n\trunning\tno\t<nil>\timg\t<no value>\nrun=1 b@6\nstop=run=b@6\nstop=result=clean\nend=1\n")
	f.Add("probe=1\r\nunit=a\tb\r\nbring=x\r\nstop=took=-5\r\n")
	f.Fuzz(func(t *testing.T, out string) {
		p, err := Parse(out)
		if err != nil {
			return
		}
		// Whatever it read, judging it never fails and the kinds hold.
		end, _ := p.LastEnd()
		if end < EndUnknown || end > EndCut {
			t.Fatalf("ending %d", end)
		}
		for _, c := range p.Containers {
			if c.Wait() <= 0 || c.ID == "" {
				t.Fatalf("container %+v", c)
			}
		}
	})
}
