<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img alt="wsl-spawner" src="docs/assets/logo-light.svg" width="420">
  </picture>
</p>

<p align="center">
  <b>Give WSL's services and containers their time to stop when Windows shuts down, and bring back what was running.</b><br>
  A terminal screen and a CLI for every WSL 2 distribution on your PC.
</p>

<p align="center">
  <a href="https://github.com/kanggu12/wsl-spawner/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/kanggu12/wsl-spawner/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/kanggu12/wsl-spawner/releases"><img alt="Release v0.2.0" src="docs/assets/badges/release.svg"></a>
  <a href="LICENSE"><img alt="License: MIT" src="docs/assets/badges/license.svg"></a>
  <a href="https://github.com/kanggu12/wsl-spawner/pkgs/container/wsl-spawner-demo"><img alt="Demo image" src="docs/assets/badges/demo.svg"></a>
  <img alt="Runs on Windows 10 and 11, x64 and ARM64, with WSL 2" src="docs/assets/badges/runs-on.svg">
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> |
  <a href="#why-wsl-spawner">Why</a> |
  <a href="#a-day-with-wsl-spawner">A day</a> |
  <a href="#a-stop-step-by-step">A stop</a> |
  <a href="#the-screen">The screen</a> |
  <a href="#architecture">Architecture</a> |
  <a href="docs/README.md">Documentation</a>
</p>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/demo-dark.gif">
  <img alt="The wsl-spawner screen on a PC with four distributions, Ubuntu-24.04 selected: it runs, is set up, kept running and started at sign-in, with three of its four containers running; beside the table, its last stop step by step, its next stop and its containers. The s key asks first, in a box that lists the steps: the hook 10-backup-db, Docker with three containers, the longest stop 60 seconds, then WSL ends the rest. The stop runs: the hook ends in 2 seconds, then Docker's daemon stops every container with its own stop timeout, which a line under the steps says, the time against the 300 seconds the limit allows. Stopped, every step in its time, in 23 seconds; web, db and cache come back at the next start. Back on the main screen the stop is in Recent and in the distribution's stops; the history shows it chosen, step by step; the check says its verdict first, one thing to look at and six fine, its groups in two columns." src="docs/assets/demo-light.gif">
</picture>

When WSL stops a distribution, by `wsl --terminate`, an idle timeout, an update or Windows signing out, its services get
about ten seconds; Docker's containers and a database's shutdown are cut off where they are. wsl-spawner stops each
distribution the slow way first, every service and container with the time it asks for, does it by itself as Windows
signs out, shuts down or restarts, and starts again what was running.

## Quick start

