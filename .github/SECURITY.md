# Security

## Reporting a vulnerability

Report a security problem privately, not in a public issue: on GitHub, open the repository's **Security** tab and
choose **Report a vulnerability**.

Say what an attacker can do, how to reproduce it, and which version you ran (a release tag or a commit). The
maintainer replies, agrees on a fix with you, and publishes an advisory with the fixed release. It credits you unless
you ask otherwise.

The latest release and `main` get security fixes.

## What matters most

wsl-spawner runs as you, runs scripts as root inside your distributions, stops and starts their services and
containers, and, with an administrator's permission, writes a sign-out script into the local Group Policy. Report any
of these:

- a distribution's name, a container's name or image, or anything else read from a distribution, that makes it run a
  command it did not build, inside the distribution or on Windows;
- a way to make it stop, start or remove anything but what its docs say: a container, a unit, a file;
- the sign-out hook, the sign-in program or `uninstall` changing anything but wsl-spawner's own entries, or the
  administrator's copy doing anything but adding or removing its own sign-out script;
- `scripts/install.ps1` installing anything but the release it checked, or removing anything but wsl-spawner's own
  files and PATH entry.

It makes no network connection.

## Checking a download

`checksums.txt` lists the SHA-256 of each release zip, and `install.ps1` checks it before installing. GitHub signs the
zips' build provenance: `gh attestation verify <zip> -R kanggu12/wsl-spawner` shows this repository's release workflow
built them from the tagged commit.
