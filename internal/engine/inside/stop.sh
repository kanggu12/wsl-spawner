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
# Hooks and engines run from the root, not from the folder wsl.exe hands
# over, Windows' own: a hook that writes a relative path, or a docker
# compose with no -f, would work there. A test's made-up root stands for
# it. Where that fails, the stop goes on where it is.
cd "$R/" || :
# What this script makes is readable by all and written by root alone,
# whatever umask it is started with.
umask 022
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
# first prints the first line of a file, or nothing. Only a file is read: a
# pipe or a device planted in its place would hold the script for ever.
first() { [ -f "$1" ] && [ -r "$1" ] && { IFS= read -r l <"$1" || [ -n "$l" ]; } && printf '%s' "$l"; }
# now is the kernel's uptime in whole seconds: the steps are timed by it, as
# it never steps back the way a clock set from Windows may.
now() { up=$(first "$R/proc/uptime"); printf '%s' "${up%%.*}"; }
t0=$(now)
# left is how many seconds the steps still have.
left() { echo $((LIMIT - ($(now) - t0))); }
# limited runs a query with a time limit of its own, where timeout is there:
# an engine whose daemon hangs must not hold the stop before it starts.
# What it runs, and timeout's own watcher, which BusyBox's leaves a moment
# behind, are not handed the stop's lock (descriptor 9, below): a program
# left running would hold it after this stop.
limited() { if have timeout; then timeout "$@" 9>&-; else shift; "$@" 9>&-; fi; }
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
# returned at the end. Neither the command nor timeout is handed the
# stop's lock, as with limited: a hook may leave a program running.
within() {
	l=$(left)
	[ "$l" -gt 0 ] || return 124
	have timeout || {
		"$@" 9>&-
		return
	}
	# When it began, by the kernel's clock, to tell how long it went on.
	within_s=$(now)
	if [ -n "$killing" ]; then timeout -k 5 "$l" "$@" 9>&-; else timeout "$l" "$@" 9>&-; fi
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
# unwritten says a file of the stop's own that could not be written (a
# full disk, a file system gone read-only), as a step of its own that
# failed with the code of the write: the stop failed, and the file it was
# to replace stays as it was.
unwritten() {
	printf 'step=record\tdone\t%s\t0\n' "$1"
	note "$1"
}

# One stop at a time in a distribution. A second one while the first is
# at work (a stop asked by hand, then the sign-out hook's) would run every
# hook again and note what comes back from engines on their way down: it
# says end=busy before anything else, and changes nothing; the first goes
# on, and ends the distribution. The lock goes with the stop that holds it,
# whatever ends that stop: flock's with its process; without flock, a
# folder in /run, which each start of the distribution clears, holds the
# pid of the stop that made it, and a later stop takes it over once that
# process is gone. Where neither can be made, stops are not kept apart.
busy() {
	printf 'end=busy\n'
	exit 0
}
locks="$R/run/wsl-spawner"
# held is the lock folder this stop holds, which it lets go of at its end.
held=""
if have mkdir && mkdir -p "$locks" 2>/dev/null; then
	if have flock; then
		# Held on descriptor 9 for as long as this script runs. command keeps
		# a file that cannot be opened from ending the script, as a failure
		# of exec's own would.
		if { command exec 9>>"$locks/stop.lock"; } 2>/dev/null; then
			flock -n 9 || busy
		fi
	elif mkdir "$locks/stop" 2>/dev/null; then
		held="$locks/stop"
	else
		# Another stop's folder: busy while the process it names runs. One
		# that names none, or this very process, is taken over.
		holder=$(first "$locks/stop/pid")
		case "$holder" in
		'' | *[!0-9]* | "$$") ;;
		*) kill -0 "$holder" 2>/dev/null && busy ;;
		esac
		held="$locks/stop"
	fi
	# This stop's pid, in the folder it holds.
	[ -n "$held" ] && printf '%s\n' "$$" 2>/dev/null >"$held/pid"
