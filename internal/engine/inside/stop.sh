# wsl-spawner stop: brings this distribution's services and containers down,
# each with the time it asks for, before WSL ends what is left with the few
# seconds it gives. Run as root by wsl-spawner, which sets, in front of this:
#   LIMIT   the seconds every step together may take
#   RESUME  which running containers come back at the next start:
#           running (all), policy (those with a restart policy), off (none)
#   BY      who asked: hand (wsl-spawner stop) or signout (Windows signing out)
#   NOW     the Unix time on Windows as the stop starts, for the record
# Each line it prints is KEY=fields split by tabs, as the probe's are:
#   plan    what it is about to do, one line per step
#   step    NAME, start or done, then the exit code and the seconds it took
#   hook    one program of /etc/wsl-spawner/pre-stop.d, as a step
#   end     clean, limit (a step ran out of the time left) or failed
# Only the shell picks text apart: a small distribution may have no text
# tools.
R="${R:-}"
# Tools are found where a distribution keeps them, snap's Docker in
# /snap/bin included, as the probe finds them.
[ -n "$R" ] || PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin
export PATH LC_ALL=C
LIMIT=${LIMIT:-300}
RESUME=${RESUME:-running}
BY=${BY:-hand}
NOW=${NOW:-0}

# have reports whether a tool is on PATH.
have() { command -v "$1" >/dev/null 2>&1; }
# first prints the first line of a file, or nothing.
first() { [ -r "$1" ] && { IFS= read -r l <"$1" || [ -n "$l" ]; } && printf '%s' "$l"; }
# now is the kernel's uptime in whole seconds: the steps are timed by it, as
# it never steps back the way a clock set from Windows may.
now() { up=$(first "$R/proc/uptime"); printf '%s' "${up%%.*}"; }
t0=$(now)
# left is how many seconds the steps still have.
left() { echo $((LIMIT - ($(now) - t0))); }
# limited runs a query with a time limit of its own, where timeout is there:
# an engine whose daemon hangs must not hold the stop before it starts.
limited() { if have timeout; then timeout "$@"; else shift; "$@"; fi; }
# upto prints how long a query may wait: its own limit, or the time left
# when that is less. A second at least, as timeout takes 0 for no limit.
upto() {
	upto_l=$(left)
	[ "$upto_l" -lt "$1" ] || upto_l=$1
	[ "$upto_l" -gt 0 ] || upto_l=1
	echo "$upto_l"
}
# killing is set where timeout takes -k: a command that does not end at the
# TERM it is sent is killed 5 seconds later, so no step runs on without end
# past its time. BusyBox's timeout of old takes no -k; only the shell reads
# what it says it takes.
killing=""
if have timeout; then
	case "$(timeout --help 2>&1)" in *-k*) killing=1 ;; esac
fi
# within runs a command with what time is left, where timeout is there, and
# returns 124 at once when none is left. A command timeout ended returns
# 124 too: coreutils' timeout says that itself, BusyBox's returns what the
# signal it sent made of the command (143 for TERM), so a command that
# failed once no time was left ran out of it, whichever said so. One that
# went on past its time, TERM or not, ran out of it as well, whatever it
# returned at the end.
within() {
	l=$(left)
	[ "$l" -gt 0 ] || return 124
	have timeout || {
		"$@"
		return
	}
	# When it began, by the kernel's clock, to tell how long it went on.
	within_s=$(now)
	if [ -n "$killing" ]; then timeout -k 5 "$l" "$@"; else timeout "$l" "$@"; fi
	# A name of its own: the steps keep their codes in rc.
	within_rc=$?
	[ "$within_rc" -ne 0 ] && [ "$(left)" -le 0 ] && within_rc=124
	# More whole seconds than it was given: it went on past its time.
	[ $(($(now) - within_s)) -gt "$l" ] && within_rc=124
	return "$within_rc"
}
# result is how the stop ends: a step that ran out of time makes it limit,
# one that failed makes it failed, unless it ran out of time already.
result=clean
note() {
	case "$1" in
	0) ;;
	124 | 137) result=limit ;;
	*) [ "$result" = clean ] && result=failed ;;
	esac
}
# done_step prints a step's end and notes how it went.
done_step() {
	printf 'step=%s\tdone\t%s\t%s\n' "$1" "$2" $(($(now) - $3))
	note "$2"
}