In PowerShell, on Windows 10 or 11 with WSL 2 ([requirements](docs/setup.md#requirements)):

```powershell
# Install for this user; it opens setup to choose how it works.
irm https://github.com/kanggu12/wsl-spawner/releases/latest/download/install.ps1 | iex

# In a new terminal. It only reads until you confirm a change.
wsl-spawner
```

The same `wsl-spawner` works in a WSL terminal opened after the install. With Scoop:
`scoop install https://github.com/kanggu12/wsl-spawner/releases/latest/download/wsl-spawner.json`.
[Setup](docs/setup.md#install) has every way to install and remove it; to look first, [try it in a
sandbox](#try-it-in-a-sandbox).

Setup is a short wizard, a page for each choice, the language and colors shown as they are chosen. The install opens
it, or the first start where the install did not; `wsl-spawner setup` opens it again.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/setup-dark.png">
  <img alt="The setup wizard at step 5 of 7, Distributions. A rail on the left lists the steps: Welcome, Language, Colors and Start checked, Distributions the step now, Sign-out hook and Confirm to come. Each distribution has a level, chosen with space: stop only, set up, kept running, or started at sign-in, each including the ones before it. Ubuntu-24.04 is set up, kept running and started at sign-in; Debian is stopped, and what is set up in it shows once it runs; Work, selected, is set up and kept running. The keys: up and down choose, space changes the level, enter goes on, left goes back, q quits." src="docs/assets/setup-light.png">
</picture>

## Why wsl-spawner

- **WSL gives services seconds.** Measured on WSL 3.0.5: `wsl --terminate` ends a distribution's services after about
  10 seconds, a `systemctl poweroff` inside after about 25. A service that needs longer is cut off.
- **Signing out is a stop too.** Windows signs out before every shutdown and restart, those an update makes too, and WSL
  goes with it. A sign-out hook is the one place to run first.
- **`docker stop` loses the restart policy.** A container stopped one by one is marked stopped by hand, and
  `unless-stopped` no longer brings it back. Stopping Docker's daemon keeps it, so that is what wsl-spawner stops.
- **A distribution with nothing open stops.** WSL ends it within half a minute of the last terminal closing (its idle
  timeout is 15 seconds), and its servers with it, unless something holds it.
- **A cut start says nothing.** After an update or a crash, the services just ended. wsl-spawner notes each start and
  each of its stops, so the next start knows how the last one ended, and reads what ended it when it was not its stop.

## A day with wsl-spawner

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/day-dark.svg">
  <img alt="A day with wsl-spawner. At sign-in, its sign-in program starts the distributions you chose. All day it keeps them running, where WSL would stop one that no Windows program uses. At sign-out, shutdown or an update's restart, Windows runs the sign-out scripts and wsl-spawner's runs last: it gives every service and container its time to stop, notes how the stop went, then stops WSL. At the next start, the containers come back and wsl-spawner knows how the last run ended. A crash, a Windows update of WSL or wsl --shutdown give no sign-out: the next start says the run ended without its time." src="docs/assets/day-light.svg">
</picture>

No program of wsl-spawner's stays running on Windows: the sign-in program starts the distributions and ends, and the
sign-out hook runs and ends. Keeping a distribution running is a service inside it, which holds Windows' own
`waitfor.exe` open, as Task Manager shows. Each part is a choice, per distribution: stop only,
set up (a start script that brings the containers back), kept running, started at sign-in.

## A stop, step by step

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/steps-dark.svg">
  <img alt="A stop, step by step. 1, the containers to bring back are noted first, so a stop cut short still brings them back. 2, your hooks in /etc/wsl-spawner/pre-stop.d run in the order of their names. 3, the systemd units /etc/wsl-spawner/stop-units names stop, ordered by their dependencies. 4, Docker's daemon stops every container itself, each with its own stop timeout, and keeps their restart policies. 5, Podman's containers stop, each with its own timeout. 6, the journal and the disk are synced and the stop's record says how it ended: clean, limit or failed. All the steps share stop_limit, 300 seconds by default; then WSL ends what is left." src="docs/assets/steps-light.svg">
</picture>

Inside the distribution, as root, all within `stop_limit` seconds (300 by default); then WSL ends what is left.
Distributions stop at the same time. The screen shows each step as it runs, with its time as a bar:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/stop-dark.png">
  <img alt="A stop of Ubuntu-24.04 on the screen, after its end: the hook 10-backup-db done in 2 seconds, Docker with 3 containers in 21, the sync to disk, then WSL ending the rest, each with a bar of its time. Stopped, every step in its time: 23 seconds of the 300 the limit allows, drawn as a bar. Web, db and cache come back at its next start." src="docs/assets/stop-light.png">
</picture>

What was running is noted inside the distribution. Where wsl-spawner is set up, a service of its own notes each start,
tells a stop of wsl-spawner's from one that cut the services short, and starts again the containers that were running
and are not back by their own restart policy. [How it works](docs/how-it-works.md) has every step and what each one
waits for.

## The screen

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/screen-dark.png">
  <img alt="The wsl-spawner screen on a wide terminal, in two panes. On the left, the table of four distributions: Ubuntu-24.04, selected, runs, wsl-spawner is set up in it and starts it at sign-in, three of its four containers run, its last stop was clean on 11-11 at 22:36; Debian is stopped; Work runs with wsl-spawner stopping it only; docker-desktop is left to Docker Desktop. Under the table, the recent stops and starts, each with who asked and how each distribution went, one of them a step that failed: the hook 10-backup-db with exit code 1. On the right, Ubuntu-24.04: each service gets its time to stop, and 3 containers come back. Its last stop, clean in 41 seconds as Windows signed out, each step with a bar of its time, and what came back at the next start. Its next stop: the hook, Docker with three containers, the longest stop 60 seconds, as a bar of the 300 the limit allows. Its containers with their images, each with how it comes back. Its choices, all on. Its stops, each a bar of its time against the limit. The header says the sign-out hook is installed, one distribution is kept running and one starts at sign-in." src="docs/assets/screen-light.png">
</picture>

| Key | Does |
|:---|:---|
| `s` / `S` | stop the selected distribution / every one of wsl-spawner's, then WSL |
| `t` | start it, and see what came back |
| `m` `k` `l` `x` | set wsl-spawner up in it, keep it running, start it at sign-in, leave it to WSL |
| `c` / `h` | the check / the history |
| `?` | every key, and how it works |

Each change asks first, in a box that says what it does. The screen is drawn from the commands' JSON, the same in
PowerShell and in a WSL terminal, so everything on it is a command too. Started without a command, wsl-spawner opens
the screen, or in CLI mode prints the distributions and the commands to use next:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/cli-dark.png">
  <img alt="CLI mode: a table of the four distributions with their state, how far wsl-spawner goes with each, their containers and their last stop; then the commands to use next, with the distributions' names in them: stop Ubuntu-24.04, start Debian, set Work --managed, check, tui." src="docs/assets/cli-light.png">
</picture>

## Try it in a sandbox

On any system with Docker, the screen on a made-up PC, where nothing real is read or changed:

```sh
docker run --rm -it ghcr.io/kanggu12/wsl-spawner-demo
```

`-e WSL_SPAWNER_LANG=ko` before the image name for Korean. Once installed, `wsl-spawner --demo` opens the same PC.

## Commands

| Command | Does |
|:---|:---|
| `wsl-spawner` | the screen in TUI mode, the distributions and the commands to use in CLI mode |
| `status [--json]` | every distribution: running or not, what wsl-spawner does with it, its containers, its last stop |
| `stop NAME... \| --all [--shutdown] [--yes]` | stop the slow way, printing each step; `--shutdown` stops WSL after |
| `start NAME...` | start, and say which containers came back |
| `set NAME [--managed] [--keep] [--logon] [--skip]` | choose what wsl-spawner does with it; `=false` turns one off |
| `hook install \| remove \| status` | the sign-out hook, in the local Group Policy; installing and removing it ask for an administrator |
| `check [--json]` | look over what stopping and starting stand on, and what to do about it |
| `history [--all] [--json] [--clear]` | the stops and starts so far: who asked, and how each distribution went |
| `setup` | the wizard; flags (`--mode cli`, `--stop-limit 600`, ...) set a choice without it |
| `uninstall [--purge]` | remove wsl-spawner and what it put on this PC |

[Commands](docs/commands.md) has every flag, the exit codes and the `--json` shapes.

## Settings

In `%APPDATA%\wsl-spawner\config.toml`, every key optional. Setup and `set` write them:

```toml
mode = "tui"              # what a bare wsl-spawner opens: "tui" or "cli"
skip = ["Scratch"]        # left to WSL: no slow stop, never started; WSL's own stop still ends them
logon = ["Ubuntu-24.04"]  # started at sign-in, in this order, and kept running
resume = "running"        # what comes back: "running" (all that ran), "policy" (always, unless-stopped), "off"
stop_limit = 300          # seconds a distribution's steps may take together
```

[Settings](docs/settings.md) has every key, the hooks and units a distribution stops first, and the files wsl-spawner
writes.

## How it compares

| | Services get their time | At sign-out and restart | Containers come back | Kept running | Says how it last stopped |
|:---|:---:|:---:|:---:|:---:|:---:|
| **wsl-spawner** | yes, each its own | yes, a sign-out hook | yes, by policy and by note | yes | yes, and what cut it |
| `wsl --terminate` | about 10 s | no | by policy only | no | no |
| `systemctl poweroff` inside | about 25 s | no | by policy only | no | no |
| Keep-alive scripts and tray apps | about 10 s | no | by policy only | yes | no |

wsl-spawner runs `wsl --terminate` itself, after its own steps.

## Architecture

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/pieces-dark.svg">
  <img alt="What runs where. In PowerShell or Windows Terminal, wsl-spawner.exe draws the screen and reads the keys. In a WSL terminal, wsl-spawner, the Linux build of the same program, draws the screen there and asks the .exe for each answer, as JSON. At sign-in and sign-out, Windows starts wsl-spawnerw.exe, the same program with no window: logon starts the distributions you chose, logoff stops them all. The program asks wsl.exe, which lists, starts and stops distributions and runs its scripts in them as root. Inside each distribution set up: start.sh at each start, a service that keeps it running, its notes in /var/lib/wsl-spawner. It sets the sign-in program as you and, as an administrator, the sign-out hook and its wait in the local Group Policy. It reads the event logs after a start that ended without its time. It runs as you, with no network, and no program of it stays running on Windows; a distribution kept running holds Windows' own waitfor.exe." src="docs/assets/pieces-light.svg">
</picture>

One Go program, built for Windows and for Linux. On Windows it does the work; in WSL the same program is a launcher
that hands each command to `wsl-spawner.exe`, and draws the screen there from the answers. It reaches a distribution
only through `wsl.exe`, as root, with POSIX `sh` scripts of its own that print `KEY=VALUE` lines, and never starts one
just to read it.

- **The screen stands on the commands' JSON alone.** It can do nothing a command cannot, so a script can do all of it.
- **A distribution is never started to be read.** One that does not run says nothing about what runs in it, and
  Docker's daemon stopped by hand is not woken to be asked.
- **Docker's daemon is stopped, not its containers**, with the time left, so their restart policies stay.
- **Windows and WSL are each one interface.** The tests fill them with a made-up PC, whose every call fails unless the
  test gave it, and a made-up WSL: no test reaches the PC it runs on.

[Internals](docs/internals.md) has the layers, the decisions and the tests.

## Questions

**Does it need an administrator?** Only to install or remove the sign-out hook, which lives in the local Group Policy,
with the policy's wait for sign-out scripts when it is too short for the stop. Everything else runs as you.

**Does signing out take longer?** As long as the stops take: Windows shows its sign-out screen while the hook gives
each service and container its time, `stop_limit` at most, then a few seconds for WSL. A distribution with nothing slow
to stop is done in seconds. `wsl-spawner history` says how long each took.

**What does it change?** Inside a distribution, nothing until you set wsl-spawner up in it: then a service, a start
script and `/etc/wsl-spawner/pre-stop.d` for your own programs, a second service when it is kept running, and the stops
and starts it notes in `/var/lib/wsl-spawner`. On Windows, its settings and history, the sign-in program when a
distribution starts at sign-in, and the sign-out hook when you install it. `wsl-spawner check` says what is there.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/touches-dark.svg">
  <img alt="What wsl-spawner reads and what it changes. It reads WSL's own answers (the distributions Windows lists, which run, WSL's version); inside a running distribution, through wsl.exe as root, systemd, its journal, the units, the containers and its own notes, never starting a distribution or Docker's daemon just to read them; and on Windows, the sign-out scripts and the policy's wait, the sign-in program, and after a run that ended without its time, the event logs. It runs as you. It changes, inside each distribution you set it up in, a start script, two services and its notes; for you, the sign-in program and its settings and history; as an administrator, which Windows asks for, the sign-out hook in the local Group Policy and the policy's wait when it is too short. wsl-spawner set NAME --managed=false takes out what it set up in a distribution; uninstall takes out the rest, the settings and history too with --purge; your hooks and units stay." src="docs/assets/touches-light.svg">
</picture>

**Does it work with Docker Desktop?** Docker Desktop's distributions are its own: wsl-spawner leaves them be and says
so. So is the `docker` command its WSL integration puts in your distribution: wsl-spawner stops and brings back only a
Docker whose daemon runs in the distribution itself, Docker Engine inside it.

**What about rootless Podman or Docker?** wsl-spawner works as root, with root's engines: the containers of another
user's engine, rootless Podman's or rootless Docker's, are not seen, stopped or brought back.

**What about WSL 1?** A WSL 1 distribution has no VM and no systemd, nothing to give time to: it is listed, and left be.

**What if WSL updates itself, or Windows crashes?** Nothing runs first then. In a distribution wsl-spawner is set up
in, `status`, `check` and the screen say next time that the start ended without its time, and, from the
distribution's journal and Windows' event logs, what ended it: a WSL update, a shutdown or a sign-out without the hook,
a crash, or a stop outside wsl-spawner.

**Why did my terminal close?** It ran in the distribution you stopped. The stop goes on in a copy of wsl-spawner with
no window, which the terminal's end does not end, and `wsl-spawner history` says how it went.

**Does it work with wsl-shrinker?** [wsl-shrinker](https://github.com/kanggu12/wsl-shrinker) 0.7 and later stop a
distribution through wsl-spawner where it is installed, so its hooks, units and containers get their time before a
disk is compacted. Those stops are listed in the history as asked by hand.

**How do I remove it?** `wsl-spawner uninstall`, in PowerShell or WSL. It takes out the hook, the sign-in program and
what it set up in the running distributions first. `--purge` removes the settings and the history too.

## Documentation

| Guide | What it covers |
|:---|:---|
| [Setup](docs/setup.md) | requirements, every way to install, upgrade and remove; Windows, WSL and PowerShell versions |
| [How it works](docs/how-it-works.md) | a stop, the sign-out hook, the start and how the last one ended, keeping a distribution running, the start at sign-in, the check |
| [Commands](docs/commands.md) | every command and flag, the screen's keys, the `--json` shapes, the exit codes |
| [Settings](docs/settings.md) | `config.toml`, the hooks and units stopped first, the files wsl-spawner writes |
| [Troubleshooting](docs/troubleshooting.md) | a stop that runs out of time, a container that does not come back, a hook that does not run |
| [Internals](docs/internals.md) | what runs where, the layers, what it reads and changes, the decisions, the tests and CI |

## Open source

MIT licensed. The tests run the scripts wsl-spawner sends into a distribution against made-up roots, and the commands
and the screen against a made-up WSL, so no change needs your own distributions.

- **Report a stop it got wrong** in [Issues](https://github.com/kanggu12/wsl-spawner/issues/new/choose), with the
  output of `wsl-spawner check` and `wsl-spawner history --json`.
- **Change the code**: [CONTRIBUTING.md](.github/CONTRIBUTING.md) maps the code and says what a change needs.
- **See what changed** in [CHANGELOG.md](CHANGELOG.md); report security problems as [SECURITY.md](.github/SECURITY.md)
  says.

```sh
git clone https://github.com/kanggu12/wsl-spawner && cd wsl-spawner
make check          # gofmt, go vet, staticcheck, shellcheck, the unit tests
make test-windows   # from WSL: every package again as a Windows program
go run ./cmd/wsl-spawner --demo
```

## License

MIT ([LICENSE](LICENSE)). The libraries wsl-spawner is built from, and the font of its logo and figures, are listed
with their licenses in [THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md).
