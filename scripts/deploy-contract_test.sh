#!/bin/bash
set -euo pipefail

for file in Dockerfile.api Dockerfile.web .dockerignore; do
  [[ -f $file ]] || { printf '%s is required\n' "$file" >&2; exit 1; }
done

for required in \
  '  migrate:' \
  '  api:' \
  '  web:' \
  'condition: service_healthy' \
  'condition: service_completed_successfully' \
  'restart: unless-stopped' \
  'stop_grace_period: 10s' \
  'node -e' \
  '127.0.0.1:3000/login'; do
  grep -Fq -- "$required" compose.yaml || { printf 'compose.yaml must contain %q\n' "$required" >&2; exit 1; }
done

for required in \
  'go build -trimpath' \
  'CGO_ENABLED=0' \
  'ENTRYPOINT ["/frameops-api"]' \
  '-o /out/frameops-render' \
  'pnpm install --frozen-lockfile' \
  'ARG FRAMEOPS_API_URL' \
  'ARG FRAMEOPS_COMPOSE_INTERNAL_API' \
  'ENV FRAMEOPS_API_URL=$FRAMEOPS_API_URL' \
  'ENV FRAMEOPS_COMPOSE_INTERNAL_API=$FRAMEOPS_COMPOSE_INTERNAL_API' \
  'RUN pnpm --filter @frameops/web build' \
  '"next", "start", "--hostname", "0.0.0.0", "--port", "3000"'; do
  grep -Fq -- "$required" Dockerfile.api Dockerfile.web || { printf 'Dockerfiles must contain %q\n' "$required" >&2; exit 1; }
done

[[ $(grep -Fc 'FRAMEOPS_API_URL: http://api:8080' compose.yaml) == 2 ]] || { printf 'Compose web build and runtime must pin the internal API origin\n' >&2; exit 1; }
[[ $(grep -Fc 'FRAMEOPS_COMPOSE_INTERNAL_API: "1"' compose.yaml) == 2 ]] || { printf 'Compose web build and runtime must set the exact internal API marker\n' >&2; exit 1; }

grep -Fq 'FRAMEOPS_API_URL=http://127.0.0.1:8081 pnpm --filter @frameops/web build' Makefile || { printf 'Makefile web-check must set FRAMEOPS_API_URL\n' >&2; exit 1; }
grep -Fq 'npm install --global corepack@0.31.0' Dockerfile.web || { printf 'Dockerfile.web must update Corepack before pnpm activation\n' >&2; exit 1; }
[[ $(grep -Fh 'USER 10001:10001' Dockerfile.api Dockerfile.web | wc -l) == 4 ]] || { printf 'api, renderer, migrate, and web runtime stages must run as UID:GID 10001:10001\n' >&2; exit 1; }
[[ $(grep -F 'chown 10001:10001 /run/frameops' Dockerfile.api | wc -l) == 2 ]] || { printf 'api and renderer images must own the shared socket directory as UID:GID 10001:10001\n' >&2; exit 1; }
grep -Fq 'ENV HOME=/tmp' Dockerfile.web || { printf 'Dockerfile.web must provide a writable HOME for non-root Corepack\n' >&2; exit 1; }

grep -Fq 'ui_port=${FRAMEOPS_UI_PORT:-3000}' scripts/local-runtime.sh || { printf 'local runtime must configure a default UI port\n' >&2; exit 1; }
grep -Fq 'curl --fail --silent --output /dev/null "http://127.0.0.1:$ui_port/"' scripts/local-runtime.sh || { printf 'local runtime must wait for UI HTTP readiness\n' >&2; exit 1; }
grep -Fq 'docker compose --project-name "$project" --env-file "$environment" down --timeout 10' scripts/local-runtime.sh || { printf 'local runtime must tear down Compose idempotently\n' >&2; exit 1; }
