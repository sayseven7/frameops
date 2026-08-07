#!/bin/bash
set -euo pipefail

state=${FRAMEOPS_LOCAL_STATE_DIR:-"${XDG_STATE_HOME:-$HOME/.local/state}/frameops"}
project="frameops-local-$(printf '%s' "$state" | sha256sum | cut -d ' ' -f1)"
postgres_port=${FRAMEOPS_POSTGRES_PORT:-15432}
minio_port=${FRAMEOPS_MINIO_PORT:-19000}
api_port=${FRAMEOPS_API_PORT:-8081}
ui_port=${FRAMEOPS_UI_PORT:-3000}
umask 077
mkdir -p "$state"
chmod 700 "$state"
environment="$state/runtime.env"
worker="$state/bin/frameops-render"
fops="$state/bin/fops"

require() {
  command -v "$1" >/dev/null || {
    printf 'local runtime requires %s\n' "$1" >&2
    exit 1
  }
}

for command in docker go pnpm curl od ss base64 sha256sum; do
  require "$command"
done

if [[ ${1:-up} == status ]]; then
  docker compose --project-name "$project" --env-file "$environment" ps
  exit
fi

if [[ ${1:-up} == down ]]; then
  [[ -f $environment ]] || exit 0
  fifo_dir=$(mktemp -d "$state/fifo.XXXXXX")
  trap 'rm -rf "$fifo_dir"' EXIT
  mkfifo "$fifo_dir/minio-root-user" "$fifo_dir/minio-root-password"
  FRAMEOPS_MINIO_ROOT_USER_FIFO="$fifo_dir/minio-root-user" \
    FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO="$fifo_dir/minio-root-password" \
    docker compose --project-name "$project" --env-file "$environment" down --timeout 10 --volumes
  rm -f "$state/api.pid" "$state/web.pid"
  exit
fi

port_available() {
  ! ss -ltn "sport = :$1" | grep -q LISTEN
}

