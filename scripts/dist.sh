#!/bin/sh
# Build the release archives into dist/: for each Windows architecture, a zip
# holding wsl-spawner.exe, the same program built with no console
# (wsl-spawnerw.exe, which the sign-in program and the sign-out hook start, so
# no window opens), its WSL launcher (the Linux build of the same
# architecture, which hands over to the .exe), the license, the notices, the
# changelog and a short README.txt, then checksums.txt over every zip.
#
#   scripts/dist.sh 0.1.0
set -eu
# zip records local times: the same time zone everywhere gives the same zip.
export TZ=UTC

version=${1:?usage: scripts/dist.sh VERSION}
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
pkg=github.com/kanggu12/wsl-spawner/internal/version
# A zip is labelled with the commit: changes not committed, and new files
# not added (a new .go file is built all the same), would ship under a
# commit that does not hold them. Ignored files (dist/) do not count.
if [ -n "$(git status --porcelain)" ]; then
  echo "scripts/dist.sh: the work tree differs from HEAD (changed or untracked files); commit or stash first" >&2
  exit 1
fi
# The commit and its date, so `wsl-spawner version` says which build it is.
# The date is the commit's in UTC, as a build from source reports it.
commit=$(git rev-parse --short HEAD)
date=$(git log -1 --date=format-local:%Y-%m-%d --format=%cd)
ldflags="-s -w -X $pkg.Version=v$version -X $pkg.Commit=$commit -X $pkg.Date=$date"
mkdir -p dist
# Only what an earlier run made goes away.
rm -f dist/*.zip dist/checksums.txt dist/install.ps1
for arch in amd64 arm64; do
  stage="dist/stage-$arch"
  mkdir -p "$stage"
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$ldflags" -o "$stage/wsl-spawner.exe" ./cmd/wsl-spawner
  # The same program with no console: Windows opens no window for it.
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$ldflags -H windowsgui" -o "$stage/wsl-spawnerw.exe" ./cmd/wsl-spawner
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "$ldflags" -o "$stage/wsl-spawner" ./cmd/wsl-spawner
  cp LICENSE CHANGELOG.md "$stage/"
  # The notices sit next to LICENSE in the zip, not one folder down as in docs/.
  sed 's#](\.\./LICENSE)#](LICENSE)#' docs/THIRD_PARTY_NOTICES.md >"$stage/THIRD_PARTY_NOTICES.md"
  # The repository's README draws on pictures and pages the zip does not
  # hold; the zip's own says what is in it and where the rest is.
  cat >"$stage/README.txt" <<EOF
wsl-spawner v$version

wsl-spawner.exe   the program, for Windows
wsl-spawnerw.exe  the same program with no console: what the sign-in program
                  and the sign-out hook start, so no window opens
wsl-spawner       the launcher for WSL: run inside a distribution, it hands
                  over to wsl-spawner.exe next to it or on the Windows PATH

Run wsl-spawner in a terminal for the screen, or wsl-spawner --help for the
commands. Put this folder on your PATH to run it from anywhere, in Windows and
in WSL, or install with install.ps1 from the release, which does that itself.
Documentation: https://github.com/kanggu12/wsl-spawner
EOF
  # Files in the zip carry the commit's time, so a rebuild gives the same zip.
  touch -d "@$(git log -1 --format=%ct)" "$stage"/*
  (cd "$stage" && zip -q -X -9 "../wsl-spawner_${version}_windows_$arch.zip" ./*)
  rm -f "$stage"/*
  rmdir "$stage"
done
(cd dist && sha256sum ./*.zip | sed 's# \./# #' > checksums.txt)
cp scripts/install.ps1 dist/
# The Scoop manifest, with this release's zips and their checksums. Its own
# checkver and autoupdate let `scoop update` find later releases.
zip_hash() { awk -v f="wsl-spawner_${version}_windows_$1.zip" '$2 == f { print $1 }' dist/checksums.txt; }
url="https://github.com/kanggu12/wsl-spawner/releases/download"
cat >dist/wsl-spawner.json <<EOF
{
    "version": "$version",
    "description": "Stop WSL services and containers gracefully when Windows shuts down, and bring them back.",
    "homepage": "https://github.com/kanggu12/wsl-spawner",
    "license": "MIT",
    "architecture": {
        "64bit": {
            "url": "$url/v$version/wsl-spawner_${version}_windows_amd64.zip",
            "hash": "$(zip_hash amd64)"
        },
        "arm64": {
            "url": "$url/v$version/wsl-spawner_${version}_windows_arm64.zip",
            "hash": "$(zip_hash arm64)"
        }
    },
    "env_add_path": ".",
    "checkver": "github",
    "autoupdate": {
        "architecture": {
            "64bit": {
                "url": "$url/v\$version/wsl-spawner_\$version_windows_amd64.zip"
            },
            "arm64": {
                "url": "$url/v\$version/wsl-spawner_\$version_windows_arm64.zip"
            }
        },
        "hash": {
            "url": "\$baseurl/checksums.txt"
        }
    },
    "notes": "Run wsl-spawner in a new terminal, in Windows or inside WSL."
}
EOF
# A manifest Scoop could not read would fail people, not this script.
python3 -m json.tool dist/wsl-spawner.json >/dev/null
ls -l dist
