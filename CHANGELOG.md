# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
versions follow [semantic versioning](https://semver.org/): a release that adds something raises the minor number, one
that only fixes raises the patch number. Each release's section here is its release notes.

## [Unreleased]

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

[Unreleased]: https://github.com/kanggu12/wsl-spawner/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/kanggu12/wsl-spawner/releases/tag/v0.1.1
[0.1.0]: https://github.com/kanggu12/wsl-spawner/releases/tag/v0.1.0
