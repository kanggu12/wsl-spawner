# Troubleshooting

Start with `wsl-spawner check`: it reads everything a stop and a start stand on and says what to do about each finding.
`wsl-spawner history --json` has each stop's steps, their exit codes and how long each took.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/trouble-dark.svg">
  <img alt="What wsl-spawner says, and where Troubleshooting reads it. wsl-spawner check: its last stop ran out of time in a step (docker), read under A stop ran out of time; a step of its last stop failed (hook 10-backup-db), under A step failed; WSL was updated at 11-10 22:35, which ends every distribution, under The last start ended without its time; sign-out hook installed, but scripts after it run once WSL has stopped, under The sign-out hook did not run; kept running needs Windows programs, which do not run in it, under A distribution does not stay running. On the screen after a start: started, web brought back, db did not start, under A container did not come back. wsl-spawner set Work --managed: setting wsl-spawner up in Work: systemd does not run in it, under Set up fails. wsl-spawner in a WSL terminal: wsl-spawner.exe was not found next to this file or on the Windows PATH, under The screen in a WSL terminal." src="assets/trouble-light.svg">
</picture>

## A stop ran out of time

The stop ended `limit` (exit code 3, `!` on the screen): a step was still running at `stop_limit`.

- `wsl-spawner history` names the step after the result, as `out of time: hook 10-backup`. A hook: make it quicker, or give it a timeout of its own. A unit: its own
  `TimeoutStopSec` is what it waits. Docker or Podman: the container with the longest stop timeout sets the wait;
  `wsl-spawner check` lists those at or past the limit.
- Raise the limit: `wsl-spawner setup --stop-limit 600`. The sign-out hook still has only what Windows waits for the
  sign-out scripts (600 seconds by default); `check` says when the two do not fit.

## A step failed

The stop ended `failed`: a step exited with a code other than 0 (a hook, the units' stop, Docker's or Podman's, or the
stop's own notes in `/var/lib/wsl-spawner`, which a full disk refuses); the other steps still ran. Run the hook by hand
as root to see what it says, `journalctl -u UNIT` for a unit, or `journalctl -u docker` for Docker's daemon. A `docker`
whose daemon is not the distribution's, with no unit of Docker's under systemd and no `/var/run/docker.pid` naming a
running `dockerd`, is Docker Desktop's or another engine's: it has no step.

## A container did not come back

- `wsl-spawner status` says whether wsl-spawner is set up in the distribution: without it, only containers with a
  restart policy come back, by Docker's or Podman's own start. `wsl-spawner set NAME --managed`.
- With `resume = "policy"`, a container with no restart policy does not come back; `check` lists them.
- `came_back` in `wsl-spawner start --json`, and the screen's details, say what became of each: `failed` when its
  start failed, `exited` when it ended at once (its own logs say why), `gone` when it was removed since.
- Docker's daemon must start with the distribution: `systemctl enable docker` inside.
- Its engine did not answer when the distribution started: its containers stay to bring back (`to_bring_back` in
  `status --json`). `systemctl restart wsl-spawner.service` inside brings them back once the engine answers;
  `systemctl status wsl-spawner.service` names the engine.

## The last start ended without its time

`check` says `✗ its last start ended without wsl-spawner's stop`: WSL or Windows ended the distribution with the few
seconds WSL gives. Something stopped WSL without a sign-out: an update of WSL, `wsl --shutdown` or `wsl --terminate`
typed by hand, a crash, or a sign-out with no hook. `wsl-spawner hook install` covers sign-out, shutdown and restart;
stop it with `wsl-spawner stop` before stopping WSL by hand.

Where it can tell, the line after says what it was, and when: Windows did not shut down cleanly (a crash, a hang or a
power cut), WSL was updated, Windows shut down or restarted, or you signed out, without wsl-spawner's stop; or it was
stopped outside wsl-spawner. The distribution's journal says when that start ended, and how: asked to power off, as WSL
asks, or without a word. Windows' System and Application logs say what happened then, which anyone signed in at the PC
may read. A distribution whose journal does not keep the last start (one kept in memory only, or none) says nothing
more. It is a hint, not proof: any program you run may write an event of Windows Installer's.

## The sign-out hook did not run

- `wsl-spawner hook status` says whether it is there and last. Another sign-out script after it runs once WSL has
  stopped; one before it that stops WSL itself leaves it nothing to stop, and `check` names it. PowerShell sign-out
  scripts and a domain's run after the hook: `hook status` lists the PowerShell ones.
- `hook status` lists other copies' hooks. One that runs before this one stops WSL first: take it out with that copy's
  own `wsl-spawner hook remove`, or in gpedit.msc > User Configuration > Windows Settings > Scripts (Logon/Logoff) > Logoff.
- `hook install` ended with exit code 3: the change is made, but Windows has not read it yet; it counts from your next
  sign-in, and `gpupdate /target:user` has Windows read it now.
- `wsl-spawnerw.exe` is gone, as a virus scanner may take it: `hook remove` and `uninstall` still take the hook out;
  `hook install` and `set --logon` need it: install wsl-spawner again.
- The hook and the sign-in program need wsl-spawner on a fixed disk of this PC, not on a network share or in a
  distribution. The hook also needs it where only you and administrators can write: `hook install` names who else may
  change the program or a folder above it, and `icacls FOLDER` shows the permissions to take away. A standard user's
  hook is an administrator's install: [For a standard user](setup.md#for-a-standard-user).
- Windows waits for the sign-out scripts up to its policy's limit; past that, it goes on without them. `check` says
  when it is too short for `stop_limit`, and `hook install` raises it; a domain's policy may set it back.
- On Windows Home, the local Group Policy's sign-out scripts may not run.
- `wsl-spawner history` lists each stop by sign-out: none after a sign-out means the hook did not run. One that says
  the settings did not load stopped with the defaults: `wsl-spawner status` names what to put right.

## A distribution does not stay running

- `wsl-spawner status`: its row must say `kept running` or `at sign-in`. `wsl-spawner set NAME --keep`.
- The keep-running service holds a Windows program: `[interop] enabled=true` in `/etc/wsl.conf`, then
  `wsl --terminate NAME`. `check` says so.
- `systemctl status wsl-spawner-keepalive` inside says why the service stopped.

## Set up fails

- `systemd does not run in it`: `[boot] systemd=true` in `/etc/wsl.conf`, then `wsl --terminate NAME`.
- `Windows programs do not run in it`: needed for `--keep`; see above.

## WSL does not answer

`WSL did not answer in time`: WSL may be starting or updating. Try again in a minute. wsl-spawner never starts a
distribution only to read it.

`read in part, as its look ran out of time`: what runs in the distribution was read up to its limit, often a container
engine whose daemon does not answer. What was read stands; `docker info` inside says whether the daemon answers.

## The screen in a WSL terminal

- `wsl-spawner.exe was not found`: the folder of the install is not on the Windows PATH WSL passes in. Open a new
  terminal after the install, or check `appendWindowsPath` in `/etc/wsl.conf`.
- `this distribution runs no Windows programs`: interop is off in that distribution.
- The marks show as boxes in the Windows console's own window: `WSL_SPAWNER_SYMBOLS=plain`, or Windows Terminal.

## Reporting a problem

[Open an issue](https://github.com/kanggu12/wsl-spawner/issues/new/choose) with `wsl-spawner version`,
`wsl-spawner check`, and for a stop `wsl-spawner history --json`.
