# Contributing to wsl-spawner

wsl-spawner stops the services and containers people run, as root, and writes into the local Group Policy, so a change
comes with a test showing it still stops only what it should, with the time it should. Almost no work needs a real
distribution: the scripts it runs inside one are tested against made-up roots, the commands against a made-up WSL and a
made-up PC, and the screen on the demo's PC (`--demo`).

## How the pieces fit

The code sits in layers. Imports go down, never up; `internal/layers_test.go` fails when one goes up.

| Layer | Packages | |
|:---|:---|:---|
| cmd | `cmd/wsl-spawner` | the program; in WSL, the launcher that hands each command to `wsl-spawner.exe` |
| front | `cli`, `tui`, `api`, `ui`, `i18n` | the commands, the screen and setup, the commands' JSON and its client, colors and marks, words |
| app | `app`, `app/demo` | the settings, WSL and this PC put together; the demo's made-up PC |
| engine | `distros`, `inside`, `stop`, `setup`, `check`, `history` | reading the distributions, the scripts run inside one, a stop, setting up and taking out, the check, the history |
| settings | `config` | `config.toml`, read, and written as a person wrote it |
| machine | `wsl`, `winapi`, `gpo`, `eventlog` | `wsl.exe`, Windows' calls, the local Group Policy's files and the policy's wait, Windows' event logs |
| core | `version`, `plain` | the build's version; text read from a distribution made safe to show |

Only `engine/inside` holds what runs inside a distribution: the probe, the stop script, the start script, and setting up
and taking out. What goes into a script is a number or a plain word, checked before it goes in; nothing read from a
distribution does. `engine/stop` alone runs a stop: the `stop` command, the screen through it, and the sign-out hook's
`logoff` all go through it. Everything on Windows but WSL goes through the App's `PC`. The screen reads and changes
nothing but through the commands' JSON (`api.Client`). [Internals](../docs/internals.md) has what runs where and why,
and [how it works](../docs/how-it-works.md) what a stop and a start do.

## Build and try it

You need Go (the version in `go.mod`; `go` fetches it if yours is older), shellcheck, Docker for the demo image, and a
Windows PC with WSL 2 to run it for real. Development works from inside WSL:

```sh
git clone https://github.com/kanggu12/wsl-spawner && cd wsl-spawner
make build
dist/wsl-spawner.exe status        # reads only
go run ./cmd/wsl-spawner --demo    # the screen on a made-up PC, on any system
```

`status`, `check`, `history` and the screen change nothing until you confirm a change. To try a stop, stop only a
throwaway distribution:

```sh
docker export "$(docker create alpine:3 true)" -o /tmp/alpine.tar
wsl.exe --import wsp-test 'C:\wsp-test' "$(wslpath -w /tmp/alpine.tar)" --version 2
wsl.exe -d wsp-test -- sleep 600 &      # a distribution runs while something does
dist/wsl-spawner.exe stop wsp-test --yes
wsl.exe --unregister wsp-test
```

Setting up, keeping running and starting again need systemd in the distribution. Leave the sign-out hook and the
sign-in program to a PC you mean to change: `hook install` writes the local Group Policy, and `set --logon` the sign-in
programs.

## Run the tests

```sh
make check          # gofmt, go vet and staticcheck for Linux and Windows, shellcheck, go test -race
make test-windows   # from WSL: every package's tests again as Windows programs
make fuzz           # the probe's reader, 30s; a failing input lands in testdata/fuzz
make vuln           # govulncheck, for Linux and Windows
make demo           # the demo image from the work tree, opened in this terminal
```

