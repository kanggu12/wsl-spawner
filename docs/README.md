<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img alt="wsl-spawner" src="assets/logo-light.svg" width="300">
  </picture>
</p>

# Documentation

| Guide | What it covers |
|:---|:---|
| [Setup](setup.md) | requirements, every way to install, upgrade and remove; Windows, WSL and PowerShell versions |
| [How it works](how-it-works.md) | a stop step by step, the sign-out hook and Windows' wait, the start and how the last one ended, keeping a distribution running, the start at sign-in, the check |
| [Commands](commands.md) | TUI and CLI, every command, flag, key and exit code, the `--json` shapes |
| [Settings](settings.md) | `config.toml`, the hooks and units a distribution stops first, the files wsl-spawner writes |
| [Troubleshooting](troubleshooting.md) | what each finding means: a stop that runs out of time, a cut start, a container that does not come back, a hook that does not run |
| [Internals](internals.md) | what runs where, the layers, what it reads and changes, the decisions, the tests and CI |
| [Third-party notices](THIRD_PARTY_NOTICES.md) | the libraries wsl-spawner is built from, and its logo's font, with their licenses |

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/day-dark.svg">
  <img alt="A day with wsl-spawner. At sign-in, its sign-in program starts the distributions you chose. All day it keeps them running, where WSL would stop one that no Windows program uses. At sign-out, shutdown or an update's restart, Windows runs the sign-out scripts and wsl-spawner's runs last: it gives every service and container its time to stop, notes how the stop went, then stops WSL. At the next start, the containers come back and wsl-spawner knows how the last run ended. A crash, a Windows update of WSL or wsl --shutdown give no sign-out: the next start says the run ended without its time." src="assets/day-light.svg">
</picture>

For working on the code, see [CONTRIBUTING](../.github/CONTRIBUTING.md).