fi

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
if [ -n "$systemd" ] && [ -f "$R/etc/wsl-spawner/stop-units" ] && [ -r "$R/etc/wsl-spawner/stop-units" ]; then
	while IFS= read -r u || [ -n "$u" ]; do
		# The name alone, as systemd knows it.
		u=$(trimmed "$u")
		case "$u" in '' | '#'*) continue ;; esac
		[ "$(limited "$(upto 5)" systemctl is-active "$u" 2>/dev/null)" = active ] && units="$units $u"
	done <"$R/etc/wsl-spawner/stop-units"
fi
# asleep reports whether Docker's daemon is not running while its socket
# listens, as Docker's packages set it up: asking such a daemon anything
# starts it, and a stop would start it only to stop it again.
asleep() {
	[ "$1" = docker ] && [ -n "$systemd" ] || return 1
	[ "$(limited "$(upto 5)" systemctl is-active docker.socket 2>/dev/null)" = active ] || return 1
	case "$(limited "$(upto 5)" systemctl is-active docker.service 2>/dev/null)" in
	active | activating | reloading) return 1 ;;
	esac
	return 0
}
# own reports whether Docker's daemon is this distribution's, as the probe
# judges it: systemd has its unit, Docker's own or snap's, or a pid file
# names a dockerd that runs. Otherwise a docker command here answers for
# another's, Docker Desktop's through its WSL integration or another
# engine's behind a docker command, or for none: the stop leaves it alone.
own() {
	if [ -n "$systemd" ]; then
		for u in docker.service snap.docker.dockerd.service; do
			[ "$(limited "$(upto 5)" systemctl show -p LoadState --value "$u" 2>/dev/null)" = loaded ] && return 0
		done
	fi
	# A pid file can outlive its daemon, its number given since to another
	# program: only a dockerd counts. Only a number is a pid.
	own_pid=$(first "$R/var/run/docker.pid")
	case "$own_pid" in '' | *[!0-9]*) return 1 ;; esac
	[ "$(first "$R/proc/$own_pid/comm")" = dockerd ]
}
# The container engines that answer, and what runs in each. Their time
# counts as the steps' does, so each query waits the time left at most.
# docker_foreign is set for a docker whose daemon is another's: it is not
# asked, nothing of it is noted or stopped, no step fails for it, and the
# lines of docker an earlier stop noted go.
docker_up=""
docker_foreign=""
if have docker && ! asleep docker; then
	if ! own; then
		docker_foreign=1
	elif limited "$(upto 15)" docker info >/dev/null 2>&1; then
		docker_up=1
		docker_running=$(limited "$(upto 15)" docker ps -q --no-trunc 2>/dev/null)
		docker_lr=$(limited "$(upto 15)" docker info --format '{{.LiveRestoreEnabled}}' 2>/dev/null)
	fi
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
	# In a subshell of its own: a line that cannot be written ends it, and
	# keep returns that write's code, whichever shell runs the end of a
	# pipeline. The shell's own word for a write that failed is not kept:
	# the stop says it as a step.
	# The fields are split on |, which no id, container name, policy name
	# or true and false holds: split on blanks, an empty policy name, as
	# Podman may give, let the next field move into its place.
	(
		limited "$(upto 30)" "$engine" inspect --format "{{.Id}}|{{.Name}}|{{.HostConfig.RestartPolicy.Name}}|$removed" "$@" 2>/dev/null |
			while IFS='|' read -r id name policy removed; do
				# One its engine removes as it stops (run with --rm) is not
				# there to start again.
				[ "$removed" = true ] && continue
				case "$RESUME:$policy" in
				running:* | policy:always | policy:unless-stopped) printf '%s %s %s\n' "$engine" "$id" "${name#/}" || exit ;;
				esac
			done
	) 2>/dev/null >>"$state/resume.new"
}
# The list is made anew beside the one it replaces, and takes its place
# only once every line of it was written: one that cannot be written (a
# full disk, a file system gone read-only) leaves the list there as it
# was. wrote is the code of the write that failed, 0 while none has. The
# file is made by printf, not :, as a shell ends the whole script where :
# cannot make it.
wrote=0
if [ -n "$state" ]; then
	printf '' 2>/dev/null >"$state/resume.new" || wrote=$?
