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

expect_web_build_arg_rejection() {
  local name=$1 key=$2 replacement=$3
  local location=${4:-build}
  local case_dir="$tmpdir/$name"
  cp -R "$base" "$case_dir"
  CASE_FILE="$case_dir/compose.yaml" KEY="$key" REPLACEMENT="$replacement" LOCATION="$location" python3 - <<'PY'
import os
from pathlib import Path

path = Path(os.environ["CASE_FILE"])
source = path.read_text()
prefix = f"{'        ' if os.environ['LOCATION'] == 'build' else '      '}{os.environ['KEY']}: "
lines = source.splitlines(keepends=True)
index = next(index for index, line in enumerate(lines) if line.startswith(prefix))
replacement = "" if os.environ["REPLACEMENT"] == "missing" else f'{prefix}"{os.environ["REPLACEMENT"]}"\n'
lines[index] = replacement
mutated = "".join(lines)
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
  RENDERED="$rendered" KEY="$key" REPLACEMENT="$replacement" LOCATION="$location" python3 - <<'PY'
import json
import os

web = json.loads(os.environ["RENDERED"])["services"]["web"]
args = web["build"]["args"] if os.environ["LOCATION"] == "build" else web["environment"]
replacement = os.environ["REPLACEMENT"]
valid = os.environ["KEY"] not in args if replacement == "missing" else args[os.environ["KEY"]] == replacement
if not valid:
    raise SystemExit("mutation did not render to the intended unsafe value")
PY

  local output
  if output=$(cd "$case_dir" && bash ./check-compose.sh 2>&1); then
    printf 'FAIL: accepted %s\n' "$name" >&2
    exit 1
  fi
  local diagnostic="services.web.build.args must pin the Compose internal API origin and marker"
  [[ $location == build ]] || diagnostic="services.web.environment must pin the Compose internal API origin and marker"
  [[ "$output" == *"- $diagnostic"* ]] || {
    printf 'FAIL: %s missing build args diagnostic\n%s\n' "$name" "$output" >&2
    exit 1
  }
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
expect_web_build_arg_rejection web-api-origin-missing FRAMEOPS_API_URL missing
expect_web_build_arg_rejection web-marker-altered FRAMEOPS_COMPOSE_INTERNAL_API 2
expect_web_build_arg_rejection web-runtime-marker-altered FRAMEOPS_COMPOSE_INTERNAL_API 2 environment

expect_api_contract_rejection() {
  local name=$1 mutation=$2 diagnostic=$3
  local case_dir="$tmpdir/$name"
  cp -R "$base" "$case_dir"
  CASE_FILE="$case_dir/compose.yaml" MUTATION="$mutation" python3 - <<'PY'
import os
from pathlib import Path

path = Path(os.environ["CASE_FILE"])
source = path.read_text()
if os.environ["MUTATION"] == "migrate-readiness":
    mutated = source.replace(
        "      migrate:\n        condition: service_completed_successfully\n",
        "      migrate:\n        condition: service_started\n",
        1,
    )
elif os.environ["MUTATION"] == "api-public-port":
    mutated = source.replace(
        "    ports:\n      - \"127.0.0.1:${FRAMEOPS_API_PORT:?set FRAMEOPS_API_PORT in .env}:8080\"\n",
        "    ports:\n      - \"127.0.0.1:${FRAMEOPS_API_PORT:?set FRAMEOPS_API_PORT in .env}:8080\"\n      - \"8082:8080\"\n",
        1,
    )
elif os.environ["MUTATION"] == "minio-auto-restart":
    mutated = source.replace(
        "  minio:\n    restart: \"no\"\n",
        "  minio:\n    restart: unless-stopped\n",
        1,
    )
elif os.environ["MUTATION"] == "api-capabilities":
    mutated = source.replace(
        "      target: api\n    user: \"10001:10001\"\n",
        "      target: api\n    user: \"10001:10001\"\n    cap_add: [ALL]\n",
        1,
    )
elif os.environ["MUTATION"] == "renderer-network":
    mutated = source.replace(
        "    network_mode: none\n",
        "    network_mode: bridge\n",
        1,
    )
elif os.environ["MUTATION"] == "renderer-cpu":
    marker = "  renderer:\n"
    start = source.index(marker)
    mutated = source[:start] + source[start:].replace('          cpus: "1.00"\n', '          cpus: "0.99"\n', 1)
elif os.environ["MUTATION"] == "renderer-memory":
    marker = "  renderer:\n"
    start = source.index(marker)
    mutated = source[:start] + source[start:].replace("          memory: 1G\n", "          memory: 999M\n", 1)
elif os.environ["MUTATION"] == "renderer-pids":
    mutated = source.replace("    pids_limit: 128\n", "    pids_limit: 129\n", 1).replace("          pids: 128\n", "          pids: 129\n", 1)
elif os.environ["MUTATION"] == "renderer-tmpfs":
    mutated = source.replace("      - /tmp:size=256m,mode=1777\n", "      - /tmp\n", 1)
elif os.environ["MUTATION"] == "renderer-writable-root":
    marker = "  renderer:\n"
    start = source.index(marker)
    mutated = source[:start] + source[start:].replace("    read_only: true\n", "    read_only: false\n", 1)
elif os.environ["MUTATION"] == "renderer-capabilities":
    marker = "  renderer:\n"
    start = source.index(marker)
    mutated = source[:start] + source[start:].replace("    cap_drop: [ALL]\n", "", 1)
else:
    raise SystemExit(f"unknown mutation: {os.environ['MUTATION']}")
if mutated == source:
    raise SystemExit("mutation was a no-op")
path.write_text(mutated)
PY

  local output
  if output=$(cd "$case_dir" && bash ./check-compose.sh 2>&1); then
    printf 'FAIL: accepted %s\n' "$name" >&2
    exit 1
  fi
  [[ "$output" == *"- $diagnostic"* ]] || {
    printf 'FAIL: %s missing diagnostic %q\n%s\n' "$name" "$diagnostic" "$output" >&2
    exit 1
  }
  printf 'PASS: rejects %s\n' "$name"
}

expect_api_contract_rejection api-migrate-readiness migrate-readiness "services.api.depends_on.migrate.condition must equal service_completed_successfully"
expect_api_contract_rejection api-public-port api-public-port "services.api must expose exactly one loopback TCP port mapping to target 8080"
expect_api_contract_rejection minio-auto-restart minio-auto-restart "services.minio.restart must equal 'no' while root credentials use one-shot FIFOs"
expect_api_contract_rejection api-capabilities api-capabilities "services.api must not add capabilities or security options"
expect_api_contract_rejection renderer-network renderer-network "services.renderer.network_mode must equal 'none'"
expect_api_contract_rejection renderer-cpu renderer-cpu "services.renderer.deploy.resources.limits.cpus must equal '1.00'"
expect_api_contract_rejection renderer-memory renderer-memory "services.renderer.deploy.resources.limits.memory must equal '1G'"
expect_api_contract_rejection renderer-pids renderer-pids "services.renderer.pids_limit must equal 128"
expect_api_contract_rejection renderer-tmpfs renderer-tmpfs "services.renderer.tmpfs must equal '/tmp:size=256m,mode=1777'"
expect_api_contract_rejection renderer-writable-root renderer-writable-root "services.renderer.read_only must equal true"
expect_api_contract_rejection renderer-capabilities renderer-capabilities "services.renderer.cap_drop must equal ['ALL']"
