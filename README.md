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
  <a href="https://github.com/kanggu12/wsl-spawner/releases"><img alt="Release v0.2.2" src="docs/assets/badges/release.svg"></a>
  <a href="LICENSE"><img alt="License: MIT" src="docs/assets/badges/license.svg"></a>
  <a href="https://github.com/kanggu12/wsl-spawner/pkgs/container/wsl-spawner-demo"><img alt="Demo image" src="docs/assets/badges/demo.svg"></a>
  <img alt="Runs on Windows 10 and 11, x64 and ARM64, with WSL 2" src="docs/assets/badges/runs-on.svg">
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> |
  <a href="#what-it-does">What it does</a> |
  <a href="#how-it-works">How it works</a> |
  <a href="#for-contributors">For contributors</a> |
  <a href="#documentation">Documentation</a>
</p>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/demo-dark.gif">
  <img alt="The wsl-spawner screen on a made-up PC with four distributions, Ubuntu-24.04 selected. The s key asks first, in a box that lists the steps. The stop runs: the hook, then Docker's three containers, each step with a bar of its time; it ends in 23 seconds of the 300 allowed. Then the main screen, the history and the check." src="docs/assets/demo-light.gif">
</picture>

When WSL stops a distribution, by `wsl --terminate`, an idle timeout, an update or Windows signing out, its services
get about ten seconds: Docker's containers and a database's shutdown are cut off where they are. wsl-spawner stops each
distribution the slow way first, by itself as Windows signs out, and starts again what was running.

## Quick start

