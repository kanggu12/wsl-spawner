# Commands

`wsl-spawner COMMAND --help` says the same, for each command. Every command runs in PowerShell, cmd and a WSL terminal;
in WSL the launcher hands it to `wsl-spawner.exe`. The commands print English, for scripts; the screen, setup and the
overview speak the settings' language.

## Started without a command

| Settings | In a terminal | Without one |
|:---|:---|:---|
| `mode = "tui"` (default) | the screen | `status`, as text |
| `mode = "cli"` | the distributions and the commands to use next | `status`, as text |

With no settings file yet, a terminal's first start opens setup first. `--demo` opens the screen on a made-up PC, where
nothing real is read or changed; `--theme auto|dark|light` chooses the screen's colors, there and with `tui`;
`--lang auto|en|ko` the language of the screen, setup and the overview, on every command.

## The commands

### status

`wsl-spawner status [--json]`: every distribution, whether it runs, how far wsl-spawner goes with it (`stop only`,
`set up`, `kept running`, `at sign-in`, or who it is left to: `left to WSL`, `left to Docker Desktop`, `WSL 1`), how
many of its containers run (3/4), and how its last start ended: as noted inside it, or else as wsl-spawner's newest stop
of it in the history went. A distribution that does not run is not started to be read: how far wsl-spawner goes with it
is a dash (`not-read` in `--json`), unless it starts at sign-in, which holds set up and kept running.

### stop

`wsl-spawner stop NAME... | --all [--shutdown] [--yes] [--json [--events]]`

