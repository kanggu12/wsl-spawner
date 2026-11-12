# wsl-spawner start 3
# Put here by wsl-spawner, and run by wsl-spawner.service at each start of
# this distribution. It notes the start, so the next look can tell a stop of
# wsl-spawner's from one that cut the services short, and starts again the
# containers the last stop noted that are not running already (those with a
# restart policy their engine brought back itself).
#   wsl-spawner set NAME --managed=false   takes it out.
# Only the shell picks text apart: a small distribution may have no text
# tools.
R="${R:-}"
# The engines start from the root, as the service starts this script, and
# never from a folder of whoever ran it by hand; a test's made-up root
# stands for it. Where that fails, the script goes on where it is.
cd "$R/" || :
# What this script makes is readable by all and written by root alone, as
# the service that runs it, whatever umask it is started with.
umask 022
# Tools are found where a distribution keeps them, snap's Docker in
# /snap/bin included, as the stop that noted the containers found them.
[ -n "$R" ] || PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin
export PATH LC_ALL=C
state="$R/var/lib/wsl-spawner"
mkdir -p "$state"

# first prints the first line of a file, or nothing. Only a file is read: a
# pipe or a device planted in its place would hold the script for ever.
first() { [ -f "$1" ] && [ -r "$1" ] && { IFS= read -r l <"$1" || [ -n "$l" ]; } && printf '%s' "$l"; }

# One run at a time. Two at once (the service at a boot a set-up caused,
# and a run by hand or the set-up's own) would each start every container
# on the list and note it, and note the start twice. A second run leaves it
# all to the first, and says so in its log, for systemctl status. The lock
# is the stop's kind, and goes with the run that holds it, whatever ends
# that run: flock's with its process; without flock, a folder in /run,
# which each start of the distribution clears, holds the run's pid, and a
# later run takes it over once that process is gone or names none. Where
# neither can be made, runs are not kept apart. No command this script
# runs is handed the lock's descriptor, 9: Podman's start leaves a program
# watching the container for as long as the container runs.
another() {
	echo 'wsl-spawner: another run of this script is bringing back what the last stop noted' >&2
	exit 0
}
locks="$R/run/wsl-spawner"
# held is the lock folder this run holds, which it lets go of at its end.
held=""
if mkdir -p "$locks" 2>/dev/null; then
	if command -v flock >/dev/null 2>&1; then
		# Held on descriptor 9 for as long as this script runs. command keeps
		# a file that cannot be opened from ending the script, as a failure
		# of exec's own would.
		if { command exec 9>>"$locks/start.lock"; } 2>/dev/null; then
			flock -n 9 || another
		fi
	elif mkdir "$locks/start" 2>/dev/null; then
		held="$locks/start"
	else
		# Another run's folder: busy while the process it names runs. One that
		# names none, or this very process, is taken over.
		holder=$(first "$locks/start/pid")
		case "$holder" in
		'' | *[!0-9]* | "$$") ;;
		*) kill -0 "$holder" 2>/dev/null && another ;;
		esac
		held="$locks/start"
	fi
	# This run's pid, in the folder it holds.
	[ -n "$held" ] && printf '%s\n' "$$" 2>/dev/null >"$held/pid"
fi

# This start: the kernel's boot and when this distribution's init started,
# as the probe and the stop name it.
boot=$(first "$R/proc/sys/kernel/random/boot_id")
stat1=$(first "$R/proc/1/stat")
set -f
# shellcheck disable=SC2086
set -- ${stat1##*) }
set +f
run="$boot@${20:-}"
now=$(date +%s 2>/dev/null)

# The starts noted, the last 20 kept: those before, then this one, unless it
# is noted already (the script runs again when the service is restarted).
runs="$state/runs"
total=0
last=""
if [ -f "$runs" ] && [ -r "$runs" ]; then
	while IFS= read -r l || [ -n "$l" ]; do
		total=$((total + 1))
		last=$l
	done <"$runs"
fi
if [ "${last#* }" != "$run" ]; then
	skip=$((total - 19))
	i=0
	if [ -f "$runs" ] && [ -r "$runs" ]; then
		while IFS= read -r l || [ -n "$l" ]; do
			i=$((i + 1))
			[ "$i" -gt "$skip" ] && printf '%s\n' "$l"
		done <"$runs"
	fi >"$runs.new"
	printf '%s %s\n' "${now:-0}" "$run" >>"$runs.new"
	mv -f "$runs.new" "$runs"
fi

# start_one starts one container. Podman's command starts the process that
# watches the container itself: started from this service as it is, systemd
# ends it with every process left in the service once the service is done,
# and the container with it. In a scope of its own it outlives the service.
# Docker's daemon runs its containers apart already.
start_one() {
	# Neither is handed the lock: what watches the container would hold it.
	if [ "$1" = podman ] && command -v systemd-run >/dev/null 2>&1; then
		systemd-run --scope --quiet --collect -- podman start "$2" 9>&-
	else
		"$1" start "$2" 9>&-
	fi
}

# The engines asked already: those whose daemon answers, and those whose
# does not, each name with a space on both sides.
answering=" "
silent=" "
# coming reports whether systemd runs the unit of an engine's daemon, or is
# starting it: Docker's own unit or snap's. Podman has no daemon to wait for.
coming() {
	[ "$1" = docker ] && command -v systemctl >/dev/null 2>&1 || return 1
	for u in docker.service snap.docker.dockerd.service; do
		case "$(systemctl is-active "$u" 2>/dev/null </dev/null 9>&-)" in active | activating) return 0 ;; esac
	done
	return 1
}
# answers reports whether an engine's daemon answers. Each is asked once a
# run. One that systemd still starts is waited for, a minute at most: snap's
# Docker answers a while after its unit has started, and this service may
# run in between. One that is not starting is not waited for.
answers() {
	case "$answering" in *" $1 "*) return 0 ;; esac
	case "$silent" in *" $1 "*) return 1 ;; esac
	tries=0
	# Nothing it reads comes from the list of containers this loop reads,
	# and it is handed no lock.
	until "$1" info >/dev/null 2>&1 </dev/null 9>&-; do
		tries=$((tries + 1))
		# Without sleep there is no waiting: one question is all.
		if [ "$tries" -ge 60 ] || ! coming "$1" || ! command -v sleep >/dev/null 2>&1; then
			silent="$silent$1 "
			return 1
		fi
		sleep 1
	done
	answering="$answering$1 "
}

