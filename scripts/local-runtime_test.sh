#!/bin/bash
set -euo pipefail

script=scripts/local-runtime.sh
if [[ ! -f $script ]]; then
  printf '%s is required\n' "$script" >&2
  exit 1
fi

# shellcheck disable=SC2016 # Intentional literals checked with grep below.
for required in 'umask 077' 'project="frameops-local-$(printf '\''%s'\'' "$state" | sha256sum | cut -d '\'' '\'' -f1)"' 'for command in docker go pnpm curl od ss base64 sha256sum; do' 'chmod 700 "$state"' 'chmod 600 "$environment"' 'postgres_port=${FRAMEOPS_POSTGRES_PORT:-15432}' 'minio_port=${FRAMEOPS_MINIO_PORT:-19000}' 'api_port=${FRAMEOPS_API_PORT:-8081}' 'ui_port=${FRAMEOPS_UI_PORT:-3000}' 'FRAMEOPS_POSTGRES_PORT=$postgres_port' 'FRAMEOPS_MINIO_PORT=$minio_port' 'FRAMEOPS_DATABASE_URL=postgres://frameops_local:$postgres_password@postgres:5432/frameops_local?sslmode=disable' 'FRAMEOPS_DATABASE_URL="postgres://frameops_local:$postgres_password@127.0.0.1:$postgres_port/frameops_local?sslmode=disable"' 'FRAMEOPS_HTTP_ADDR=127.0.0.1:$api_port' 'FRAMEOPS_API_PORT=$api_port' 'FRAMEOPS_UI_PORT=$ui_port' 'FRAMEOPS_OBJECT_LOCK_PROOF=1' 'bootstrap-first-admin' 'docker compose --project-name "$project" --env-file "$environment"' 'up --build --wait' 'curl --fail --silent --output /dev/null "http://127.0.0.1:$ui_port/"' 'down --timeout 10'; do
  if ! grep -Fq "$required" "$script"; then
    printf '%s must contain %q\n' "$script" "$required" >&2
    exit 1
  fi
done

checker_line=$(grep -nF 'if ! bash scripts/check-toolchains.sh; then' "$script" | cut -d: -f1)
require_line=$(grep -nF 'for command in docker go pnpm curl od ss base64 sha256sum; do' "$script" | cut -d: -f1)
if [[ -z $checker_line || -z $require_line || $checker_line -ge $require_line ]]; then
  printf '%s must run the shared prerequisite check before local command checks\n' "$script" >&2
  exit 1
fi

if grep -Fq '***' "$script"; then
  printf '%s must not use a masked database password\n' "$script" >&2
  exit 1
fi

if grep -Eq 'printf.*(PASSWORD|SECRET|TOKEN)|cat.*(PASSWORD|SECRET|TOKEN)' "$script"; then
  printf '%s must not print secret material\n' "$script" >&2
  exit 1
fi

for forbidden in 5432 9000 18081 13000; do
  if grep -Eq "(FRAMEOPS_(POSTGRES|MINIO)_PORT|FRAMEOPS_HTTP_ADDR|FRAMEOPS_UI_PORT|FRAMEOPS_API_URL|127\\.0\\.0\\.1:)$forbidden" "$script"; then
    printf '%s must not use shared port %s\n' "$script" "$forbidden" >&2
    exit 1
  fi
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin" "$work/state"

# A missing release prerequisite must fail before generating output through
# Compose; the launcher delegates the version contract to the shared checker.
preflight_state="$work/preflight-state"
mkdir -p "$preflight_state"
cat >"$work/bin/ss" <<'EOF'
#!/bin/bash
exit 0
EOF
cat >"$work/bin/docker" <<EOF
#!/bin/bash
printf '%s\n' "\$*" >"$work/preflight-docker-args"
exit 23
EOF
cat >"$work/bin/bash" <<EOF
#!/bin/bash
printf '%s\n' "\$*" >"$work/preflight-check-args"
printf 'missing required release prerequisite\n' >&2
exit 37
EOF
chmod 700 "$work/bin/ss" "$work/bin/docker" "$work/bin/bash"

if PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$preflight_state" /bin/bash "$script" >/dev/null 2>"$work/preflight-stderr"; then
  printf '%s must reject an unavailable release prerequisite\n' "$script" >&2
  exit 1
fi
if [[ ! -f $work/preflight-check-args ]] || [[ $(<"$work/preflight-check-args") != 'scripts/check-toolchains.sh' ]] || ! grep -Fq 'local runtime prerequisites are unavailable' "$work/preflight-stderr" || [[ -e $work/preflight-docker-args ]]; then
  printf '%s must run the shared prerequisite check and fail before Docker Compose\n' "$script" >&2
  exit 1
