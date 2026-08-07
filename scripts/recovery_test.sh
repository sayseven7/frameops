#!/bin/bash
set -euo pipefail

script=scripts/recovery.sh
if [[ ! -x $script ]]; then
  printf '%s must be an executable isolated recovery command\n' "$script" >&2
  exit 1
fi

for required in \
  'backup|restore' \
  'FRAMEOPS_LOCAL_STATE_DIR' \
  'local destination=$1 parent temporary=' \
  'local backup=$1 temporary=' \
  'pg_dump -Fc' \
  'docker cp "$minio:/data"' \
  'docker cp "$temporary/minio/data/." "$minio:/data"' \
  'cat "$state/minio-root-user" > "$state/fifo/minio-root-user" &' \
  'cat "$state/minio-root-password" > "$state/fifo/minio-root-password" &' \
  'if ! wait "$minio_user_writer"; then' \
  'if ! wait "$minio_password_writer"; then' \
  'docker exec "$api" wget -q -O /dev/null http://127.0.0.1:8080/health' \
  'docker start "$postgres" "$minio"' \
  'docker start "$api" "$web"' \
  'docker stop "$postgres" >/dev/null || true' \
  'postgres:18.4@sha256:3a82e1f56c8f0f5616a11103ac3d47e632c3938698946a7ad26da0df1334744a'; do
  if ! grep -Fq "$required" "$script"; then
    printf '%s must contain %q\n' "$script" "$required" >&2
    exit 1
  fi
done

for required in \
  '--pull=never' \
  '--network=none' \
  '--cap-drop=ALL' \
  '--security-opt=no-new-privileges' \
  'docker inspect --format' \
  'docker volume inspect --format' \
  '--mount "type=volume,src=$minio_data_volume,dst=/data"'; do
  if ! grep -Fq -- "$required" "$script"; then
    printf '%s must harden the MinIO cleanup helper with %q\n' "$script" "$required" >&2
    exit 1
  fi
done

for forbidden in 'down -v' '--remove-orphans' 'docker system prune' 'docker volume prune' 'docker volume rm' 'docker compose down' '--volumes-from'; do
  if grep -Fq -- "$forbidden" "$script"; then
    printf '%s must not contain %q\n' "$script" "$forbidden" >&2
    exit 1
  fi
done

if grep -Eq 'printf.*(PASSWORD|SECRET|TOKEN)|cat.*(PASSWORD|SECRET|TOKEN)' "$script"; then
  printf '%s must not print secret material\n' "$script" >&2
  exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/state/fifo"
mkfifo "$work/state/fifo/minio-root-user" "$work/state/fifo/minio-root-password"
printf 'user\n' >"$work/state/minio-root-user"
printf 'password\n' >"$work/state/minio-root-password"
printf 'FRAMEOPS_LOCAL_STATE=1\n' >"$work/state/runtime.env"
project="frameops-local-$(printf '%s' "$work/state" | sha256sum | cut -d ' ' -f1)"

cat >"$work/bin/docker" <<EOF
#!/bin/bash
printf '%s\\n' "\$*" >>"$work/docker-args"
case "\$*" in
  "compose --project-name $project --env-file $work/state/runtime.env ps -q postgres") printf 'postgres-id\\n' ;;
  "compose --project-name $project --env-file $work/state/runtime.env ps -q minio") printf 'minio-id\\n' ;;
  "compose --project-name $project --env-file $work/state/runtime.env ps -q api") printf 'api-id\\n' ;;
  "compose --project-name $project --env-file $work/state/runtime.env ps -q web") printf 'web-id\\n' ;;
  *' pg_dump -Fc '*) printf 'postgres-backup' ;;
  "start postgres-id minio-id") cat "$work/state/fifo/minio-root-user" "$work/state/fifo/minio-root-password" >/dev/null ;;
esac
EOF
chmod 700 "$work/bin/docker"
backup="$work/backup"
PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$work/state" bash "$script" backup "$backup" >/dev/null

for required in \
  "compose --project-name $project --env-file $work/state/runtime.env ps -q postgres" \
  "compose --project-name $project --env-file $work/state/runtime.env stop api web" \
  'exec postgres-id sh -ceu exec pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  'stop postgres-id minio-id' \
  'cp minio-id:/data' \
  'start postgres-id minio-id' \
  'start api-id web-id'; do
  if ! grep -Fq "$required" "$work/docker-args"; then
    printf '%s backup must issue scoped command %q\n' "$script" "$required" >&2
    exit 1
  fi
done

[[ -f $backup/postgres.dump && -f $backup/minio.tar && -f $backup/SHA256SUMS ]] || {
  printf '%s backup must write PostgreSQL, MinIO, and checksum artifacts\n' "$script" >&2
  exit 1
}

cat >"$work/bin/docker" <<EOF
#!/bin/bash
printf '%s\\n' "\$*" >>"$work/docker-args"
case "\$*" in
  "compose --project-name $project --env-file $work/state/runtime.env ps -q postgres") printf 'postgres-id\\n' ;;
  "compose --project-name $project --env-file $work/state/runtime.env ps -q minio") printf 'minio-id\\n' ;;
  "compose --project-name $project --env-file $work/state/runtime.env ps -q api") printf 'api-id\\n' ;;
  "compose --project-name $project --env-file $work/state/runtime.env ps -q web") printf 'web-id\\n' ;;
  "start postgres-id minio-id") cat "$work/state/fifo/minio-root-user" "$work/state/fifo/minio-root-password" >/dev/null ;;
  "cp ")
    if [[ \$2 != */minio/data/. || \$3 != minio-id:/data ]]; then
      exit 44
    fi
    ;;
  "volume inspect --format "*)
    if [[ \${RECOVERY_MOUNT_CASE:-valid} == foreign ]]; then
      printf 'other-project\\n'
    else
      printf '$project\\n'
    fi
    ;;
  "inspect --format "*)
    case "\${RECOVERY_MOUNT_CASE:-valid}" in
      valid) printf 'minio-data-volume\\n' ;;
      missing) ;;
      ambiguous) printf 'minio-data-volume\\nother-volume\\n' ;;
      foreign) printf 'foreign-volume\\n' ;;
    esac
    ;;
  "run "*)
    if [[ \${RECOVERY_IMAGE_CASE:-cached} == missing ]]; then
      exit 125
    fi
    ;;
esac
EOF
chmod 700 "$work/bin/docker"
if ! PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$work/state" bash "$script" restore "$backup" >/dev/null; then
  printf '%s restore must copy the verified extracted MinIO archive data\n' "$script" >&2
  exit 1
fi
for required in \
  'run --rm --pull=never --network=none --cap-drop=ALL --security-opt=no-new-privileges --mount type=volume,src=minio-data-volume,dst=/data alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659'; do
  if ! grep -Fq -- "$required" "$work/docker-args"; then
    printf '%s restore must run the offline least-privilege helper with only MinIO data\n' "$script" >&2
    exit 1
  fi
done

for mount_case in missing ambiguous foreign; do
  if PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$work/state" RECOVERY_MOUNT_CASE="$mount_case" bash "$script" restore "$backup" >/dev/null 2>&1; then
    printf '%s restore must fail closed for %s MinIO data mounts\n' "$script" "$mount_case" >&2
    exit 1
  fi
done
if PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$work/state" RECOVERY_IMAGE_CASE=missing bash "$script" restore "$backup" >/dev/null 2>&1; then
  printf '%s restore must fail when the pinned helper image is absent from cache\n' "$script" >&2
  exit 1
fi
printf 'PASS: recovery contract backup is isolated and preserves both stores\n'
