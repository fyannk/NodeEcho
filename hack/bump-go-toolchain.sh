#!/bin/sh
# Moves the Go toolchain to the latest patch release of its minor version,
# in go.mod and in the Dockerfile's builder image together, so
# check-go-version.sh keeps agreeing. Dependabot moves the image alone and
# leaves go.mod behind; patch releases are where Go ships its security
# fixes, so this is the update that cannot wait for a human.
#
# Prints the new version when it changed the files, nothing when the
# toolchain is current or the image for the new release is not published
# yet (the next run retries). A new minor version is left to a person: it
# moves the documented "Go 1.x" and can change the linters' behavior.
#
# Needs curl, jq and docker buildx (for the image digest).
set -eu

current=$(sed -n 's/^toolchain go\([0-9][0-9.]*\)$/\1/p' go.mod)
[ -n "$current" ] || { echo "no toolchain directive in go.mod" >&2; exit 2; }
minor=$(echo "$current" | cut -d. -f1,2)

latest=$(curl -fsS 'https://go.dev/dl/?mode=json&include=all' |
  jq -r --arg m "go$minor." '.[] | select(.stable) | .version | select(startswith($m))' |
  sed 's/^go//' | sort -V | tail -n1)
[ -n "$latest" ] || { echo "no stable release of Go $minor on go.dev" >&2; exit 2; }

# Versions sort as versions: only ever move forward.
newest=$(printf '%s\n%s\n' "$current" "$latest" | sort -V | tail -n1)
[ "$newest" != "$current" ] || exit 0

digest=$(docker buildx imagetools inspect "golang:$latest" 2>/dev/null |
  sed -n 's/^Digest: *\(sha256:[0-9a-f]\{64\}\)$/\1/p') || true
if [ -z "$digest" ]; then
  echo "golang:$latest is not published yet" >&2
  exit 0
fi

sed -i "s/^toolchain go$current\$/toolchain go$latest/" go.mod
sed -i "s|golang:$current@sha256:[0-9a-f]\{64\}|golang:$latest@$digest|" Dockerfile
./hack/check-go-version.sh >&2
echo "$latest"