# The containers the last stop noted: each one still there and not running
# is started, and what became of each is kept for a look. Those of an
# engine whose daemon does not answer are not known to be anything yet:
# they stay to bring back, for when this service runs again.
if [ -f "$state/resume" ] && [ -r "$state/resume" ]; then
	# The list as this start found it, under a second name: a stop that
	# runs meanwhile writes a new list in its place, which this start then
	# leaves as it is.
	list="$state/resume"
	rm -f "$state/resume.taken"
	# Only where the shell tells two names of one file apart: every sh a
	# distribution comes with does (dash, bash, BusyBox's, ksh), and one
	# that does not leaves the list as it was always read.
	# shellcheck disable=SC3013
	ln "$state/resume" "$state/resume.taken" 2>/dev/null &&
		[ "$state/resume.taken" -ef "$state/resume" ] 2>/dev/null && list="$state/resume.taken"
	printf 'at %s\n' "${now:-0}" >"$state/resumed.new"
	# Made by printf, not :, as a shell ends the whole script where : cannot
	# make a file: a note that cannot be written does not keep the
	# containers from coming back.
	printf '' >"$state/resume.left"
	while read -r engine id name; do
		[ -n "$id" ] || continue
		# Each first word is run as a command: only the two engines are. The
		# second goes to that command as an argument: only an id, which is
		# hex and so never an option.
		case "$engine" in docker | podman) ;; *) continue ;; esac
		case "$id" in *[!0-9a-f]*) continue ;; esac
		if ! command -v "$engine" >/dev/null 2>&1; then
			what=gone
		elif ! answers "$engine"; then
			# Not known to be anything yet: kept on the list, and said of
			# nowhere else.
			printf '%s %s %s\n' "$engine" "$id" "$name" >>"$state/resume.left"
			continue
		else
			case "$("$engine" inspect --format '{{.State.Running}}' "$id" 2>/dev/null 9>&-)" in
			true) what=running ;;
			false) if start_one "$engine" "$id" >/dev/null 2>&1; then what=started; else what=failed; fi ;;
			*) what=gone ;;
			esac
		fi
		printf '%s %s %s %s\n' "$engine" "$id" "$name" "$what" >>"$state/resumed.new"
	done <"$list"
	# A container started is looked at again a moment later: one that ended
	# at once did not come back, whatever its start said.
	if command -v sleep >/dev/null 2>&1; then
		sleep 2
		while read -r engine id name what; do
			if [ "$what" = started ] && [ "$("$engine" inspect --format '{{.State.Running}}' "$id" 2>/dev/null 9>&-)" != true ]; then
				what=exited
			fi
			if [ "$engine" = at ]; then printf 'at %s\n' "$id"; else printf '%s %s %s %s\n' "$engine" "$id" "$name" "$what"; fi
		done <"$state/resumed.new" >"$state/resumed.checked"
		mv -f "$state/resumed.checked" "$state/resumed.new"
	fi
	mv -f "$state/resumed.new" "$state/resumed"
	# A stop that ran meanwhile wrote a list of its own, from the one this
	# start read and what ran as it began: that list stands. Otherwise what
	# waits for its engine stays on the list, and its log says why, for
	# systemctl status; the rest of the list is done with.
	# shellcheck disable=SC3013
	if [ "$list" != "$state/resume" ] && ! [ "$state/resume" -ef "$list" ]; then
		rm -f "$state/resume.left"
	elif [ -s "$state/resume.left" ]; then
		mv -f "$state/resume.left" "$state/resume"
		printf 'wsl-spawner: no answer from%s: its containers wait to come back when this service runs again\n' "${silent% }" >&2
	else
		rm -f "$state/resume" "$state/resume.left"
	fi
	rm -f "$state/resume.taken"
fi
# The lock folder is let go of: with no pid in it, the next run takes it.
# flock's lock goes as the script ends.
[ -n "$held" ] && printf '' 2>/dev/null >"$held/pid"
exit 0
