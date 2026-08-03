#!/bin/bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

make_shim() {
  local name=$1
  local output=$2
  cat >"$tmpdir/$name" <<EOF
#!/bin/bash
printf '%s\n' '$output'
EOF
  chmod +x "$tmpdir/$name"
}

run_case() {
  local description=$1
  local go_output=$2
  local node_output=$3
  local pnpm_output=$4
  local python_output=$5
  local expected_exit=$6

  make_shim go "$go_output"
  make_shim node "$node_output"
  make_shim pnpm "$pnpm_output"
  make_shim python3 "$python_output"
  cat >"$tmpdir/docker" <<'EOF'
#!/bin/bash
if [[ "${1:-}" == "compose" && "${2:-}" == "version" ]]; then
  printf '%s\n' 'Docker Compose version v5.3.1'
  exit 0
fi
exit 1
EOF
  chmod +x "$tmpdir/docker"

  set +e
  PATH="$tmpdir:/usr/bin:/bin" "$root/scripts/check-toolchains.sh" >/dev/null 2>&1
  local actual_exit=$?
  set -e

  if [[ "$actual_exit" -ne "$expected_exit" ]]; then
    printf 'FAIL: %s: expected exit %s, got %s\n' "$description" "$expected_exit" "$actual_exit" >&2
    exit 1
  fi
  printf 'PASS: %s\n' "$description"
}

run_case "accepts approved floors" "go version go1.26.5" "v22.12.0" "10.0.0" "Python 3.13.0" 0
run_case "rejects old Go" "go version go1.25.9" "v22.12.0" "10.0.0" "Python 3.13.0" 1
run_case "rejects old Node" "go version go1.26.5" "v22.11.9" "10.0.0" "Python 3.13.0" 1
run_case "rejects pnpm outside major ten" "go version go1.26.5" "v22.12.0" "9.15.0" "Python 3.13.0" 1
run_case "rejects old Python" "go version go1.26.5" "v22.12.0" "10.0.0" "Python 3.12.9" 1
run_case "rejects Go release candidate" "go version go1.26.5rc1" "v22.12.0" "10.0.0" "Python 3.13.0" 1
run_case "rejects Python release candidate" "go version go1.26.5" "v22.12.0" "10.0.0" "Python 3.13.0rc1" 1
