#!/bin/bash
set -euo pipefail

script=scripts/local-runtime.sh
if [[ ! -f $script ]]; then
  printf '%s is required\n' "$script" >&2
  exit 1
fi

for required in 'umask 077' 'project="frameops-local-$(printf '\''%s'\'' "$state" | sha256sum | cut -d '\'' '\'' -f1)"' 'for command in docker go pnpm curl od ss base64 sha256sum; do' 'chmod 700 "$state"' 'chmod 600 "$environment"' 'FRAMEOPS_POSTGRES_PORT=15432' 'FRAMEOPS_MINIO_PORT=19000' 'FRAMEOPS_HTTP_ADDR=127.0.0.1:18081' 'FRAMEOPS_API_URL=http://127.0.0.1:18081' 'FRAMEOPS_UI_PORT=13000' 'FRAMEOPS_OBJECT_LOCK_PROOF=1' 'FRAMEOPS_DATABASE_URL=postgres://frameops_local:${postgres_password}@127.0.0.1:15432/frameops_local?sslmode=disable' 'go build -o "$worker" ./cmd/frameops-render' 'bootstrap-first-admin' 'docker compose --project-name "$project" --env-file "$environment"' 'psql -U frameops_local -d frameops_local -c '\''SELECT 1'\''' 'pnpm --filter @frameops/web dev --hostname 127.0.0.1 --port "$FRAMEOPS_UI_PORT"'; do
  if ! grep -Fq "$required" "$script"; then
    printf '%s must contain %q\n' "$script" "$required" >&2
    exit 1
  fi
done

if grep -Fq 'FRAMEOPS_DATABASE_URL=postgres://frameops_local:***@' "$script"; then
  printf '%s must not use a masked database password\n' "$script" >&2
  exit 1
fi

if grep -Eq 'printf.*(PASSWORD|SECRET|TOKEN)|cat.*(PASSWORD|SECRET|TOKEN)' "$script"; then
  printf '%s must not print secret material\n' "$script" >&2
  exit 1
fi

for forbidden in 5432 9000 8081 3000; do
  if grep -Eq "(FRAMEOPS_(POSTGRES|MINIO)_PORT|FRAMEOPS_HTTP_ADDR|FRAMEOPS_UI_PORT|FRAMEOPS_API_URL|127\\.0\\.0\\.1:)$forbidden" "$script"; then
    printf '%s must not use shared port %s\n' "$script" "$forbidden" >&2
    exit 1
  fi
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin" "$work/state"
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