A screen test draws every state of the screen and every page of setup at sizes from 160x40 to 40x12, in English and
Korean, and fails if a line is not exactly the screen's width or the screen not its height. `--snapshot 120x30 --keys
"c"` prints one frame of the demo after the keys, `--snapshot 100x30 --setup` setup's first page, and
`--snapshot 100x24 --cli` CLI mode's overview.

A few tests hold what is hardest to see go wrong:

- `internal/engine/inside/main_test.go`: the scripts run under the machine's own `sh`, against a temporary folder that
  stands for `/`. The package's tests fail when a path of wsl-spawner's appears on the machine itself while they run.
- `TestTheScriptsPickNoTextApartWithTools` and `TestProbeNeedsNothingButTheShell` (engine/inside): the scripts run
  with nothing but the shell on the PATH, as in a small distribution with no `awk` or `sed`.
- `TestEveryDistributionStopsAndThenWSLEndsIt` and `TestStoppedWaitingTheDistributionStillGoesDown` (engine/stop): a
  stop asked for always ends with the distribution stopped, when a step fails, runs out of time, or Ctrl+C comes.
- `TestAddPutsTheHookLastAndKeepsTheRest`, `TestKeysAreReadAsWindowsReadsThem`, `TestScriptsAfterAMissingIndexStayUnrun`
  and `TestScriptsIsReadOnlyWhereItIsCertain` (machine/gpo): every other sign-out script stays as Windows runs it, and a
  file wsl-spawner cannot read for certain as Windows does is left as it is. `TestAChangeIsMadeWholeOrNotAtAll`: the
  two policy files change together, or neither does.

No test reaches the PC it runs on. A test's App has a made-up PC whose every call fails unless the test gave it.
`api.Exe`, `winapi.StartDetached`, `winapi.RunElevated`, `winapi.DeleteAfterExit` and the sign-in program's and PATH's
calls refuse inside a test binary, so no test starts the program, `cmd.exe` or a window, or changes the PC; on
Windows, an empty folder variable reaches no real folder in a test. The launcher's tests give it only fake programs
without the execute bit: a real one built for another processor would put Windows' dialog on the desktop.

## Make a change

- **A step in a stop**: in `internal/engine/inside/stop.sh`, printed as a `step` line the Windows side reads, with a
  word for it in both catalogs (`step.<name>`), a test against a made-up root, and the order in
  [how it works](../docs/how-it-works.md#a-stop).
- **Something the probe reads**: a `KEY=VALUE` line in `probe.sh` and its field in `inside.Probe`; a key the Windows
  side does not know is skipped, so an older program reads a newer script.
- **Words people see**: every word goes through `i18n.T`, in `internal/front/i18n/catalog.go`, in English and Korean.
  `TestEveryWordTheScreenNamesIsInBothCatalogs` fails for a key missing from either.
- **A finding in the check**: in `internal/engine/check`, with what to do about it, in both catalogs.

## Style

Comments say why a line is there, nearly line by line. Docs and comments are in English. A commit says what changed for
the person running wsl-spawner. `CHANGELOG.md` gets a line under `[Unreleased]` for anything they would notice.

## Releases

A release is a `## [X.Y.Z] - date` section in `CHANGELOG.md` with its link at the bottom, `make badges`, a commit, and
an annotated `vX.Y.Z` tag on it once CI is green. Versions follow [semantic versioning](https://semver.org/): additions
raise the minor number (0.2.0), fixes alone raise the patch number (0.1.1). Before 1.0 a minor release may also change
how something works; its notes say what. The section becomes the release notes: `.github/workflows/release.yml`
refuses a tag without one, and `internal/changelog_test.go` checks the file's shape and that the README's badge names
the newest release. A tag with a hyphen (`v0.2.0-rc.1`) goes out as a pre-release, with a section of its own.

The workflow runs all of CI. Then `scripts/dist.sh` builds the zips, `checksums.txt` and the Scoop manifest, the zips
get build provenance, and the demo image goes to `ghcr.io`. Last, `.github/workflows/installers.yml` installs the
published release as people do. `scripts/dist.sh` builds only from a work tree that matches its commit, since the zips
carry that commit's name.

### A WSL release

The weekly canary (`.github/workflows/canary.yml`) runs the stops and starts of `.github/workflows/wsl.yml` on WSL's
newest pre-release, so a WSL release that changes how it stops a distribution usually shows up there first. It can also
be started by hand from the Actions tab. A failure there blocks nothing: it says what to look at, and the fix goes out
as any other.

## Report a problem

[Open an issue](https://github.com/kanggu12/wsl-spawner/issues). Its bug form asks for the output of
`wsl-spawner check`, `wsl-spawner history --json` for a stop that went wrong, and the versions. Report a security
problem privately: see [SECURITY](SECURITY.md).
