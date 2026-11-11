# wsl-spawner probe: what runs in this distribution, read only. It changes
# nothing. Each line is KEY=VALUE, or KEY= and fields split by tabs; a reader
# skips a key it does not know, so a newer script can say more. Only the
# shell picks text apart: a small distribution may have no text tools.
#
# R is put in front of every path read, for the tests; it is empty here.
R="${R:-}"
# Tools are found where a distribution keeps them, whatever PATH wsl.exe
# brought, snap's Docker in /snap/bin included; a test sets its own PATH
# and keeps it.
[ -n "$R" ] || PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin
export PATH LC_ALL=C

# say prints one KEY=VALUE line; a value never holds a line break.
say() { printf '%s=%s\n' "$1" "$2"; }

# have reports whether a tool is on PATH.
have() { command -v "$1" >/dev/null 2>&1; }

# first prints the first line of a file, or nothing.
first() { [ -r "$1" ] && { IFS= read -r l <"$1" || [ -n "$l" ]; } && printf '%s' "$l"; }

# limited runs a command with a time limit where timeout is there: an engine
# whose daemon hangs must not hold the probe.
limited() { if have timeout; then timeout "$@"; else shift; "$@"; fi; }

say probe 1
# This distribution's clock, to set beside Windows'.
have date && say now "$(date +%s)"

# The VM's kernel boot, which every distribution shares, and this one's own
# start: its init's start time, in clock ticks after the kernel's boot, is
# field 22 of /proc/1/stat. The name in parentheses may hold spaces, so the
# fields are read after its closing one; there the start is field 20.
say boot_id "$(first "$R/proc/sys/kernel/random/boot_id")"
stat1=$(first "$R/proc/1/stat")
# Split on spaces only: no field is a pattern to expand, and the hooks
# below are found by one, so expansion comes back on after.
set -f
# shellcheck disable=SC2086
set -- ${stat1##*) }
set +f
say init_start "${20:-}"
say init "$(first "$R/proc/1/comm")"
# Seconds since the kernel booted, whole.
up=$(first "$R/proc/uptime")
say uptime "${up%%.*}"

# systemd, when it is this distribution's init: how far it is ("running",
# "degraded", "starting", "stopping"), and the units that failed.
if [ -d "$R/run/systemd/system" ] && have systemctl; then
	say systemd "$(limited 10 systemctl is-system-running 2>/dev/null)"
	limited 10 systemctl list-units --failed --plain --no-legend 2>/dev/null | while read -r u _; do
		[ -n "$u" ] && say failed "$u"
	done
	# The units wsl-spawner reads or puts in place: name, whether it is
	# loaded, active and enabled.
	for u in wsl-spawner.service wsl-spawner-keepalive.service docker.service docker.socket \
		snap.docker.dockerd.service containerd.service podman.socket podman-restart.service; do
		load=$(limited 10 systemctl show -p LoadState --value "$u" 2>/dev/null)
		[ "$load" = loaded ] || continue
		act=$(limited 10 systemctl is-active "$u" 2>/dev/null)
		en=$(limited 10 systemctl is-enabled "$u" 2>/dev/null)
		printf 'unit=%s\t%s\t%s\n' "$u" "${act:-unknown}" "${en:-unknown}"
	done
else
	say systemd ""
fi

# Interop: Windows programs run from here only while WSL's handler for them
# is registered and on. Keeping a distribution running needs it.
interop=0
for f in "$R/proc/sys/fs/binfmt_misc/WSLInterop" "$R/proc/sys/fs/binfmt_misc/WSLInterop-late"; do
	[ "$(first "$f")" = enabled ] && interop=1
done
say interop "$interop"
# Windows' waitfor.exe, which the keep-running service holds, where this
# distribution mounts the Windows drive.
if have wslpath; then
	w=$(wslpath -u 'C:\Windows\System32\waitfor.exe' 2>/dev/null)
	[ -n "$w" ] && [ -f "$R$w" ] && say waitfor "$w"
fi

# engine prints what one container engine runs: whether it answers, and
# every container with what its own stop and restart need. The fields of a
# container line: engine, id, name, state, restart policy, stop timeout
# (empty for the engine's default), image, compose project, and the ports it
# publishes on the host, as HOSTPORT:PORT/PROTO, each followed by a comma.
engine() {
	name=$1
	have "$name" || return 0
	if ! limited 15 "$name" info >/dev/null 2>&1; then
		say "$name" down
		return 0
	fi
	say "$name" up
	if [ "$name" = docker ]; then
		say docker_live_restore "$(limited 15 docker info --format '{{.LiveRestoreEnabled}}' 2>/dev/null)"
	fi
	ids=$(limited 15 "$name" ps -aq --no-trunc 2>/dev/null)
	[ -n "$ids" ] || return 0
	# One inspect for all: the format puts a tab between the fields. Docker
	# reads the format against the JSON, where a stop timeout never set is
	# no key at all and naming it fails the whole inspect: index gives an
	# empty value instead. Podman reads it against its own types, where the
	# field is always there and index would fail.
	stop='{{index .Config "StopTimeout"}}'
	[ "$name" = podman ] && stop='{{.Config.StopTimeout}}'
	# The Compose project is a label, free text: quoted as Go quotes it, a
	# tab or a line break in it cannot move the fields after it or split
	# the line. One not set prints nothing.
	project='{{if .Config.Labels}}{{with index .Config.Labels "com.docker.compose.project"}}{{printf "%q" .}}{{end}}{{end}}'
	# Whether its engine removes it as it stops (run with --rm), last: as
	# with the stop timeout, Docker's JSON may lack the key, Podman's types
	# always have it.
	removed='{{index .HostConfig "AutoRemove"}}'
	[ "$name" = podman ] && removed='{{.HostConfig.AutoRemove}}'
	# The $ in the format is the template's own, not the shell's.
	# shellcheck disable=SC2086,SC2016
	limited 30 "$name" inspect --format \
		'{{.Id}}	{{.Name}}	{{.State.Status}}	{{.HostConfig.RestartPolicy.Name}}	'"$stop"'	{{.Config.Image}}	'"$project"'	{{range $p, $b := .HostConfig.PortBindings}}{{range $b}}{{.HostPort}}:{{$p}},{{end}}{{end}}	'"$removed" \
		$ids 2>/dev/null | while IFS= read -r line; do
		[ -n "$line" ] && printf 'container=%s\t%s\n' "$name" "$line"
	done
}
engine docker
engine podman

# What wsl-spawner keeps in this distribution, when it was set up here: the
# script that starts with it and its version, the hooks to run before a
# stop, the starts it saw, the last stop it made and the containers to
# bring back.
say start_script "$(first "$R/usr/local/lib/wsl-spawner/start.sh")"
# A hook's name as a line can hold it, as the stop says it: a tab or a line
# break in it becomes a question mark.
tab=$(printf '\t')
nl='
'
plain() {
	p_in=$1
	case "$p_in" in *"$tab"* | *"$nl"*) ;; *)
		printf '%s' "$p_in"
		return
		;;
	esac
	p_out=""
	while [ -n "$p_in" ]; do
		p_c=${p_in%"${p_in#?}"}
		p_in=${p_in#?}
		case "$p_c" in "$tab" | "$nl") p_c='?' ;; esac
		p_out=$p_out$p_c
	done
	printf '%s' "$p_out"
}
if [ -d "$R/etc/wsl-spawner/pre-stop.d" ]; then
	for h in "$R/etc/wsl-spawner/pre-stop.d"/*; do
		[ -f "$h" ] && [ -x "$h" ] && say hook "$(plain "${h##*/}")"
	done
