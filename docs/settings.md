# Settings

## config.toml

`%APPDATA%\wsl-spawner\config.toml`. Every key is optional, and the file too: without it the defaults apply. Setup,
`set` and `setup --FLAG` write it, and keep its notes and anything written by hand. A key wsl-spawner does not know, or
a value it cannot use, stops a command that reads the settings, which names the key, or the line where the file is not
TOML; `setup --FLAG` writes a value it can use. The sign-out hook still stops, with the defaults, and its history entry
says why; the sign-in program starts nothing and says why. `hook` and `uninstall` go on.

| Key | Default | Takes |
|:---|:---|:---|
| `language` | `"auto"` | `"en"`, `"ko"`, or `"auto"`: `WSL_SPAWNER_LANG`, then Windows' display language. For the screen, setup and the overview; the commands print English |
| `theme` | `"auto"` | `"dark"`, `"light"`, or `"auto"`, which asks the terminal |
| `mode` | `"tui"` | what a bare `wsl-spawner` opens in a terminal: `"tui"`, the screen, or `"cli"`, the overview |
| `skip` | `[]` | distributions, by name, left to WSL: wsl-spawner neither stops nor starts them |
| `logon` | `[]` | distributions started at sign-in, in this order, each kept running; not one of `skip` |
| `resume` | `"running"` | which running containers come back at the next start after a stop of wsl-spawner's: `"running"`, every one; `"policy"`, those with a restart policy of `always` or `unless-stopped`; `"off"`, none that wsl-spawner brings back itself: a restart policy still brings its container back |
| `stop_limit` | `300` | seconds a distribution's steps may take together, from 10 to 3600 |

```toml
language = "auto"
theme = "auto"
mode = "tui"
skip = []
logon = ["Ubuntu-24.04"]
resume = "running"
stop_limit = 300
```

Names are compared without case, as `wsl.exe` compares them. Docker Desktop's, Rancher Desktop's and Podman's machine
distributions are always left to those tools.

`stop_limit` is for each distribution, and they stop at the same time. The sign-out hook has as long as Windows waits
for all the sign-out scripts together, 600 seconds by default, less what the scripts before it take;
`wsl-spawner check` says when the limit and a minute are more than that.

## Inside a distribution

What a stop does first, set by you, inside the distribution:

- **`/etc/wsl-spawner/pre-stop.d/`**: programs run before anything else stops, as root, in the order of their names,
  each with the time left. A script needs its execute bit and its first line (`#!/bin/sh`). Exit code 0 is a step done;
  another code makes the stop end as `failed`, and the other steps still run. For example, a game server's save:

  ```sh
  #!/bin/sh
  # /etc/wsl-spawner/pre-stop.d/10-save-world
  docker exec minecraft rcon-cli save-all flush
  ```

- **`/etc/wsl-spawner/stop-units`**: systemd units to stop before the containers, one name a line, `#` for a note.
  Each gets its own `TimeoutStopSec`:

  ```text
  # the game server first
  minecraft.service
  postgresql.service
  ```

Setting wsl-spawner up makes the `pre-stop.d` folder; taking it out leaves both, as they are yours.

## The files wsl-spawner writes

| Where | What | When |
|:---|:---|:---|
| `%APPDATA%\wsl-spawner\config.toml` | the settings | setup, `set`, `setup --FLAG` |
| `%LOCALAPPDATA%\wsl-spawner\history.jsonl` | the stops and starts, 500 kept | each stop and start |
| `%LOCALAPPDATA%\wsl-spawner\crash-*.txt` | what a crash of the program said, for the next start to show | a crash |
| `HKCU\...\CurrentVersion\Run`, value `wsl-spawner` | the sign-in program | a distribution set to start at sign-in |
| `C:\Windows\System32\GroupPolicy\User\Scripts\scripts.ini`, `GroupPolicy\gpt.ini` | the sign-out hook; each kept once as `*.before-wsl-spawner`, an empty copy where there was none, which uninstall keeps | `hook install`, `hook remove` |
| `/usr/local/lib/wsl-spawner/start.sh`, `/etc/systemd/system/wsl-spawner.service` | the start script and its service | `set NAME --managed` |
| `/etc/systemd/system/wsl-spawner-keepalive.service` | the service that keeps it running | `set NAME --keep` |
| `/var/lib/wsl-spawner/` | `runs` (its starts), `last-stop`, `resume` (to bring back), `resumed` | each stop and start, where set up |
| `/run/systemd/system/docker.service.d/zz-wsl-spawner.conf` (`snap.docker.dockerd.service.d` for Docker from a snap) | the daemon's stop timeout for this stop | a stop with Docker; gone at the next start |

`wsl-spawner uninstall` takes all of it out, but for the settings and the history, which `--purge` removes too, the
`*.before-wsl-spawner` copies, and what it set up in a distribution that does not run then ([Setup](setup.md#remove)).
