# Troubleshooting

Start with `wsl-spawner check`: it reads everything a stop and a start stand on and says what to do about each finding.
`wsl-spawner history --json` has each stop's steps, their exit codes and how long each took.

## A stop ran out of time

The stop ended `limit` (exit code 3, `!` on the screen): a step was still running at `stop_limit`.

- `wsl-spawner history` names the step. A hook: make it quicker, or give it a timeout of its own. A unit: its own
  `TimeoutStopSec` is what it waits. Docker or Podman: the container with the longest stop timeout sets the wait;
  `wsl-spawner check` lists those at or past the limit.
- Raise the limit: `wsl-spawner setup --stop-limit 600`. The sign-out hook still has only what Windows waits for the
  sign-out scripts (600 seconds by default); `check` says when the two do not fit.

## A step failed

The stop ended `failed`: a step exited with a code other than 0 (a hook, the units' stop, Docker's or Podman's); the
other steps still ran. Run the hook by hand as root to see what it says, `journalctl -u UNIT` for a unit, or
`journalctl -u docker` for Docker's daemon. Without systemd, Docker's step fails when `/var/run/docker.pid` names no
running `dockerd`.

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

## The sign-out hook did not run

- `wsl-spawner hook status` says whether it is there and last. Another sign-out script after it runs once WSL has
  stopped; one before it that stops WSL itself leaves it nothing to stop, and `check` names it. PowerShell sign-out
  scripts and a domain's run after the hook: `hook status` lists the PowerShell ones.
- `hook status` lists other copies' hooks. One that runs before this one stops WSL first: take it out with that copy's
  own `wsl-spawner hook remove`, or in gpedit.msc.
- `hook install` ended with exit code 3: the change is made, but Windows has not read it yet; it counts from your next
  sign-in, and `gpupdate /target:user` has Windows read it now.
- `wsl-spawnerw.exe` is gone, as a virus scanner may take it: `hook remove` and `uninstall` still take the hook out;
  `hook install` and `set --logon` need it: install wsl-spawner again.
- The hook and the sign-in program need wsl-spawner on a fixed disk of this PC, not on a network share or in a
  distribution.
- Windows waits for the sign-out scripts up to its policy's limit; past that, it goes on without them.
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

## The screen in a WSL terminal

- `wsl-spawner.exe was not found`: the folder of the install is not on the Windows PATH WSL passes in. Open a new
  terminal after the install, or check `appendWindowsPath` in `/etc/wsl.conf`.
- `this distribution runs no Windows programs`: interop is off in that distribution.
- The marks show as boxes in the Windows console's own window: `WSL_SPAWNER_SYMBOLS=plain`, or Windows Terminal.

## Reporting a problem

[Open an issue](https://github.com/kanggu12/wsl-spawner/issues/new/choose) with `wsl-spawner version`,
`wsl-spawner check`, and for a stop `wsl-spawner history --json`.