# The state of this run of the distribution: the kernel's boot and when its
# init started, as the probe names them.
boot=$(first "$R/proc/sys/kernel/random/boot_id")
stat1=$(first "$R/proc/1/stat")
set -f
# shellcheck disable=SC2086
set -- ${stat1##*) }
set +f
run="$boot@${20:-}"
# wsl-spawner's own folder here exists only where it was set up: elsewhere
# nothing is written, and nothing comes back at the next start.
state="$R/var/lib/wsl-spawner"
[ -d "$state" ] || state=""
systemd=""
[ -d "$R/run/systemd/system" ] && have systemctl && systemd=1

# The hooks: the programs in pre-stop.d, in the order of their names. What
# they print is not kept: a hook that wants a log writes its own. They are
# found by the folder's pattern each time, never kept in a list: a name may
# hold spaces or a pattern's characters, which a list split again would
# take apart.
hookdir="$R/etc/wsl-spawner/pre-stop.d"
# runnable reports whether a file of that folder is a hook: a file that can
# run. With no folder, the pattern itself comes back, which is no file.
runnable() { [ -f "$1" ] && [ -x "$1" ]; }
# tab and nl end a line's fields and its lines.
tab=$(printf '\t')
nl='
'
# plain is a hook's name as a line can hold it: a tab or a line break in the
# name would end its field or its line, so each becomes a question mark.
# The hook still runs by its own name.
plain() {
	p_in=$1
	case "$p_in" in *"$tab"* | *"$nl"*) ;; *)
		printf '%s' "$p_in"
		return
		;;
	esac
	p_out=""
	while [ -n "$p_in" ]; do
		# The first character, and the rest after it.
		p_c=${p_in%"${p_in#?}"}
		p_in=${p_in#?}
		case "$p_c" in "$tab" | "$nl") p_c='?' ;; esac
		p_out=$p_out$p_c
	done
	printf '%s' "$p_out"
}
# trimmed is a line without the blanks around it: spaces, tabs, and the
# carriage return an editor on Windows ends each line with.
trimmed() {
	t=$1
	while :; do case "$t" in [[:space:]]*) t=${t#?} ;; *) break ;; esac; done
	while :; do case "$t" in *[[:space:]]) t=${t%?} ;; *) break ;; esac; done
	printf '%s' "$t"
}
# The units to stop first, one name a line in stop-units, with what time
# each asks for in its own TimeoutStopSec.
units=""
if [ -n "$systemd" ] && [ -r "$R/etc/wsl-spawner/stop-units" ]; then
	while IFS= read -r u || [ -n "$u" ]; do
		# The name alone, as systemd knows it.
		u=$(trimmed "$u")
		case "$u" in '' | '#'*) continue ;; esac
		[ "$(limited "$(upto 5)" systemctl is-active "$u" 2>/dev/null)" = active ] && units="$units $u"
	done <"$R/etc/wsl-spawner/stop-units"
fi
# The container engines that answer, and what runs in each. Their time
# counts as the steps' does, so each query waits the time left at most.
docker_up=""
if have docker && limited "$(upto 15)" docker info >/dev/null 2>&1; then
	docker_up=1
	docker_running=$(limited "$(upto 15)" docker ps -q --no-trunc 2>/dev/null)
	docker_lr=$(limited "$(upto 15)" docker info --format '{{.LiveRestoreEnabled}}' 2>/dev/null)
fi
podman_up=""
if have podman && limited "$(upto 15)" podman info >/dev/null 2>&1; then
	podman_up=1
	podman_running=$(limited "$(upto 15)" podman ps -q --no-trunc 2>/dev/null)
fi
# count prints how many words a list holds.
# shellcheck disable=SC2086
count() { set -f; set -- $1; set +f; echo $#; }

# The plan, before anything stops: the screen shows it as the steps to come.
# Each hook by its own name, the folder's path cut off.
for h in "$hookdir"/*; do runnable "$h" && printf 'plan=hook\t%s\n' "$(plain "${h##*/}")"; done
[ -n "$units" ] && printf 'plan=units\t%s\n' "$(count "$units")"
[ -n "$docker_up" ] && printf 'plan=docker\t%s\n' "$(count "$docker_running")"
[ -n "$podman_up" ] && printf 'plan=podman\t%s\n' "$(count "$podman_running")"
printf 'plan=sync\t\n'

