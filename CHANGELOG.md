# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
versions follow [semantic versioning](https://semver.org/): a release that adds something raises the minor number, one
that only fixes raises the patch number. Each release's section here is its release notes.

## [Unreleased]

### Added

- A start that ended without its time says what ended it, where it can tell: Windows did not shut down cleanly, WSL was
  updated, Windows shut down or restarted, or you signed out, without wsl-spawner's stop; or it was stopped outside
  wsl-spawner. The distribution's journal says when that start ended and how; Windows' System and Application logs, read
  as any user may, say what happened then. `check` says it on a line after the cut, `status` and the screen after its
  words, and `status --json` in `last_end`'s `cause` and `cause_at`.
- `hook install` raises Windows' wait for the sign-out scripts where it is too short for `stop_limit`: to the limit, a
  minute for WSL to end and a minute for each other sign-out script, PowerShell's too. It notes what was there;
  `hook remove` and `uninstall` put it back, as long as it is still the wait it set and no other copy's hook stays.
  `hook status` says it, and `hook status --json` has `max_wait_before_seconds`. `check`'s way out of a short wait names
  `hook install`. A standard user's hook, an administrator's install, leaves the wait as it is.

## [0.1.2] - 2026-11-12

Two reviews: of what crosses between users, between a distribution and Windows, and in the release pipeline; then of
what the stop and the start do inside a distribution, what the Windows side says, and what the screen shows. And a new
mark.

### Changed

- The mark is a shell prompt in the power ring, where a play triangle was; the icon stands on a dark tile, and the
  screen's header leads with `❯`.
- `history`, the command and the screen, names the step a stop's result is about: `out of time: docker`,
  `a step failed: hook 20-db, exit 3`. A start that failed says `did not start`.
- An error printed on one line is the error, then what to do after a semicolon, where it was in brackets.
- `esc` on the main screen does nothing, where it quit: going back twice from another screen no longer leaves the
  program. In setup it goes back a page. `q` quits both.
- `uninstall` on a Scoop install no longer needs `--purge`, and keeps the settings without it, as elsewhere.
- `status --json` has `engine_states`, and `stop_units` holds the units a stop takes, those running now, with the
  others in `stop_units_skipped`; `systemd` may be `noanswer`. A stop's `result` may be `busy`, and its steps may hold
  `record`.
- The start script is version 3. `check` names a distribution set up by 0.1.0 or 0.1.1, or by 0.1.2 as first
  released, and `set NAME --managed` puts the new script in.

### Fixed

Inside a distribution:

- A `docker` that answers for a daemon that is not the distribution's own, as Docker Desktop's WSL integration and a
  `podman-docker` shim do, was taken for its Docker: every stop ended `failed`, and the next start ran `docker start`
  on that daemon's containers. It is left alone now, and said as such.
- Two stops of one distribution could run at once, as the sign-out hook's while one asked by hand was at work: the
  second ran every hook again and emptied the list of containers to bring back. One runs at a time; the second leaves
  the distribution to the first, and with `--shutdown` WSL's stop waits for it.
- `set NAME --managed`, `--keep` or `--logon` on a distribution with containers to bring back could say WSL did not
  answer, and save nothing: the set-up ran the whole start script inside a call WSL cuts off after a minute. It starts
  the start service now, and does not wait for it.
- Two runs of the start script at once, as at a boot a set-up caused, started each container twice.
- Where a stop could not write its notes, on a full disk or a read-only `/var/lib`, it left empty ones and ended
  `clean`, so nothing came back at the next start. The old notes stay now, and the stop ends `failed` with a step that
  says so. Under dash and BusyBox a file that could not be made ended the stop, or the start, before its first step.
- A stop ended `failed` where `docker.service` has no `docker.socket` beside it.
- A Docker stopped by hand, its socket listening, read as a daemon that does not answer, with a warning. It reads as
  asleep.
- A container whose engine names no restart policy was misread, and one run with `--rm` stayed on the list to bring
  back.
- A name in `stop-units` that systemd does not have, or a unit that does not run, was counted in the plan and passed
  by without a word. `check` names them, and the plan counts the units that run.
- A systemd that did not answer within its time read as no systemd.
- The start at sign-in waited up to two minutes for each distribution before starting the next. They are all
  started, then waited for together.
- Hooks ran with a folder of Windows as their own (`/mnt/c/Windows`); a stop and a start run from `/` now.
- A build older than the start script in a distribution took that script for the older one, and `set --managed` put
  its own back.
- Bytes that are no UTF-8, the single-byte escape among them, and the line separators U+2028 and U+2029 are shown as
  question marks, as the other control characters are.
- `wsl-spawner set NAME --managed` on a distribution set up by an older version put nothing in it and said "nothing to
  change", though `check` named it as the way to update the start script. It now puts the newer one.

On Windows:

- A standard user whose sign-out hook starts a program in their own folder could not take it out: the advice, an
  administrator's `hook remove`, takes out only the administrator's own. The message names the Local Group Policy
  Editor's page instead, and
  [setup](https://github.com/kanggu12/wsl-spawner/blob/main/docs/setup.md#for-a-standard-user) says how an administrator
  installs the hook for a standard user. The refusal comes before the question, not after a yes.
- `check` told a person to run `hook install` for another copy's hook that others can change, which changes nothing.
  It names that copy's own `hook remove`, says the program once, and fits its advice to a standard user.
- A program reached through a junction, as a profile moved to another drive, was judged by the junction's own
  folders, not by those it leads to. A drive's root that lets users delete was counted, though no one deletes a
  drive's root, so every program on such a drive was refused.
- `uninstall` of a program this user may not delete, as one an administrator installed under Program Files, took out
  the hook and the sign-in program and then failed at the files, each time. It keeps the program now, and says how
  it is removed.
- A program in a folder with a blank in its path (a user named Ann Lee, `C:\Users\Ann Lee\...`) was written into the
  sign-out policy bare, which Windows may skip, or cut at the blank. It is written in quotes now.
- `hook install` and `hook remove` have a help of their own, with their exit codes; every help fits 80 columns.

On the screen:

- With more distributions than rows, the selected one could be off the screen, with no row lit. The table shows the
  rows around it, and says how many more there are.
- The error screen cut the error with "…"; it wraps now, and offers `r` to read again.
- A distribution without systemd was told each service gets its time, and offered a set-up that cannot be made. It is
  told what it gets, and the keys offer only what does something: `s` for one running, `t` for one stopped.
- Setup's colors page drew the whole wizard in the colors chosen, which a terminal of the other kind cannot read; it
  shows them on a row of their own. Its hook page asked whether to install a hook already installed, and its last
  page stayed "Confirm".
- A section with no room for what it says drew its heading alone, or a bar cut off; it goes whole now. In ASCII marks
  the dot between words was a full stop, and the mark's prompt ran into its ring.
- Korean: wsl-spawner's stop is 멈춤 everywhere and Windows' shutdown 종료; lines translated word for word are said
  plainly.

In the release pipeline:

- A run of the release started again, with `checksums.txt` or the Scoop manifest already out and a zip missing, would
  have uploaded a zip built again beside the checksums of the first build. It stops, with nothing uploaded. The demo
  image is pushed only once the release is out.

### Security

- The sign-out hook is in a policy for every user of the PC and runs as whoever signs out, an administrator among them.
  `hook install` now refuses a program that anyone beyond its installer, the system and the administrators can change,
  or that stands in a folder, or under one, that they can: it names who, and where. `check` says it of a hook already
  installed, of another copy's, and of one whose program is gone from a folder where anyone may put a file. The install
  script's own folder, under your profile, is such a place; a folder under a drive's root usually lets every user write
  in it.
- A standard user, who would type an administrator's name and password to change the policy, is refused by `hook
  install`, `hook remove` and `uninstall` unless only administrators can change the program Windows would start: one
  swapped for the real program by whatever runs as that user would be started as the administrator.
- The keep-running service waits for a signal whose name ended with a checksum of the distribution's name, which any
  program on the PC could work out, and take first or send. The name now ends with a secret made at each set-up.
- What a distribution says, a hook's name, a container's name or image, a unit's name, what `wsl.exe` printed on an
  error, is shown with each control character, escape sequences among them, and each mark that sets the direction of
  text turned into a question mark. A hook named with an escape sequence could redraw the terminal or set its clipboard.
- A look and a stop no longer start Docker's daemon that was stopped by hand: Docker's packages leave `docker.socket`
  listening, and asking such a daemon anything starts it. It reads as down, and is left alone.
- The start script that runs as root at each start of a distribution (version 2) runs only an id for a container, which
  is hex, so a line of its list could not hand an engine an option; it reads only files, not a pipe put in their place;
  and what it makes is readable by all and written by root alone, whatever umask it is started with. The stop and the
  set-up do the same. `check` tells a distribution set up by 0.1.0 or 0.1.1 from this, and `set NAME --managed` puts
  the new script in.
- A call of `wsl.exe` keeps at most 8 MiB of what it prints, so a distribution cannot use up the memory of the run
  that stops the others.
- `wsl.exe` and `netsh.exe` are run from Windows' own folder, as `gpupdate.exe` was, not found through the PATH: the
  sign-out hook runs `wsl.exe` as whoever signs out.
- `check` and `hook status` no longer look at, or read, a program or script on a share that a sign-out script names:
  a command that only reads made a connection to that host.
- `install.ps1` takes only an `https://` address for `WSL_SPAWNER_BASE_URL` and a full path for `WSL_SPAWNER_BIN_DIR`,
  and its uninstall without the program removes only files, never a folder of the program's name. The release script
  refuses a Scoop manifest with no checksum for a zip.
- Every file of a release is attested, the install script, `checksums.txt` and the Scoop manifest too, not only the
  zips, and a run of the release started again never replaces a file that was published.

## [0.1.1] - 2026-11-12

### Fixed

- A distribution whose look runs out of time, as one with a container engine that hangs does, keeps what was read by
  then: the screen, `status` and `check` show it and say it was read in part, and `status --json` gives
  `complete: false` beside `probe_error`. Nothing of it was shown before.

## [0.1.0] - 2026-11-12

The first release.

### Added

- `stop` stops a distribution the slow way: the programs in `/etc/wsl-spawner/pre-stop.d`, the units in
  `/etc/wsl-spawner/stop-units`, Docker's daemon with all the time left, so each container gets its own stop timeout
  and keeps its restart policy, then Podman's containers and a sync to disk, within `stop_limit` seconds (300 by
  default). Then WSL ends the rest. Distributions stop at the same time; `--shutdown` stops WSL after. A stop asked from
  a terminal in a distribution it stops goes on without that terminal.
- The sign-out hook (`hook install`, `hook remove`, `hook status`): a sign-out script in the local Group Policy, after
  every other, that stops every distribution of wsl-spawner's as Windows signs out, shuts down or restarts, then WSL.
  Installing it asks Windows for an administrator. `hook status` names every other sign-out script, PowerShell's with
  the order the policy gives them, and other copies' hooks.
- `set NAME --managed` sets wsl-spawner up in a distribution: a service notes each start, tells a stop of
  wsl-spawner's from one that cut the services short, and starts again the containers the last stop noted
  (`resume = "running"`, `"policy"` or `"off"`). `--keep` keeps a distribution running with nothing open in it,
  `--logon` starts it when you sign in, and `--skip` leaves it to WSL.
- `start` starts distributions and says which containers came back.
- `status` lists every distribution with what runs in it and how its last start ended. `check` looks over the hook,
  each distribution, the sign-in program, WSL and the ports Windows keeps, and says what to do about each finding.
  `history` lists the stops and starts, who asked, and how each distribution went.
- A screen for all of it, the same in PowerShell and in a WSL terminal, and CLI mode, which prints the distributions
  and the commands to use next. Setup is a wizard of seven pages. English and Korean, dark and light.
- `--json` for scripts on `status`, `stop`, `start`, `set`, `check`, `history` and `hook status`, and `--events` for a
  stop's steps as they happen.
- `install.ps1` for PowerShell 5.1 and 7, a Scoop manifest, and `uninstall`, which takes out the hook, the sign-in
  program and what wsl-spawner set up in the running distributions, then itself; `--purge` the settings and history.
- `--demo`, and the demo image `ghcr.io/kanggu12/wsl-spawner-demo`: the screen on a made-up PC.

[Unreleased]: https://github.com/kanggu12/wsl-spawner/compare/v0.1.2...HEAD
[0.1.2]: https://github.com/kanggu12/wsl-spawner/releases/tag/v0.1.2
[0.1.1]: https://github.com/kanggu12/wsl-spawner/releases/tag/v0.1.1
[0.1.0]: https://github.com/kanggu12/wsl-spawner/releases/tag/v0.1.0