fi
rm "$work/bin/bash"
cat >"$work/bin/bash" <<'EOF'
#!/bin/bash
if [[ $1 == scripts/check-toolchains.sh ]]; then
  exit 0
fi
exec /bin/bash "$@"
EOF
chmod 700 "$work/bin/bash"

long_state="$work/state"
for _ in {1..20}; do
  long_state+='/frameops-local-state'
done
project="frameops-local-$(printf '%s' "$long_state" | sha256sum | cut -d ' ' -f1)"
if (( ${#long_state} <= 200 || ${#project} != 79 )); then
  printf '%s must use a fixed-size project name for long state paths\n' "$script" >&2
  exit 1
fi
cat >"$work/bin/ss" <<'EOF'
#!/bin/bash
printf 'LISTEN\n'
EOF
cat >"$work/bin/docker" <<EOF
#!/bin/bash
: >"$work/docker-reached"
EOF
ln -s /bin/true "$work/bin/pnpm"
chmod 700 "$work/bin/ss" "$work/bin/docker"

if PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$long_state" bash "$script" >/dev/null 2>"$work/stderr"; then
  printf '%s must reject a selected-port collision\n' "$script" >&2
  exit 1
fi
if [[ -e $work/docker-reached ]] || ! grep -Fq 'port 15432 is already listening; local runtime was not started' "$work/stderr"; then
  printf '%s must reject collisions before Docker is reached\n' "$script" >&2
  exit 1
fi

own_state="$work/own-state"
mkdir -p "$own_state"
printf 'FRAMEOPS_LOCAL_OWN=1\n' >"$own_state/runtime.env"
own_project="frameops-local-$(printf '%s' "$own_state" | sha256sum | cut -d ' ' -f1)"
cat >"$work/bin/docker" <<EOF
#!/bin/bash
if [[ "\$*" == "compose --project-name $own_project --env-file $own_state/runtime.env ps -q" ]]; then
  printf 'owned-container\n'
  exit 0
fi
if [[ "\$1" == inspect && "\$*" == *Config.Labels* ]]; then
  printf '%s\n' "$own_project"
  exit 0
fi
if [[ "\$1" == inspect ]]; then
  if [[ "\$*" == *.HostIp* ]]; then
    printf '5432/tcp 127.0.0.1 15432\n'
  else
    printf '15432\n'
  fi
  exit 0
fi
exit 23
EOF
chmod 700 "$work/bin/docker"

if PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$own_state" bash "$script" >/dev/null 2>"$work/own-stderr"; then
  printf '%s must continue when its own Compose project owns a listening port\n' "$script" >&2
  exit 1
fi
if [[ $(<"$work/own-stderr") == *'port 15432 is already listening'* ]]; then
  printf '%s must not reject a listening port owned by its Compose project\n' "$script" >&2
  exit 1
fi

for binding in \
  '5432/tcp 0.0.0.0 15432' \
  '15432/tcp 127.0.0.1 15432' \
  '5432/udp 127.0.0.1 15432'; do
  cat >"$work/bin/docker" <<EOF
#!/bin/bash
if [[ "\$*" == "compose --project-name $own_project --env-file $own_state/runtime.env ps -q" ]]; then
  printf 'owned-container\\n'
  exit 0
fi
if [[ "\$1" == inspect && "\$*" == *Config.Labels* ]]; then
  printf '%s\\n' "$own_project"
  exit 0
fi
if [[ "\$1" == inspect ]]; then
  if [[ "\$*" == *.HostIp* ]]; then
    printf '%s\\n' "$binding"
  else
    printf '15432\\n'
  fi
  exit 0
fi
exit 23
EOF
  chmod 700 "$work/bin/docker"
  if PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$own_state" bash "$script" >/dev/null 2>"$work/binding-stderr"; then
    printf '%s must reject a same-project container with binding %q\n' "$script" "$binding" >&2
    exit 1
  fi
  if ! grep -Fq 'port 15432 is already listening; local runtime was not started' "$work/binding-stderr"; then
    printf '%s must require loopback HostIp, target, and tcp protocol for a reused port\n' "$script" >&2
    exit 1
  fi
done

foreign_state="$work/foreign-state"
mkdir -p "$foreign_state"
printf 'FRAMEOPS_LOCAL_FOREIGN=1\n' >"$foreign_state/runtime.env"
foreign_project="frameops-local-$(printf '%s' "$foreign_state" | sha256sum | cut -d ' ' -f1)"
for label in '' "$own_project"; do
  cat >"$work/bin/docker" <<EOF
#!/bin/bash
if [[ "\$*" == "compose --project-name $foreign_project --env-file $foreign_state/runtime.env ps -q" ]]; then
  printf 'foreign-container\\n'
  exit 0
fi
if [[ "\$1" == inspect && "\$*" == *Config.Labels* ]]; then
  printf '%s\\n' "$label"
  exit 0
fi
if [[ "\$1" == inspect ]]; then
  if [[ "\$*" == *.HostIp* ]]; then
    printf '5432/tcp 127.0.0.1 15432\\n'
  else
    printf '15432\\n'
  fi
  exit 0
fi
exit 23
EOF
  chmod 700 "$work/bin/docker"
  if PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$foreign_state" bash "$script" >/dev/null 2>"$work/foreign-stderr"; then
    printf '%s must reject a listening port with label %q\n' "$script" "$label" >&2
    exit 1
  fi
  if ! grep -Fq 'port 15432 is already listening; local runtime was not started' "$work/foreign-stderr"; then
    printf '%s must require the current Compose project label for a listening port\n' "$script" >&2
    exit 1
  fi
done

status_state="$work/status-state"
mkdir -p "$status_state"
printf 'FRAMEOPS_LOCAL_STATUS=1\n' >"$status_state/runtime.env"
status_project="frameops-local-$(printf '%s' "$status_state" | sha256sum | cut -d ' ' -f1)"
cat >"$work/bin/docker" <<EOF
#!/bin/bash
printf '%s\n' "\$*" >"$work/docker-status-args"
exit 23
EOF
chmod 700 "$work/bin/docker"

if PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$status_state" bash "$script" status >/dev/null 2>"$work/status-stderr"; then
  printf '%s status must propagate docker compose ps failures\n' "$script" >&2
  exit 1
fi
if [[ $(<"$work/status-stderr") != '' ]] || [[ $(<"$work/docker-status-args") != "compose --project-name $status_project --env-file $status_state/runtime.env ps" ]]; then
  printf '%s status must run scoped docker compose ps without starting services\n' "$script" >&2
  exit 1
fi

down_state="$work/down-state"
mkdir -p "$down_state"
printf 'FRAMEOPS_LOCAL_DOWN=1\n' >"$down_state/runtime.env"
down_project="frameops-local-$(printf '%s' "$down_state" | sha256sum | cut -d ' ' -f1)"
cat >"$work/bin/docker" <<EOF
#!/bin/bash
printf '%s\n' "\$*" >"$work/docker-down-args"
EOF
chmod 700 "$work/bin/docker"

PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$down_state" bash "$script" down
if [[ $(<"$work/docker-down-args") != "compose --project-name $down_project --env-file $down_state/runtime.env down --timeout 10" ]]; then
  printf '%s down must preserve the current project named volumes\n' "$script" >&2
  exit 1
fi
for forbidden in 'docker system prune' 'docker volume prune' 'docker volume rm' '--remove-orphans'; do
  if grep -Fq -- "$forbidden" "$script"; then
    printf '%s down must not use %q\n' "$script" "$forbidden" >&2
    exit 1
  fi
done

single_up_state="$work/single-up-state"
mkdir -p "$single_up_state"
cat >"$work/bin/ss" <<'EOF'
#!/bin/bash
exit 0
EOF
cat >"$work/bin/docker" <<EOF
#!/bin/bash
printf '%s\n' "\$*" >>"$work/docker-up-args"
if [[ " \$* " == *' up '* ]]; then
  printf '%s\n' "\$FRAMEOPS_MINIO_ROOT_USER_FIFO" >>"$work/fifo-opens"
  cat "\$FRAMEOPS_MINIO_ROOT_USER_FIFO" >/dev/null
  printf '%s\n' "\$FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO" >>"$work/fifo-opens"
  cat "\$FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO" >/dev/null
fi
EOF
cat >"$work/bin/go" <<'EOF'
#!/bin/bash
if [[ $1 == build ]]; then
  printf '#!/bin/bash\nexit 0\n' >"$3"
  chmod 700 "$3"
fi
EOF
cat >"$work/bin/curl" <<'EOF'
#!/bin/bash
exit 0
EOF
chmod 700 "$work/bin/ss" "$work/bin/docker" "$work/bin/go" "$work/bin/curl"

if ! PATH="$work/bin:$PATH" FRAMEOPS_LOCAL_STATE_DIR="$single_up_state" timeout 10 /bin/bash "$script" >/dev/null 2>"$work/single-up-stderr"; then
  printf '%s must complete after one Compose up that consumes both MinIO FIFOs\n' "$script" >&2
  exit 1
fi
if [[ $(grep -c ' up ' "$work/docker-up-args") != 1 || $(wc -l <"$work/fifo-opens") != 2 || $(<"$work/docker-up-args") != *'up --build --wait'* || $(<"$work/docker-up-args") == *' exec '* ]]; then
  printf '%s must use one Compose up --build --wait without reopening MinIO FIFOs or polling readiness\n' "$script" >&2
  exit 1
fi