Windows 10 or 11 with WSL 2 ([requirements](docs/setup.md#requirements)), in PowerShell:

```powershell
# 1. Install for this user. It opens setup to choose how it works.
irm https://github.com/kanggu12/wsl-spawner/releases/latest/download/install.ps1 | iex

# 2. In a new terminal, look over what stopping and starting stand on. It changes nothing.
wsl-spawner check

# 3. Open the screen: s stops the selected distribution the slow way, t starts it again.
wsl-spawner
```

The same `wsl-spawner` works in a WSL terminal opened after the install. With Scoop:
`scoop install https://github.com/kanggu12/wsl-spawner/releases/latest/download/wsl-spawner.json`.

Setup asks one thing a page, the language and colors shown as they are chosen, and nothing changes before its last
page:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/setup-dark.png">
  <img alt="The setup wizard at step 5 of 7, Distributions. A rail on the left lists the steps, the first four checked. Each distribution has a level: stop only, set up, kept running, or started at sign-in. Ubuntu-24.04 is started at sign-in; Debian is stopped, so what is set up in it shows once it runs; Work, selected, is kept running." src="docs/assets/setup-light.png">
</picture>

`wsl-spawner setup` opens it again. From any terminal, `wsl-spawner stop NAME` stops a distribution the same way and
prints each step. No install yet? [Try it in a sandbox](docs/setup.md#try-it-in-a-sandbox): the screen on a made-up
PC, wherever Docker runs.

## What it does

- **Gives each service its time.** Your hooks and units first, then every container with its own stop timeout.
- **Runs as Windows signs out.** Its hook runs at every sign-out, shutdown and restart, an update's restart too.
- **Brings back what ran.** The containers that were running start again, their restart policies kept.
- **Keeps a distribution up.** WSL stops one with nothing open in it; wsl-spawner keeps it running, or starts it at
  sign-in.
- **Says how the last run ended.** After an update or a crash, the next start says the run was cut, and by what.
- **Scriptable.** Everything on the screen is a command too, with `--json`, and `check` says what to fix.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/steps-dark.svg">
  <img alt="A stop in seven steps: note the containers to bring back, your hooks, your units, Docker's daemon stopping every container, Podman's containers, sync, then record how it went. Under them, stop_limit, 300 seconds, as a row of cells: the demo's stop lit 23 seconds of it; then WSL ends what is left." src="docs/assets/steps-light.svg">
</picture>

<p align="center"><i>A stop, step by step, the same from <code>wsl-spawner stop</code>, the screen and the sign-out hook.</i></p>

## How it works

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/pieces-dark.svg">
  <img alt="What runs where. Above: a Windows terminal, a WSL terminal, where the Linux build sends commands and gets JSON back, and Windows itself at sign-in and sign-out. In the middle: wsl-spawner.exe and its commands. Below: each distribution through wsl.exe as root, Windows' settings, which it sets, and the event logs, which it reads." src="docs/assets/pieces-light.svg">
</picture>

One Go program, built for Windows and for Linux: on Windows it does the work, and in WSL the same program is a launcher
that hands each command to `wsl-spawner.exe` and draws the screen from its JSON. It reaches a distribution only through
`wsl.exe`, as root, never starts one just to read it, and leaves no program of its own running on Windows.
[How it works](docs/how-it-works.md) follows a stop and a start, step by step.

## For contributors

wsl-spawner is MIT licensed and developed in the open. No change needs your own distributions: the tests run the
scripts it sends into a distribution against made-up roots, and the commands and the screen against a made-up WSL.
[CONTRIBUTING.md](.github/CONTRIBUTING.md) says what a change needs, and [Internals](docs/internals.md) how the parts
fit together.

| Path | What it is |
|:---|:---|
| `cmd/wsl-spawner` | the program; in WSL, the launcher that hands each command to `wsl-spawner.exe` |
| `internal/front` | the commands, the screen and setup, the commands' JSON, the words in English and Korean |
| `internal/engine` | the distributions, the scripts run inside them, a stop, set-up, the check, the history |
| `internal/machine` | `wsl.exe`, Windows' calls, the local Group Policy and its wait, the event logs |
| `internal/app`, `settings`, `core` | the pieces put together and the demo's made-up PC, `config.toml`, the version |

```sh
git clone https://github.com/kanggu12/wsl-spawner && cd wsl-spawner
make check          # gofmt, go vet, staticcheck, shellcheck, the unit tests
make test-windows   # from WSL: every package again as a Windows program
go run ./cmd/wsl-spawner --demo
```

Report a stop it got wrong in [Issues](https://github.com/kanggu12/wsl-spawner/issues/new/choose), with the output of
`wsl-spawner check` and `wsl-spawner history --json`. See what changed in [CHANGELOG.md](CHANGELOG.md); report
security problems as [SECURITY.md](.github/SECURITY.md) says.

## What it runs on

| | Needs |
|:---|:---|
| **Windows** | Windows 10 or 11, x64 or ARM64 |
| **WSL** | WSL 2 distributions; systemd in those wsl-spawner is set up in (a stop works without it) |
| **Sign-out hook** | the local Group Policy's sign-out scripts, which Pro, Enterprise and Education run; an administrator to install it |
| **Install script** | Windows PowerShell 5.1, which every Windows has, or PowerShell 7 |
| **Demo sandbox** | Docker |

It runs as you, with no network; only the sign-out hook asks for an administrator. [Setup](docs/setup.md#requirements)
has the details.

## Documentation

| Guide | What it covers |
|:---|:---|
| [Setup](docs/setup.md) | requirements, every way to install, upgrade and remove, the sandbox |
| [How it works](docs/how-it-works.md) | why, a day, a stop step by step, the sign-out hook, the start and how the last one ended, how it compares |
| [Commands](docs/commands.md) | the screen and its keys, every command and flag, the `--json` shapes, the exit codes |
| [Settings](docs/settings.md) | `config.toml`, the hooks and units stopped first, the files wsl-spawner writes |
| [Troubleshooting](docs/troubleshooting.md) | what each finding of `check` means and what to do about it |
| [Questions](docs/questions.md) | administrator rights, signing out, what it changes, Docker Desktop, removing it |
| [Internals](docs/internals.md) | what runs where, the layers, the decisions, the tests and CI |

## License

MIT ([LICENSE](LICENSE)). The libraries wsl-spawner is built from, and the font of its logo and figures, are listed
with their licenses in [THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md).