fi
set -f
# shellcheck disable=SC2086
[ -n "$docker_up" ] && [ "$wrote" = 0 ] && { keep docker $docker_running || wrote=$?; }
# shellcheck disable=SC2086
[ -n "$podman_up" ] && [ "$wrote" = 0 ] && { keep podman $podman_running || wrote=$?; }
set +f
# The containers of an engine that did not answer stay to bring back: the
# last start kept them until it answers, and this stop knows nothing new
# of them. Those of a docker whose daemon is another's go, as none of them
# is this distribution's. A line of anything but the two engines goes:
# each first word is run as a command at the next start.
if [ -n "$state" ] && [ "$wrote" = 0 ] && [ "$RESUME" != off ] && [ -f "$state/resume" ] && [ -r "$state/resume" ]; then
	# In a subshell, as keep's lines: a line that cannot be written ends it
	# with that write's code.
	(
		while read -r r_engine r_id r_name; do
			case "$r_engine" in
			docker) [ -z "$docker_up$docker_foreign" ] || continue ;;
			podman) [ -z "$podman_up" ] || continue ;;
			*) continue ;;
			esac
			# An id is hex: a line with anything else is not one of this
			# script's, and does not live on.
			case "$r_id" in '' | *[!0-9a-f]*) continue ;; esac
			printf '%s %s %s\n' "$r_engine" "$r_id" "$r_name" || exit
		done <"$state/resume"
	) 2>/dev/null >>"$state/resume.new" || wrote=$?
fi
if [ -n "$state" ]; then
	# Written whole, it takes the old one's place; otherwise the old one
	# stays, and the stop failed.
	if [ "$wrote" = 0 ]; then
		mv -f "$state/resume.new" "$state/resume" || unwritten $?
	else
		unwritten "$wrote"
	fi
fi

# 1. The hooks, each with what time is left, found as the plan found them.
ran=""
for h in "$hookdir"/*; do
	# What is not a program in the folder is not a hook.
	runnable "$h" || continue
	ran=1
	n=$(plain "${h##*/}")
	printf 'hook=%s\tstart\n' "$n"
	s=$(now)
	# within hands it no lock: a program the hook leaves running holds none.
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
	# Made beside the list as the list was: a line that cannot be written
	# ends the subshell with that write's code, and the list stays as it is.
	(
		while read -r r_engine r_id r_name; do
			if [ "$r_engine" = "$1" ]; then
				case "$nl$g_all$nl" in *"$nl$r_id$nl"*) ;; *) continue ;; esac
			fi
			printf '%s %s %s\n' "$r_engine" "$r_id" "$r_name" || exit
		done <"$state/resume"
	) 2>/dev/null >"$state/resume.new" || {
		unwritten $?
		return
	}
	mv -f "$state/resume.new" "$state/resume" || unwritten $?
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
		# The socket first, so nothing starts the daemon again as it stops:
		# only where it is there, as systemctl fails a stop that names a unit
		# it does not have (5), and the step with it. A docker.service
		# written by hand, or by an older package, may have none.
		sockets=""
		[ "$unit" = docker.service ] &&
			[ "$(limited "$(upto 5)" systemctl show -p LoadState --value docker.socket 2>/dev/null)" = loaded ] &&
			sockets=docker.socket
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
	# Made beside the record it replaces, and put in its place once written
	# whole: one that cannot be written leaves the last record as it was,
	# and the stop failed.
	if printf 'run=%s\nat=%s\ntook=%s\nresult=%s\nby=%s\n' "$run" $((NOW + took)) "$took" "$result" "$BY" 2>/dev/null >"$state/last-stop.new"; then
		mv -f "$state/last-stop.new" "$state/last-stop" || unwritten $?
	else
		unwritten $?
	fi
	have sync && sync
fi
# The lock folder is let go of: with no pid in it, the next stop takes it.
# flock's lock goes as the script ends.
[ -n "$held" ] && printf '' 2>/dev/null >"$held/pid"
printf 'end=%s\n' "$result"
