#!/bin/bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmp=$(mktemp -d)
image="frameops-dockerignore-test-$$"
container=""
root_marker="$root/.worktrees/root-sentinel-$$"
nested_marker="$root/nested/.worktrees/nested-sentinel-$$"
trap 'docker rm -f "$container" >/dev/null 2>&1 || true; docker image rm -f "$image" >/dev/null 2>&1 || true; rm -f "$root_marker" "$nested_marker"; rmdir "$root/nested/.worktrees" "$root/nested" "$root/.worktrees" >/dev/null 2>&1 || true; rm -rf "$tmp"' EXIT

mkdir -p "$root/.worktrees" "$root/nested/.worktrees"
touch "$root_marker" "$nested_marker"
printf 'FROM scratch\nCOPY . /context\n' >"$tmp/Dockerfile"

docker build --file "$tmp/Dockerfile" --tag "$image" "$root" >/dev/null
container=$(docker create "$image" /)
docker cp "$container:/context/." "$tmp/context"

test ! -e "$tmp/context/.worktrees/$(basename "$root_marker")"
test ! -e "$tmp/context/nested/.worktrees/$(basename "$nested_marker")"
