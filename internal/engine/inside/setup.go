package inside

import (
	_ "embed"
	"fmt"
	"hash/crc32"
	"strings"
)

//go:embed start.sh
var startScript string

// StartScriptVersion is the version the start script's first line names:
// a set-up with an older one is set up again when it is changed.
const StartScriptVersion = "1"

// The files wsl-spawner puts in a distribution it is set up in, where the
// probe and the stop look for them.
const (
	StartScriptPath = "/usr/local/lib/wsl-spawner/start.sh"
	StartUnitPath   = "/etc/systemd/system/" + StartUnit
	KeepAlivePath   = "/etc/systemd/system/" + KeepAliveUnit
)

// startUnit runs the start script at each start, after the container
// engines, so it finds them answering: Docker's own unit, snap's, and
// Podman's.
const startUnit = `# Put here by wsl-spawner: notes each start of this distribution and starts
# again the containers its last stop noted.
#   wsl-spawner set NAME --managed=false   takes it out.
[Unit]
Description=wsl-spawner: note this start, bring back what ran
After=docker.service snap.docker.dockerd.service podman.service podman-restart.service

[Service]
Type=oneshot
ExecStart=/bin/sh ` + StartScriptPath + `
TimeoutStartSec=300

[Install]
WantedBy=multi-user.target
`

// keepAliveUnit holds a Windows program that waits, WAITFOR with the
// signal SIGNAL, which nothing sends: WSL keeps a distribution up while a
// program of its runs, and ends the program with it.
const keepAliveUnit = `# Put here by wsl-spawner: keeps this distribution running with nothing open
# in it, by holding a Windows program that waits; WSL keeps a distribution
# up while one of its programs runs, and ends that program with it.
#   wsl-spawner set NAME --keep=false   takes it out.
[Unit]
Description=wsl-spawner: keep this distribution running

[Service]
ExecStart=WAITFOR SIGNAL
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

// Signal is the name the keep-running service of the distribution named
// distro waits for: waitfor.exe takes letters and digits, and two waiting
// for one name would fail, so the name is the distribution's letters and
// digits with a checksum of the whole name, which tells apart names that
// differ only in other characters.
func Signal(distro string) string {
	var b strings.Builder
	for _, r := range distro {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return fmt.Sprintf("WslSpawnerKeep%s%08x", b.String(), crc32.ChecksumIEEE([]byte(distro)))
}

// quote is s as one word for sh, inside single quotes, which take
// everything as it is but a single quote.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// SetupScript sets wsl-spawner up in a distribution, as root: the start
// script and its service, and, when keep, the keep-running service waiting
// for signal. It needs systemd, and keep needs Windows programs to run; it
// says which is missing with error=systemd or error=interop and exit code 3,
// before writing anything. The start is noted at once, as the start script
// would have at boot, so the next stop's record names a start a look knows.
// It ends with ok=1. As the other scripts, it puts R in front of every path
// it writes, for the tests; R is empty in a distribution.
func SetupScript(keep bool, signal string) string {
	var b strings.Builder
	b.WriteString(prelude + `[ -d "$R/run/systemd/system" ] && command -v systemctl >/dev/null 2>&1 || { echo error=systemd; exit 3; }
`)
	if keep {
		// Where this distribution mounts Windows' drive, and whether its
		// programs run here at all.
		b.WriteString(`waitfor=$(wslpath -u 'C:\Windows\System32\waitfor.exe' 2>/dev/null)
on=0
for f in "$R/proc/sys/fs/binfmt_misc/WSLInterop" "$R/proc/sys/fs/binfmt_misc/WSLInterop-late"; do
	[ -r "$f" ] && { IFS= read -r l <"$f"; [ "$l" = enabled ] && on=1; }
done
[ "$on" = 1 ] && [ -n "$waitfor" ] && [ -f "$R$waitfor" ] || { echo error=interop; exit 3; }
`)
	}
	b.WriteString(`set -e
mkdir -p "$R/usr/local/lib/wsl-spawner" "$R/etc/wsl-spawner/pre-stop.d" "$R/var/lib/wsl-spawner" "$R/etc/systemd/system"
printf '%s' ` + quote(startScript) + ` >"$R` + StartScriptPath + `.new"
mv -f "$R` + StartScriptPath + `.new" "$R` + StartScriptPath + `"
printf '%s' ` + quote(startUnit) + ` >"$R` + StartUnitPath + `"
`)
	if keep {
		unit := strings.ReplaceAll(keepAliveUnit, "SIGNAL", signal)
		before, after, _ := strings.Cut(unit, "WAITFOR")
		// The path is this distribution's own, read above.
		b.WriteString(`printf '%s%s%s' ` + quote(before) + ` "$waitfor" ` + quote(after) + ` >"$R` + KeepAlivePath + `"` + "\n")
	}
	b.WriteString("systemctl daemon-reload\nsystemctl enable --quiet " + StartUnit + "\n")
	if keep {
		b.WriteString("systemctl enable --quiet --now " + KeepAliveUnit + "\n")
	} else {
		b.WriteString("systemctl disable --quiet --now " + KeepAliveUnit + " 2>/dev/null || :\nrm -f \"$R" + KeepAlivePath + "\"\nsystemctl daemon-reload\n")
	}
	// The start script, run as at boot. R is handed over by name: a test's
	// R that did not reach it would have it write into the machine's own
	// /var/lib.
	b.WriteString("R=\"$R\" /bin/sh \"$R" + StartScriptPath + "\"\necho ok=1\n")
	return b.String()
}

// RemoveScript takes out everything wsl-spawner put in a distribution, as
// root: its two services, the start script and what it kept. The person's
// own hooks and stop-units stay, with their folders. It ends with ok=1.
func RemoveScript() string {
	return prelude + `if [ -d "$R/run/systemd/system" ] && command -v systemctl >/dev/null 2>&1; then
	systemctl disable --quiet --now ` + KeepAliveUnit + ` ` + StartUnit + ` 2>/dev/null || :
fi
rm -f "$R` + KeepAlivePath + `" "$R` + StartUnitPath + `"
rm -rf "$R/usr/local/lib/wsl-spawner" "$R/var/lib/wsl-spawner"
rmdir "$R/etc/wsl-spawner/pre-stop.d" "$R/etc/wsl-spawner" 2>/dev/null || :
if [ -d "$R/run/systemd/system" ] && command -v systemctl >/dev/null 2>&1; then
	systemctl daemon-reload
fi
echo ok=1
`
}

// prelude starts the set-up and removal scripts as the others start: R in
// front of every path, and a distribution's own PATH unless a test gave R,
// the same one the others set, snap's /snap/bin included.
const prelude = `R="${R:-}"
[ -n "$R" ] || PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin
export PATH LC_ALL=C
`

// SetupError is what a set-up or removal script said went wrong: "systemd"
// or "interop", or "" when it ended well (ok=1).
func SetupError(out string) (string, bool) {
	for line := range strings.SplitSeq(strings.ReplaceAll(out, "\r", ""), "\n") {
		if v, ok := strings.CutPrefix(line, "error="); ok {
			return v, false
		}
		if line == "ok=1" {
			return "", true
		}
	}
	return "", false
}
