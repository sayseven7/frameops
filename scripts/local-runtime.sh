#!/bin/bash
set -euo pipefail

state=${FRAMEOPS_LOCAL_STATE_DIR:-"${XDG_STATE_HOME:-$HOME/.local/state}/frameops"}
project="frameops-local-$(printf '%s' "$state" | sha256sum | cut -d ' ' -f1)"
umask 077
mkdir -p "$state"
chmod 700 "$state"
environment="$state/runtime.env"
worker="$state/bin/frameops-render"
api="$state/bin/frameops-api"
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

port_available() {
  ! ss -ltn "sport = :$1" | grep -q LISTEN
}

for port in 15432 19000 18081 13000; do
  if ! port_available "$port"; then
    printf 'port %s is already listening; local runtime was not started\n' "$port" >&2
    exit 1
  fi
done

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
FRAMEOPS_POSTGRES_PORT=15432
FRAMEOPS_MINIO_PORT=19000
FRAMEOPS_EVIDENCE_S3_ENDPOINT=http://127.0.0.1:19000
FRAMEOPS_EVIDENCE_S3_BUCKET=frameops-evidence-locked
FRAMEOPS_EVIDENCE_S3_REGION=us-east-1
FRAMEOPS_EVIDENCE_S3_ACCESS_KEY=$minio_user
FRAMEOPS_EVIDENCE_S3_SECRET_KEY=$minio_password
FRAMEOPS_OBJECT_RETENTION_DAYS=365
FRAMEOPS_DATABASE_URL=postgres://frameops_local:${postgres_password}@127.0.0.1:15432/frameops_local?sslmode=disable
FRAMEOPS_HTTP_ADDR=127.0.0.1:18081
FRAMEOPS_PDF_WORKER=$worker
FRAMEOPS_API_URL=http://127.0.0.1:18081
FRAMEOPS_UI_PORT=13000
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
  if docker compose --project-name "$project" --env-file "$environment" exec -T postgres pg_isready -U frameops_local -d frameops_local >/dev/null && curl --fail --silent --output /dev/null http://127.0.0.1:19000/minio/health/live; then
    break
  fi
  if [[ $attempt == 30 ]]; then
    printf 'PostgreSQL or MinIO did not become ready\n' >&2
    exit 1
  fi
  sleep 1
done

set -a
source "$environment"
set +a
bash scripts/migrate.sh up
go build -o "$worker" ./cmd/frameops-render
go build -o "$api" ./cmd/frameops-api
go build -o "$fops" ./cmd/fops

# Bootstrap is transactionally consumed by the database and removes its token
# only after commit. The local marker prevents this launcher from retrying it.
if [[ ! -e $state/bootstrap-complete ]]; then
  "$fops" bootstrap-first-admin \
    --token-file "$state/bootstrap-token" \
    --password-file "$state/bootstrap-password" \
    --organization 'FrameOPS Local' \
    --name 'Local Admin' \
    --email 'admin@frameops.local'
  : >"$state/bootstrap-complete"
  chmod 600 "$state/bootstrap-complete"
fi

FRAMEOPS_OBJECT_LOCK_PROOF=1 go test ./internal/store/objectstore -run '^TestMinIOObjectLockProof$' -count=1
"$api" >"$state/api.log" 2>&1 &
echo $! >"$state/api.pid"
for attempt in {1..30}; do
  if curl --fail --silent --output /dev/null http://127.0.0.1:18081/health; then
    break
  fi
  if [[ $attempt == 30 ]]; then
    printf 'API did not become healthy; inspect %s\n' "$state/api.log" >&2
    exit 1
  fi
  sleep 1
done

# Keep Secure cookies in production mode. Browsers treat localhost as a secure
# local context; the UI proxies same-origin /v1 to loopback API without CORS.
FRAMEOPS_API_URL=http://127.0.0.1:18081 pnpm --filter @frameops/web dev -- --hostname 127.0.0.1 --port "$FRAMEOPS_UI_PORT" >"$state/web.log" 2>&1 &
echo $! >"$state/web.pid"
printf 'local runtime started: API http://127.0.0.1:18081, UI http://localhost:13000\n'
