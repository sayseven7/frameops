#!/bin/bash
set -euo pipefail

if [[ -z ${FRAMEOPS_DATABASE_URL:-} ]]; then
  printf 'FRAMEOPS_DATABASE_URL must be set explicitly.\n' >&2
  exit 1
fi

command=${1:-}
case "$command" in
  up|status)
    if (($# != 1)); then
      printf 'usage: %s {up|status|down-to VERSION}\n' "$0" >&2
      exit 2
    fi
    args=("$command")
    ;;
  down-to)
    if (($# != 2)) || [[ ! $2 =~ ^[0-9]+$ ]]; then
      printf 'usage: %s down-to VERSION\n' "$0" >&2
      exit 2
    fi
    args=(down-to "$2")
    ;;
  *)
    printf 'usage: %s {up|status|down-to VERSION}\n' "$0" >&2
    exit 2
    ;;
esac

exec go run ./cmd/frameops-migrate "${args[@]}"