fi
# trimmed is a line without the blanks around it: spaces, tabs, and the
# carriage return an editor on Windows ends each line with.
trimmed() {
	t=$1
	while :; do case "$t" in [[:space:]]*) t=${t#?} ;; *) break ;; esac; done
	while :; do case "$t" in *[[:space:]]) t=${t%?} ;; *) break ;; esac; done
	printf '%s' "$t"
}
# The units to stop before the containers, one name a line in stop-units,
# each read as the stop reads it.
if [ -r "$R/etc/wsl-spawner/stop-units" ]; then
	while IFS= read -r u || [ -n "$u" ]; do
		# The name alone, as systemd knows it.
		u=$(trimmed "$u")
		case "$u" in '' | '#'*) continue ;; esac
		say stop_unit "$u"
	done <"$R/etc/wsl-spawner/stop-units"
fi
# lines prints each line of a file as KEY=line.
lines() { [ -r "$2" ] || return 0; while IFS= read -r l || [ -n "$l" ]; do say "$1" "$l"; done <"$2"; }
lines run "$R/var/lib/wsl-spawner/runs"
lines stop "$R/var/lib/wsl-spawner/last-stop"
lines bring "$R/var/lib/wsl-spawner/resume"
lines resumed "$R/var/lib/wsl-spawner/resumed"
say end 1