# keep notes the containers to bring back at the next start, from what ran
# as the stop began: every one, or those with a restart policy. The list is
# written before anything stops, the hooks included, while there is time
# for it, so a stop cut short still brings them back.
keep() {
	engine=$1
	shift
	[ -n "$state" ] && [ "$RESUME" != off ] && [ $# -gt 0 ] || return 0
	# Whether its engine removes it as it stops: Docker's JSON may lack the
	# key, Podman's types always have it, as the probe reads it.
	removed='{{index .HostConfig "AutoRemove"}}'
	[ "$engine" = podman ] && removed='{{.HostConfig.AutoRemove}}'
	limited "$(upto 30)" "$engine" inspect --format "{{.Id}} {{.Name}} {{.HostConfig.RestartPolicy.Name}} $removed" "$@" 2>/dev/null |
		while read -r id name policy removed; do
			# One its engine removes as it stops (run with --rm) is not
			# there to start again.
			[ "$removed" = true ] && continue
			case "$RESUME:$policy" in
			running:* | policy:always | policy:unless-stopped) printf '%s %s %s\n' "$engine" "$id" "${name#/}" ;;
			esac
		done >>"$state/resume.new"
}
[ -n "$state" ] && : >"$state/resume.new"
set -f
# shellcheck disable=SC2086
[ -n "$docker_up" ] && keep docker $docker_running
# shellcheck disable=SC2086
[ -n "$podman_up" ] && keep podman $podman_running
set +f
# The containers of an engine that did not answer stay to bring back: the
# last start kept them until it answers, and this stop knows nothing new
# of them. A line of anything but the two engines goes: each first word is
# run as a command at the next start.
if [ -n "$state" ] && [ "$RESUME" != off ] && [ -r "$state/resume" ]; then
	while read -r r_engine r_id r_name; do
		case "$r_engine" in
		docker) [ -z "$docker_up" ] || continue ;;
		podman) [ -z "$podman_up" ] || continue ;;
		*) continue ;;
		esac
		[ -n "$r_id" ] && printf '%s %s %s\n' "$r_engine" "$r_id" "$r_name"
	done <"$state/resume" >>"$state/resume.new"
fi
[ -n "$state" ] && mv -f "$state/resume.new" "$state/resume"

# 1. The hooks, each with what time is left, found as the plan found them.
ran=""
for h in "$hookdir"/*; do
	# What is not a program in the folder is not a hook.
	runnable "$h" || continue
	ran=1
	n=$(plain "${h##*/}")
	printf 'hook=%s\tstart\n' "$n"
	s=$(now)
	within "$h" >/dev/null 2>&1
	rc=$?
	printf 'hook=%s\tdone\t%s\t%s\n' "$n" "$rc" $(($(now) - s))
	note "$rc"
done

# 2. The units, in one call: systemd stops them in the order their
# dependencies say, each with its own time.
if [ -n "$units" ]; then
	printf 'step=units\tstart\t%s\n' "$(count "$units")"
	s=$(now)
	# Split on blanks alone: a name is never matched against the files of
	# the folder the program ran from.
	set -f
	# shellcheck disable=SC2086
	within systemctl stop $units >/dev/null 2>&1
	rc=$?
	set +f
	done_step units "$rc" "$s"
fi

# gone drops from the list to bring back the containers of an engine that
# are not there any more: a hook or a unit may remove them as it stops, as
# docker compose down does, and the next start would find them gone. The
# list stays as it is when the engine does not say which it has.
gone() {
	g_all=$(limited "$(upto 10)" "$1" ps -aq --no-trunc 2>/dev/null) || return 0
	while read -r r_engine r_id r_name; do
		if [ "$r_engine" = "$1" ]; then
			case "$nl$g_all$nl" in *"$nl$r_id$nl"*) ;; *) continue ;; esac
		fi
		printf '%s %s %s\n' "$r_engine" "$r_id" "$r_name"
	done <"$state/resume" >"$state/resume.new"
	mv -f "$state/resume.new" "$state/resume"
}
if [ -n "$ran$units" ] && [ -n "$state" ] && [ -s "$state/resume" ]; then
	[ -n "$docker_up" ] && gone docker
	[ -n "$podman_up" ] && gone podman
