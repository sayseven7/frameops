#!/bin/bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
base="$tmpdir/base"
mkdir "$base"
cp "$root/compose.yaml" "$root/.env.example" "$root/scripts/check-compose.sh" "$base/"

(
  cd "$base"
  bash ./check-compose.sh
)

expect_rejection() {
  local name=$1 service=$2 group=$3 field=$4 mutation=$5 diagnostic=$6
  local case_dir="$tmpdir/$name"
  cp -R "$base" "$case_dir"
  CASE_FILE="$case_dir/compose.yaml" SERVICE="$service" GROUP="$group" FIELD="$field" MUTATION="$mutation" python3 - <<'PY'
import os
from pathlib import Path

path = Path(os.environ["CASE_FILE"])
source = path.read_text()
service = os.environ["SERVICE"]
start = source.index(f"  {service}:\n")
end = source.index("\n  minio:" if service == "postgres" else "\nvolumes:", start)
section = source[start:end]
group_marker = f"        {os.environ['GROUP']}:\n"
group_start = section.index(group_marker)
group_end = section.index(
    "\n        reservations:" if os.environ["GROUP"] == "limits" else "\n    environment:",
    group_start + len(group_marker),
)
group = section[group_start:group_end]
field = os.environ["FIELD"]
line_prefix = f"          {field}: "
line = next((line for line in group.splitlines(keepends=True) if line.startswith(line_prefix)), None)
if line is None:
    raise SystemExit(f"mutation target missing: {service}.{os.environ['GROUP']}.{field}")
mutation = os.environ["MUTATION"]
if mutation == "missing":
    replacement = ""
elif mutation == "relation":
    replacement = f'{line_prefix}"1.01"\n' if field == "cpus" else f"{line_prefix}2G\n"
elif field == "cpus":
    replacement = f'{line_prefix}"{mutation}"\n'
else:
    replacement = f"{line_prefix}{mutation}\n"
mutated_group = group.replace(line, replacement, 1)
mutated_section = section[:group_start] + mutated_group + section[group_end:]
mutated = source[:start] + mutated_section + source[end:]
if mutated == source:
    raise SystemExit("mutation was a no-op")
path.write_text(mutated)
PY

  local rendered
  rendered=$(
    cd "$case_dir"
    FRAMEOPS_MINIO_ROOT_USER_FIFO="$case_dir/user" \
      FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO="$case_dir/password" \
      docker compose --env-file .env.example config --format json
  )
  RENDERED="$rendered" SERVICE="$service" GROUP="$group" FIELD="$field" MUTATION="$mutation" python3 - <<'PY'
import json
import os

resources = json.loads(os.environ["RENDERED"])["services"][os.environ["SERVICE"]]["deploy"]["resources"]
group = resources.get(os.environ["GROUP"], {})
field = os.environ["FIELD"]
mutation = os.environ["MUTATION"]
if mutation == "missing":
    valid = field not in group
elif mutation == "relation":
    valid = float(group[field]) > float(resources["limits"][field])
else:
    expected = float(mutation) if field == "cpus" else {"999M": 999 * 1024 * 1024, "511M": 511 * 1024 * 1024}[mutation]
    valid = float(group[field]) == expected
if not valid:
    raise SystemExit("mutation did not render to the intended unsafe value")
PY

  local output
  if output=$(cd "$case_dir" && bash ./check-compose.sh 2>&1); then
    printf 'FAIL: accepted %s\n' "$name" >&2
    exit 1
  fi
  local diagnostic_found=false
  while IFS= read -r line; do
    [[ "$line" == "- $diagnostic" ]] && diagnostic_found=true
  done <<<"$output"
  if [[ "$diagnostic_found" != "true" ]]; then
    printf 'FAIL: %s missing diagnostic %q\n%s\n' "$name" "$diagnostic" "$output" >&2
    exit 1
  fi
  printf 'PASS: rejects %s\n' "$name"
}

for service in postgres minio; do
  for group in limits reservations; do
    if [[ "$group" == "limits" ]]; then
      expected_cpus=1.00 expected_memory=1G altered_cpus=0.99 altered_memory=999M
    else
      expected_cpus=0.25 expected_memory=512M altered_cpus=0.24 altered_memory=511M
    fi
    expect_rejection "$service-$group-cpus-missing" "$service" "$group" cpus missing "services.$service.deploy.resources.$group.cpus must equal '$expected_cpus'"
    expect_rejection "$service-$group-memory-missing" "$service" "$group" memory missing "services.$service.deploy.resources.$group.memory must equal '$expected_memory'"
    expect_rejection "$service-$group-cpus-altered" "$service" "$group" cpus "$altered_cpus" "services.$service.deploy.resources.$group.cpus must equal '$expected_cpus'"
    expect_rejection "$service-$group-memory-altered" "$service" "$group" memory "$altered_memory" "services.$service.deploy.resources.$group.memory must equal '$expected_memory'"
  done
done
expect_rejection postgres-reservations-cpus-over-limit postgres reservations cpus relation "services.postgres.deploy.resources.reservations.cpus must not exceed its limit"
expect_rejection minio-reservations-memory-over-limit minio reservations memory relation "services.minio.deploy.resources.reservations.memory must not exceed its limit"
