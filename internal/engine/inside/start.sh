# wsl-spawner start 1
# Put here by wsl-spawner, and run by wsl-spawner.service at each start of
# this distribution. It notes the start, so the next look can tell a stop of
# wsl-spawner's from one that cut the services short, and starts again the
# containers the last stop noted that are not running already (those with a
# restart policy their engine brought back itself).
#   wsl-spawner set NAME --managed=false   takes it out.
# Only the shell picks text apart: a small distribution may have no text
# tools.
R="${R:-}"
# Tools are found where a distribution keeps them, snap's Docker in
# /snap/bin included, as the stop that noted the containers found them.
[ -n "$R" ] || PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin
export PATH LC_ALL=C
state="$R/var/lib/wsl-spawner"
mkdir -p "$state"

# first prints the first line of a file, or nothing.
first() { [ -r "$1" ] && { IFS= read -r l <"$1" || [ -n "$l" ]; } && printf '%s' "$l"; }

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
if [ -r "$runs" ]; then
	while IFS= read -r l || [ -n "$l" ]; do
		total=$((total + 1))
		last=$l
	done <"$runs"
fi
if [ "${last#* }" != "$run" ]; then
	skip=$((total - 19))
	i=0
	if [ -r "$runs" ]; then
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
	if [ "$1" = podman ] && command -v systemd-run >/dev/null 2>&1; then
		systemd-run --scope --quiet --collect -- podman start "$2"
	else
		"$1" start "$2"
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
		case "$(systemctl is-active "$u" 2>/dev/null </dev/null)" in active | activating) return 0 ;; esac
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
	# Nothing it reads comes from the list of containers this loop reads.
	until "$1" info >/dev/null 2>&1 </dev/null; do
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
if [ -r "$state/resume" ]; then
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
	: >"$state/resume.left"
	while read -r engine id name; do
		[ -n "$id" ] || continue
		# Each first word is run as a command: only the two engines are.
		case "$engine" in docker | podman) ;; *) continue ;; esac
		if ! command -v "$engine" >/dev/null 2>&1; then
			what=gone
		elif ! answers "$engine"; then
			# Not known to be anything yet: kept on the list, and said of
			# nowhere else.
			printf '%s %s %s\n' "$engine" "$id" "$name" >>"$state/resume.left"
			continue
		else
			case "$("$engine" inspect --format '{{.State.Running}}' "$id" 2>/dev/null)" in
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
			if [ "$what" = started ] && [ "$("$engine" inspect --format '{{.State.Running}}' "$id" 2>/dev/null)" != true ]; then
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
exit 0
