#!/bin/bash
set -euo pipefail
# commands: backup|restore

state=${FRAMEOPS_LOCAL_STATE_DIR:-"${XDG_STATE_HOME:-$HOME/.local/state}/frameops"}
environment="$state/runtime.env"
project="frameops-local-$(printf '%s' "$state" | sha256sum | cut -d ' ' -f1)"
postgres_image='postgres:18.4@sha256:3a82e1f56c8f0f5616a11103ac3d47e632c3938698946a7ad26da0df1334744a'

usage() {
  printf 'usage: %s {backup DIRECTORY|restore DIRECTORY}\n' "$0" >&2
  exit 2
}

fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

[[ $# == 2 ]] || usage
[[ -r $environment ]] || fail "local runtime state is unavailable: $environment"
for command in docker sha256sum tar mktemp; do
  command -v "$command" >/dev/null || fail "recovery requires $command"
done
if ! grep -Fxq "    image: $postgres_image" compose.yaml; then
  fail "compose.yaml does not contain the approved PostgreSQL image"
fi

compose_id() {
  local service=$1 id
  id=$(docker compose --project-name "$project" --env-file "$environment" ps -q "$service")
  [[ -n $id && $(wc -w <<<"$id") == 1 ]] || fail "expected one running $service container in $project"
  printf '%s\n' "$id"
}

postgres=$(compose_id postgres)
minio=$(compose_id minio)
api=$(compose_id api)
web=$(compose_id web)

backup() {
  local destination=$1 parent temporary
  [[ ! -e $destination ]] || fail "backup destination already exists: $destination"
  parent=$(dirname "$destination")
  [[ -d $parent ]] || fail "backup parent does not exist: $parent"
  temporary=$(mktemp -d "$parent/.frameops-recovery.XXXXXX")
  trap 'rm -rf "$temporary"' EXIT

  docker compose --project-name "$project" --env-file "$environment" stop api web
  if ! docker exec "$postgres" sh -ceu 'exec pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >"$temporary/postgres.dump"; then
    docker start "$api" "$web" >/dev/null
    fail "PostgreSQL backup failed; application containers were restarted"
  fi
  docker stop "$postgres" "$minio" >/dev/null
  mkdir "$temporary/minio"
  if ! docker cp "$minio:/data" "$temporary/minio" || ! tar -C "$temporary" -cf "$temporary/minio.tar" minio; then
    docker start "$postgres" "$minio" "$api" "$web" >/dev/null
    fail "MinIO backup failed; containers were restarted"
  fi
  rm -rf "$temporary/minio"
  (cd "$temporary" && sha256sum postgres.dump minio.tar >SHA256SUMS)
  chmod 700 "$temporary"
  mv "$temporary" "$destination"
  trap - EXIT
  docker start "$postgres" "$minio" "$api" "$web" >/dev/null
  printf 'recovery backup created: %s\n' "$destination"
}

restore() {
  local backup=$1 temporary
  [[ -d $backup && -f $backup/postgres.dump && -f $backup/minio.tar && -f $backup/SHA256SUMS ]] || fail "invalid recovery backup: $backup"
  (cd "$backup" && sha256sum --check --status SHA256SUMS) || fail "recovery backup checksum verification failed"
  temporary=$(mktemp -d)
  trap 'rm -rf "$temporary"' EXIT
  tar -C "$temporary" -xf "$backup/minio.tar"
  [[ -d $temporary/minio ]] || fail "recovery backup has no MinIO data"

  docker compose --project-name "$project" --env-file "$environment" stop api web
  docker stop "$postgres" "$minio" >/dev/null
  if ! docker start "$postgres" >/dev/null || ! docker exec "$postgres" sh -ceu 'until pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"; do sleep 1; done' || ! docker exec -i "$postgres" sh -ceu 'exec pg_restore --clean --if-exists --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <"$backup/postgres.dump"; then
    docker stop "$postgres" >/dev/null || true
    fail "restore failed; PostgreSQL and MinIO remain stopped; do not release"
  fi
  docker stop "$postgres" >/dev/null
  if ! docker run --rm --volumes-from "$minio" alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 sh -ceu 'find /data -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +'; then
    fail "restore failed; PostgreSQL and MinIO remain stopped; do not release"
  fi
  if ! docker cp "$temporary/minio/." "$minio:/data"; then
    fail "restore failed; PostgreSQL and MinIO remain stopped; do not release"
  fi
  docker start "$postgres" "$minio" "$api" "$web" >/dev/null
  trap - EXIT
  rm -rf "$temporary"
  printf 'recovery restore completed: %s\n' "$backup"
}

case "$1" in
  backup) backup "$2" ;;
  restore) restore "$2" ;;
  *) usage ;;
esac