Stops each distribution named, or every running one of wsl-spawner's with `--all`, the slow way
([how](how-it-works.md#a-stop)), all at once, and with `--shutdown` WSL after. It says the plan and asks first; `--yes`
does it without asking, and without a terminal it needs `--yes`. Each step is printed as it ends. `--json` prints the
results at the end; with `--events`, each step as a line of JSON as it happens, the results last.

A name that is not a distribution, or one wsl-spawner leaves be, is refused (exit code 2); one that does not run is
said and passed by.

### start

`wsl-spawner start NAME... [--json]`: starts each, waits where wsl-spawner is set up for its service to bring back what
the last stop noted, and says what came back.

### set

`wsl-spawner set NAME [--managed[=false]] [--keep[=false]] [--logon[=false]] [--skip[=false]] [--yes] [--json]`

| Flag | On | Off |
|:---|:---|:---|
| `--managed` | set wsl-spawner up in it ([what that is](how-it-works.md#the-start)) | take all of it out; your hooks and `stop-units` stay |
| `--keep` | keep it running with nothing open in it | let WSL stop it when nothing runs |
| `--logon` | start it at sign-in, in the order of `logon` | no longer |
| `--skip` | leave it to WSL: wsl-spawner neither stops nor starts it | make it wsl-spawner's again |

Starting at sign-in keeps it running, which needs wsl-spawner set up in it; left to WSL, it has none of these. A choice
turned on brings what it needs; one asked both ways (`--logon --keep=false`) is refused. A change inside a distribution
that does not run, on or off, starts it and reads it first, and only what is not so already changes. `--managed` on a
distribution set up by an older version of wsl-spawner puts the newer start script in it, which `check` says to do. It
says what changes and asks first.

### check

`wsl-spawner check [--json]`: looks over what stopping and starting stand on ([what](how-it-works.md#the-check)), and
changes nothing. Each finding is `✓` as it should be, `·` for information only, `!` worth a look, or `✗` not as it
should be, with what to do.

### history

`wsl-spawner history [--all] [--json]`: the stops and starts, newest first, the newest 20 without `--all`, with who
asked (by hand, sign-out, sign-in) and how each distribution went. `--json` gives every entry, oldest first, with each
distribution's steps. `--clear [--yes]` removes the history; with `--json` it prints what is left, no entries.

### hook

| Command | Does |
|:---|:---|
| `hook status [--json]` | whether the sign-out hook is installed and last, what runs at sign-out, and how long Windows waits |
| `hook install [--yes]` | adds it, after every other sign-out script, or moves it last, and raises Windows' wait for the sign-out scripts where it is too short for `stop_limit`; Windows asks for an administrator. The program must be on a fixed disk, in a folder only you and administrators can write to |
| `hook remove [--yes]` | takes it out, and puts back a wait `hook install` raised; the other sign-out scripts stay |

### setup

`wsl-spawner setup`: the wizard, page by page. With flags it writes those settings and opens nothing:
`--language auto|en|ko`, `--theme auto|dark|light`, `--mode tui|cli`, `--resume running|policy|off`,
`--stop-limit SECONDS`. The settings file keeps its notes and anything written by hand.

### tui, uninstall, version

`wsl-spawner tui [--theme auto|dark|light]` opens the screen, whatever the mode.
`wsl-spawner uninstall [--purge] [--yes]` removes wsl-spawner and what it put on this PC ([setup](setup.md#remove)).
`wsl-spawner version` prints the build.

## The screen's keys

| Key | Main screen |
|:---|:---|
| `↑` `↓` `home` `end` | select a distribution |
| `s` | stop it |
| `S` | stop every running distribution of wsl-spawner's, then WSL; with none running, WSL alone |
| `t` | start it |
| `m` / `k` / `l` / `x` | turn: set up in it / kept running / started at sign-in / left to WSL |
| `c` / `h` / `?` | the check / the history / the keys and how it works |
| `r` | read the distributions again |
| `q` | quit |

The bar at the bottom shows the keys that do something to the distribution selected: `s` for one running, `t` for one
stopped, and `m`, `k` and `l` only where systemd runs or is not known yet; `S` while one of wsl-spawner's runs, and `r`,
where the terminal is wide enough for them. Each change asks in a box: `enter` or `y` does it, `esc` or `n` does not.
On the check, history and help screens, `↑` `↓` `pgup` `pgdown` scroll, `esc` goes back, and `r` reads the check
again; on the main screen `esc` does nothing, so going back twice does not quit. While a stop runs, its steps play on
the screen; `enter` goes back once it ends. `ctrl+c` during a stop says the stop goes on to its end, and the screen
closes as it ends. In setup, `enter`, `→`, `l` and `tab` go on,
`←`, `h`, `shift+tab` and `esc` go back a page, `↑` `↓` choose, `space` changes a distribution's level or the hook's
choice, and `q` quits; `esc` on the first page quits as well.

## Exit codes

| Code | Means |
|:---|:---|
| 0 | done |
| 1 | failed, or the question was answered no |
| 2 | asked wrong: a flag, a name WSL does not have or wsl-spawner leaves be, a question with no terminal and no `--yes` |
| 3 | done, but not as it should be: a stop's step ran out of time or failed, or a distribution's stop ended in an error; a check with a `✗` finding; a `hook install` or `hook remove` Windows has not read yet, or whose wait for the sign-out scripts stays as it was |

## JSON

Every `--json` output is one object that starts with `"schema": 1`; a later change says so in it. Lists are `[]`, never
`null`; in `history`, `shutdown`, `steps`, `error` and `version` are left out where they have nothing to say.
`--events` lines are one object each.

| Command | Holds |
|:---|:---|
| `status` | `wsl_version`, `stop_limit_seconds`, `resume`, and `distributions`: each `name`, `default`, `wsl`, `running`, `kind` (`set-up`, `stop-only`, `not-read` for one not running or not answering, `skipped`, `owned`, `wsl1`), `owner`, `at_logon`, `probe_error`, and for one that runs `probe` (with `probe_error` too when it was read in part): `systemd` (systemd's own word, `""` without it, `noanswer` when it runs but did not answer), `interop`, `kept_running`, `set_up`, `docker_live_restore`, `engines`, `containers` (`engine`, `id`, `name`, `state`, `restart_policy`, `stop_timeout_seconds`, `image`, `compose_project`, `auto_remove`), `engine_states` (each engine found: `up`, `down`, `asleep` for a Docker stopped by hand with its socket listening, `foreign` for a `docker` whose daemon is not the distribution's own), `hooks`, `stop_units` (those a stop takes: running now), `stop_units_skipped`, `failed_units`, `last_end` (`how`: `unknown`, `first`, `clean`, `limit`, `failed`, `cut`; `start`; for a `cut` where it can tell, `cause`: `crash`, `update`, `shutdown`, `outside`, and `cause_at`), `last_stop` (`at`, `took_seconds`, `result`, `by`), `to_bring_back`, `resumed` (`engine`, `name`, `what`), `resumed_at`, `complete` (`false` for a look cut off at its limit) |
| `stop` | `results`: each `name`, `result` (`clean`, `limit`, `failed`, `error`, or `busy` for one another stop is stopping, which that stop ends and records), `clean`, `took_seconds`, `steps` (`step`: `hook`, `units`, `docker`, `podman`, `sync`, or `record` when a file of the stop's own could not be written; `hook`, `count`, `code`, `took_seconds`), `error`; `shutdown`; `error`; `detached_from`, the distribution of the terminal a stop went on without |
| `stop --events` | per line: `event` (`line`, `terminate`, `shutdown`, `finished`), `distro`, `done` once a `terminate` or `shutdown` ended, `error`; for a line `kind` (`plan`, `start`, `done`, `end`), `step`, `hook`, `count` on a `plan` or `start`, `code` and `took_seconds` on every `done`, 0 included, and `result` on the `end`; for `finished` the distribution's `result` and `took_seconds`. The results follow, as `--json` prints them |
| `start` | `results`: each `name`, `result` (`started`, `running`, `error`), `error`, `came_back` (`engine`, `name`, `what`: `started`, `running`, `failed`, `exited`, `gone`) |
| `set` | `name`, `changed`, `managed`, `keep`, `logon`, `skip` |
| `check` | `worst` (`ok`, `warn`, `bad`) and `groups`: each `name`, `key`, `findings` (`mark`: `ok`, `info`, `warn`, `bad`; `key`, `text`, `do`, `args`, `do_key`, `do_args`) |
| `hook status` | `installed`, `last`, `command`, `other_scripts`, `other_copies` (each `command`, `program`: `gone`, `there` or `unknown`, `before`), `powershell_scripts`, `powershell_first` (`true` before the others, `false` after, `null` when `psscripts.ini` does not say), `max_wait_seconds` (0: Windows waits as long as the scripts take), `max_wait_before_seconds` (where `hook install` raised it: what `hook remove` puts back) |
| `history` | `entries`: each `at`, `took_seconds`, `what` (`stop`, `start`), `by` (`hand`, `signout`, `logon`), `shutdown`, `distributions` (`name`, `result`, `took_seconds`, `steps`, `error`), `error`, `version` |

## Environment

| Variable | Does |
|:---|:---|
| `WSL_SPAWNER_LANG` | `ko` or `en`, when the settings say `auto` |
| `WSL_SPAWNER_SYMBOLS` | `unicode`, `plain` (ASCII for the marks a console's font lacks) or `ascii` (for a console on an East Asian code page) |
| `NO_COLOR` | any value: no colors in the commands' output |
