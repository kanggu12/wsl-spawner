#!/bin/sh
# Write docs/THIRD_PARTY_NOTICES.md: the license of every module compiled into
# wsl-spawner, of Go itself, and of the font the logo is lettered in. The release zips carry it next to the program. Run it after changing
# dependencies:
#
#   make notices
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

# Only what is linked into the programs is distributed with them, not every
# module go.mod names, so the list comes from the builds themselves: the
# Windows program and the WSL launcher, which links a few modules of its own
# (the terminal's modes on Linux). Each module once, by name.
CGO_ENABLED=0 GOOS=windows go build -trimpath -o "$tmp/wsl-spawner.exe" ./cmd/wsl-spawner
CGO_ENABLED=0 GOOS=linux go build -trimpath -o "$tmp/wsl-spawner" ./cmd/wsl-spawner
for bin in "$tmp/wsl-spawner.exe" "$tmp/wsl-spawner"; do
  go version -m "$bin" | awk '$1 == "dep" { print $2, $3 }'
done | sort -u >"$tmp/deps"

# license_file prints the license file of the module in directory $1.
license_file() {
  for name in LICENSE LICENSE.txt LICENSE.md COPYING LICENCE; do
    if [ -f "$1/$name" ]; then
      echo "$1/$name"
      return 0
    fi
  done
  return 1
}

# kind names the license in file $1 from words every copy of it carries.
kind() {
  if grep -q "Apache License" "$1"; then
    echo "Apache-2.0"
  elif grep -q "Permission is hereby granted, free of charge" "$1"; then
    echo "MIT"
  elif grep -q "Neither the name" "$1"; then
    echo "BSD-3-Clause"
  elif grep -q "Redistribution and use in source and binary forms" "$1"; then
    echo "BSD-2-Clause"
  else
    echo "see the text below"
  fi
}

# The Go release go.mod asks for, not the one this machine has: the file must
# read the same wherever it is generated.
gover=$(awk '$1 == "go" { print $2 }' go.mod)
font_license=docs/assets/Outfit-OFL.txt

out="$tmp/notices.md"
{
  echo "# Third-party notices"
  echo
  echo "wsl-spawner's own code is under the MIT License ([LICENSE](../LICENSE)). Its program is built from the Go modules"
  echo "and the Go standard library below, and its logo is lettered in the Outfit font. Their licenses"
  echo "follow. \`scripts/third-party-notices.sh\` writes this file from what the program is built from (\`make notices\`)."
  echo
  echo "| Component | Version | License | Source |"
  echo "|:---|:---:|:---:|:---|"
  while read -r mod ver; do
    dir=$(go list -m -f '{{.Dir}}' "$mod")
    # A module without a license file must not go unnoticed.
    file=$(license_file "$dir") || { echo "no license file in $mod" >&2; exit 1; }
    echo "| \`$mod\` | $ver | $(kind "$file") | https://pkg.go.dev/$mod |"
  done <"$tmp/deps"
  echo "| Go standard library | $gover | $(kind "$(go env GOROOT)/LICENSE") | https://go.dev |"
  echo "| Outfit (the lettering of the logo) | 1.100 | OFL-1.1 | https://github.com/Outfitio/Outfit-Fonts |"
  while read -r mod ver; do
    dir=$(go list -m -f '{{.Dir}}' "$mod")
    file=$(license_file "$dir")
    echo
    echo "## $mod $ver"
    echo
    echo '```text'
    cat "$file"
    echo '```'
  done <"$tmp/deps"
  echo
  echo "## Go standard library"
  echo
  echo '```text'
  cat "$(go env GOROOT)/LICENSE"
  echo '```'
  echo
  echo "## Outfit 1.100"
  echo
  echo '```text'
  cat "$font_license"
  echo '```'
} >"$out"
mv "$out" docs/THIRD_PARTY_NOTICES.md
echo "wrote docs/THIRD_PARTY_NOTICES.md ($(wc -l <"$tmp/deps" | tr -d ' ') modules)"