fi

# 3. Docker. Its daemon stops every container itself, each with its own
# stop timeout, and leaves them as they were: what has a restart policy
# comes back with it. systemd would end the daemon at its own stop timeout,
# so for this stop it gets all the time left. With live-restore the daemon
# leaves its containers running, so they are stopped first, each with its
# own timeout.
if [ -n "$docker_up" ]; then
	printf 'step=docker\tstart\t%s\n' "$(count "$docker_running")"
	s=$(now)
	rc=0
	if [ "$docker_lr" = true ] && [ -n "$docker_running" ]; then
		set -f
		# shellcheck disable=SC2086
		within docker stop $docker_running >/dev/null 2>&1 || rc=$?
		set +f
	fi
	unit=""
	if [ -n "$systemd" ]; then
		for u in docker.service snap.docker.dockerd.service; do
			[ "$(limited "$(upto 5)" systemctl is-active "$u" 2>/dev/null)" = active ] && unit=$u && break
		done
	fi
	# The time the daemon gets, read once for its drop-in.
	give=$(left)
	if [ -n "$unit" ] && [ "$give" -le 0 ]; then
		# No time left: systemd reads TimeoutStopSec=0 as no limit at all,
		# and the drop-in would stay for the rest of this start. Neither it
		# nor the stop is written; the step ran out of time.
		rc=124
	elif [ -n "$unit" ]; then
		# The drop-in is in /run: it goes with this start of the distribution.
		mkdir -p "$R/run/systemd/system/$unit.d"
		printf '[Service]\nTimeoutStopSec=%s\n' "$give" >"$R/run/systemd/system/$unit.d/zz-wsl-spawner.conf"
		limited "$(upto 10)" systemctl daemon-reload >/dev/null 2>&1
		# The socket first, so nothing starts the daemon again as it stops.
		sockets=""
		[ "$unit" = docker.service ] && sockets=docker.socket
		# shellcheck disable=SC2086
		within systemctl stop $sockets "$unit" >/dev/null 2>&1 || rc=$?
	else
		# No systemd: the daemon is asked to stop, and waited for. A pid file
		# can outlive its daemon, its number given since to another process:
		# only a process named dockerd is asked.
		pid=$(first "$R/var/run/docker.pid")
		name=""
		[ -n "$pid" ] && [ -r "$R/proc/$pid/comm" ] && name=$(first "$R/proc/$pid/comm")
		if [ "$name" = dockerd ] && kill -TERM "$pid" 2>/dev/null; then
			while kill -0 "$pid" 2>/dev/null; do
				[ "$(left)" -gt 0 ] || { rc=124; break; }
				sleep 1
			done
		else
			# No pid to ask it by, or none of a process: the daemon that
			# answered was never asked to stop, and WSL ends it with its
			# containers. The step failed.
			rc=1
		fi
	fi
	done_step docker "$rc" "$s"
fi

# 4. Podman, which has no daemon: its containers are stopped, each with its
# own stop timeout.
if [ -n "$podman_up" ] && [ -n "$podman_running" ]; then
	printf 'step=podman\tstart\t%s\n' "$(count "$podman_running")"
	s=$(now)
	set -f
	# shellcheck disable=SC2086
	within podman stop $podman_running >/dev/null 2>&1
	rc=$?
	set +f
	done_step podman "$rc" "$s"
fi

# 5. What the services wrote, on the disk.
printf 'step=sync\tstart\n'
s=$(now)
[ -n "$systemd" ] && have journalctl && within journalctl --sync >/dev/null 2>&1
have sync && within sync
done_step sync 0 "$s"

# The record of this stop, which the next start reads to tell a stop of
# wsl-spawner's from one that cut the services short.
if [ -n "$state" ]; then
	took=$(($(now) - t0))
	printf 'run=%s\nat=%s\ntook=%s\nresult=%s\nby=%s\n' "$run" $((NOW + took)) "$took" "$result" "$BY" >"$state/last-stop.new"
	mv -f "$state/last-stop.new" "$state/last-stop"
	have sync && sync
fi
printf 'end=%s\n' "$result"
