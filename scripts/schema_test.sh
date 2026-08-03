#!/bin/bash
set -euo pipefail

if [[ ! -f .env.example ]]; then
  printf 'missing .env.example\n' >&2
  exit 1
fi

set -a
# shellcheck disable=SC1091
source .env.example
set +a

database_name="frameops_schema_test_$(date +%s)_$RANDOM"
cleanup() {
  docker compose --env-file .env.example exec -T postgres \
    psql -v ON_ERROR_STOP=1 -U "$FRAMEOPS_POSTGRES_USER" -d postgres \
    -c "DROP DATABASE IF EXISTS ${database_name};" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker compose --env-file .env.example up -d --wait postgres minio >/dev/null

docker compose --env-file .env.example exec -T postgres \
  psql -v ON_ERROR_STOP=1 -U "$FRAMEOPS_POSTGRES_USER" -d postgres \
  -c "CREATE DATABASE ${database_name};" >/dev/null

export FRAMEOPS_DATABASE_URL="postgres://${FRAMEOPS_POSTGRES_USER}:${FRAMEOPS_POSTGRES_PASSWORD}@127.0.0.1:${FRAMEOPS_POSTGRES_PORT}/${database_name}?sslmode=disable"

bash scripts/migrate.sh up
go test ./...
bash scripts/migrate.sh status
bash scripts/migrate.sh down-to 0
