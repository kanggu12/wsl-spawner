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

## Where it runs for other users

The sign-out hook is in the local Group Policy, which applies to every user of the PC: Windows runs it as whoever signs
out, and it starts the program of the user who installed it. `hook install` therefore refuses a program that anyone
beyond its installer, the system and the administrators can change, or whose folder or a folder above can be changed,
and `check` says it of a hook already installed. The elevated copy that changes the policy is the unsigned program you
installed: Windows' prompt shows no publisher, so check that the path it names is the one you installed before you
approve it.

## Checking a download

`checksums.txt` lists the SHA-256 of each release zip, and `install.ps1` checks it before installing. GitHub signs the
build provenance of every file of a release (the zips, `checksums.txt`, `install.ps1` and the Scoop manifest, from
0.1.2 on; the zips alone before):
`gh attestation verify <file> -R kanggu12/wsl-spawner` shows this repository's release workflow built it from the
tagged commit. A file of a published release is never replaced.
