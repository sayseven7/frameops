#!/bin/bash
set -euo pipefail

status=0

version_at_least() {
  local actual=$1 required=$2
  local IFS=.
  local -a actual_parts required_parts
  read -r -a actual_parts <<<"$actual"
  read -r -a required_parts <<<"$required"

  for index in 0 1 2; do
    local a=${actual_parts[index]:-0}
    local r=${required_parts[index]:-0}
    if ((10#$a > 10#$r)); then
      return 0
    fi
    if ((10#$a < 10#$r)); then
      return 1
    fi
  done
  return 0
}

check_floor() {
  local label=$1 actual=$2 required=$3 remediation=$4
  if version_at_least "$actual" "$required"; then
    printf 'OK   %-14s %s\n' "$label" "$actual"
  else
    printf 'FAIL %-14s detected %s; requires >= %s. %s\n' "$label" "$actual" "$required" "$remediation" >&2
    status=1
  fi
}

if go_output=$(go version 2>/dev/null); then
  if [[ $go_output =~ go([0-9]+\.[0-9]+\.[0-9]+)($|[[:space:]]) ]]; then
    check_floor "Go" "${BASH_REMATCH[1]}" "1.26.5" "Install Go 1.26.5 or newer."
  else
    printf 'FAIL Go             could not parse version from: %s\n' "$go_output" >&2
    status=1
  fi
else
  printf 'FAIL Go             command not found. Install Go 1.26.5 or newer.\n' >&2
  status=1
fi

if node_output=$(node --version 2>/dev/null); then
  node_version=${node_output#v}
  if [[ $node_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    check_floor "Node.js" "$node_version" "22.12.0" "Install Node.js 22.12.0 or newer."
  else
    printf 'FAIL Node.js        could not parse version from: %s\n' "$node_output" >&2
    status=1
  fi
else
  printf 'FAIL Node.js        command not found. Install Node.js 22.12.0 or newer.\n' >&2
  status=1
fi

if pnpm_output=$(pnpm --version 2>/dev/null); then
  if [[ $pnpm_output =~ ^10\.[0-9]+\.[0-9]+$ ]]; then
    printf 'OK   %-14s %s\n' "pnpm" "$pnpm_output"
  else
    printf 'FAIL pnpm           detected %s; requires major version 10. Install pnpm 10.x.\n' "$pnpm_output" >&2
    status=1
  fi
else
  printf 'FAIL pnpm           command not found. Install pnpm 10.x.\n' >&2
  status=1
fi

if python_output=$(python3 --version 2>/dev/null); then
  if [[ $python_output =~ ^Python\ ([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
    check_floor "Python" "${BASH_REMATCH[1]}" "3.13.0" "Install Python 3.13 or newer."
  else
    printf 'FAIL Python         could not parse version from: %s\n' "$python_output" >&2
    status=1
  fi
else
  printf 'FAIL Python         command not found. Install Python 3.13 or newer.\n' >&2
  status=1
fi

if docker_compose_output=$(docker compose version 2>/dev/null); then
  if [[ $docker_compose_output =~ ^Docker\ Compose\ version\ v?([0-9]+\.[0-9]+\.[0-9]+)(\+[0-9A-Za-z.~+-]+)?$ ]]; then
    check_floor "Docker Compose" "${BASH_REMATCH[1]}" "2.20.0" "Install Docker Compose 2.20.0 or newer."
  else
    printf 'FAIL Docker Compose could not parse version from: %s\n' "$docker_compose_output" >&2
    status=1
  fi
else
  printf 'FAIL Docker Compose command unavailable. Install Docker Compose 2.20.0 or newer.\n' >&2
  status=1
fi

if golangci_output=$(golangci-lint --version 2>/dev/null); then
  printf 'OK   %-14s %s\n' "golangci-lint" "$golangci_output"
else
  printf 'FAIL golangci-lint command not found. Install golangci-lint v2.9.0 built with Go 1.26.5.\n' >&2
  status=1
fi

exit "$status"
