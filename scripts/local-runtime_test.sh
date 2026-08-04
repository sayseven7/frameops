#!/bin/bash
set -euo pipefail

script=scripts/local-runtime.sh
if [[ ! -f $script ]]; then
  printf '%s is required\n' "$script" >&2
  exit 1
fi

for required in 'umask 077' 'chmod 700 "$state"' 'chmod 600 "$environment"' 'FRAMEOPS_HTTP_ADDR=127.0.0.1:8081' 'FRAMEOPS_API_URL=http://127.0.0.1:8081' 'FRAMEOPS_OBJECT_LOCK_PROOF=1' 'FRAMEOPS_DATABASE_URL=postgres://frameops_local:$postgres_password@127.0.0.1:5432/frameops_local?sslmode=disable' 'go build -o "$worker" ./cmd/frameops-render' 'bootstrap-first-admin' 'docker compose --project-name "$project" --env-file "$environment"'; do
  if ! grep -Fq "$required" "$script"; then
    printf '%s must contain %q\n' "$script" "$required" >&2
    exit 1
  fi
done

if grep -Eq 'printf.*(PASSWORD|SECRET|TOKEN)|cat.*(PASSWORD|SECRET|TOKEN)' "$script"; then
  printf '%s must not print secret material\n' "$script" >&2
  exit 1
fi
