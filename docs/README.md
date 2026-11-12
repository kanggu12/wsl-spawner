<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img alt="wsl-spawner" src="assets/logo-light.svg" width="300">
  </picture>
</p>

# Documentation

| Guide | What it covers |
|:---|:---|
| [Setup](setup.md) | requirements, a sandbox to try it in, every way to install, upgrade and remove; Windows, WSL and PowerShell versions |
| [How it works](how-it-works.md) | why it is needed and how it compares, a day with it, a stop step by step, the sign-out hook and Windows' wait, the start and how the last one ended, keeping a distribution running, the start at sign-in, the check |
| [Commands](commands.md) | TUI and CLI, every command and flag, the screen and its keys, the exit codes, the `--json` shapes |
| [Settings](settings.md) | `config.toml`, the hooks and units a distribution stops first, the files wsl-spawner writes |
| [Troubleshooting](troubleshooting.md) | what each finding means: a stop that runs out of time, a cut start, a container that does not come back, a hook that does not run |
| [Questions](questions.md) | an administrator, the time a sign-out takes, what it changes, Docker Desktop, rootless engines, WSL 1, updates and crashes, removing it |
| [Internals](internals.md) | what runs where, the layers, what it reads and changes, the decisions, the tests and CI |
| [Third-party notices](THIRD_PARTY_NOTICES.md) | the libraries wsl-spawner is built from, and the font of its logo and figures, with their licenses |

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/day-dark.svg">
  <img alt="A day with wsl-spawner. Sign-in: it starts the distributions you chose. All day: it keeps them running. Sign-out: its hook gives each service its time. Next start: the containers come back. Under it, the day as cells: starting, running, its stop, off, back. A crash, an update of WSL or wsl --shutdown has no sign-out; the next start says so." src="assets/day-light.svg">
</picture>

For working on the code, see [CONTRIBUTING](../.github/CONTRIBUTING.md).