port_owned_by_project() {
  local host_port=$1 target_port=$2 container_ids container
  [[ -f $environment ]] || return 1
  container_ids=$(docker compose --project-name "$project" --env-file "$environment" ps -q) || return 1
  [[ -n $container_ids ]] || return 1
  for container in $container_ids; do
    [[ $(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}' "$container") == "$project" ]] || continue
    if docker inspect --format '{{range $port, $bindings := .NetworkSettings.Ports}}{{range $bindings}}{{printf "%s %s %s\n" $port .HostIp .HostPort}}{{end}}{{end}}' "$container" | grep -Fxq "$target_port/tcp 127.0.0.1 $host_port"; then
      return 0
    fi
  done
  return 1
}

for binding in "$postgres_port:5432" "$minio_port:9000" "$api_port:8080" "$ui_port:3000"; do
  host_port=${binding%%:*}
  target_port=${binding##*:}
  if ! port_available "$host_port" && ! port_owned_by_project "$host_port" "$target_port"; then
    printf 'port %s is already listening; local runtime was not started\n' "$host_port" >&2
    exit 1
  fi
done

if ! bash scripts/check-toolchains.sh; then
  printf 'local runtime prerequisites are unavailable; run bash scripts/check-toolchains.sh\n' >&2
  exit 1
fi

secret_file() {
  local path=$1
  local bytes=$2
  if [[ ! -s $path ]]; then
    od -An -N "$bytes" -tx1 /dev/urandom | tr -d ' \n' >"$path"
  fi
  chmod 600 "$path"
}

bootstrap_token() {
  local path=$1
  if [[ ! -s $path ]]; then
    head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n' >"$path"
  fi
  chmod 600 "$path"
}

mkdir -p "$state/bin"
chmod 700 "$state/bin"
secret_file "$state/postgres-password" 16
secret_file "$state/minio-root-user" 8
secret_file "$state/minio-root-password" 32
bootstrap_token "$state/bootstrap-token"
secret_file "$state/bootstrap-password" 16

postgres_password=$(<"$state/postgres-password")
minio_user=$(<"$state/minio-root-user")
minio_password=$(<"$state/minio-root-password")
cat >"$environment" <<EOF
FRAMEOPS_POSTGRES_USER=frameops_local
FRAMEOPS_POSTGRES_PASSWORD=$postgres_password
FRAMEOPS_POSTGRES_DB=frameops_local
FRAMEOPS_POSTGRES_PORT=$postgres_port
FRAMEOPS_MINIO_PORT=$minio_port
FRAMEOPS_EVIDENCE_S3_ENDPOINT=http://127.0.0.1:$minio_port
FRAMEOPS_EVIDENCE_S3_BUCKET=frameops-evidence-locked
FRAMEOPS_EVIDENCE_S3_REGION=us-east-1
FRAMEOPS_EVIDENCE_S3_ACCESS_KEY=$minio_user
FRAMEOPS_EVIDENCE_S3_SECRET_KEY=$minio_password
FRAMEOPS_OBJECT_RETENTION_DAYS=365
FRAMEOPS_DATABASE_URL=postgres://frameops_local:$postgres_password@postgres:5432/frameops_local?sslmode=disable
FRAMEOPS_HTTP_ADDR=127.0.0.1:$api_port
FRAMEOPS_PDF_WORKER=$worker
FRAMEOPS_API_URL=http://127.0.0.1:$api_port
FRAMEOPS_API_PORT=$api_port
FRAMEOPS_UI_PORT=$ui_port
EOF
chmod 600 "$environment"

fifo_dir=$(mktemp -d "$state/fifo.XXXXXX")
trap 'rm -rf "$fifo_dir"' EXIT
minio_user_fifo="$fifo_dir/minio-root-user"
minio_password_fifo="$fifo_dir/minio-root-password"
mkfifo "$minio_user_fifo" "$minio_password_fifo"
(
  cat "$state/minio-root-user" >"$minio_user_fifo"
) &
(
  cat "$state/minio-root-password" >"$minio_password_fifo"
) &
FRAMEOPS_MINIO_ROOT_USER_FIFO=$minio_user_fifo \
FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO=$minio_password_fifo \
docker compose --project-name "$project" --env-file "$environment" up -d postgres minio

for attempt in {1..30}; do
  if docker compose --project-name "$project" --env-file "$environment" exec -T postgres psql -U frameops_local -d frameops_local -c 'SELECT 1' >/dev/null && curl --fail --silent --output /dev/null "http://127.0.0.1:$minio_port/minio/health/live"; then
    break
  fi
  if [[ $attempt == 30 ]]; then
    printf 'PostgreSQL or MinIO did not become ready\n' >&2
    exit 1
  fi
  sleep 1
done

FRAMEOPS_MINIO_ROOT_USER_FIFO=$minio_user_fifo \
FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO=$minio_password_fifo \
  docker compose --project-name "$project" --env-file "$environment" up --build --wait
set -a
# shellcheck source=/dev/null
source "$environment"
set +a
go build -o "$fops" ./cmd/fops

# Bootstrap is transactionally consumed by the database and removes its token
# only after commit. The local marker prevents this launcher from retrying it.
if [[ ! -e $state/bootstrap-complete ]]; then
  FRAMEOPS_DATABASE_URL="postgres://frameops_local:$postgres_password@127.0.0.1:$postgres_port/frameops_local?sslmode=disable" "$fops" bootstrap-first-admin \
    --token-file "$state/bootstrap-token" \
    --password-file "$state/bootstrap-password" \
    --organization 'FrameOPS Local' \
    --name 'Local Admin' \
    --email 'admin@frameops.local'
  : >"$state/bootstrap-complete"
  chmod 600 "$state/bootstrap-complete"
fi

FRAMEOPS_OBJECT_LOCK_PROOF=1 go test ./internal/store/objectstore -run '^TestMinIOObjectLockProof$' -count=1
for attempt in {1..30}; do
  if curl --fail --silent --output /dev/null "http://127.0.0.1:$ui_port/"; then
    break
  fi
  if [[ $attempt == 30 ]]; then
    printf 'UI did not become healthy; inspect docker compose logs\n' >&2
    exit 1
  fi
  sleep 1
done
printf 'local runtime started: API http://127.0.0.1:%s, UI http://localhost:%s\n' "$api_port" "$ui_port"
