# FrameOPS development

## Prerequisites

Install Go 1.26.5 or newer, Node.js 22.12.0 or newer, pnpm 10.x, Python 3.13 or newer, Docker Compose, and `golangci-lint` 2.9.0 built with Go 1.26.5. Verify the local environment with `bash scripts/check-toolchains.sh`; it reports each unmet prerequisite with remediation guidance.

## Quick start

Copy the safe local placeholders and run the complete verification gate:

```bash
cp .env.example .env
make check
```

`.env` is local-only and is ignored by Git.

## Quality checks

`make check` runs the toolchain and Compose validators, Go formatting, linting and tests, then the frozen frontend installation, lint, type check, and production build. The frontend must be installed only with pnpm and its committed `pnpm-lock.yaml`.

Individual commands are available through `make fmt`, `make lint`, `make test`, and `make web-check`.

## Local PostgreSQL

Start the development PostgreSQL and MinIO dependencies with:

```bash
docker compose --env-file .env up -d postgres minio
```

Stop the containers without deleting their named volumes with:

```bash
docker compose --env-file .env down
```

PostgreSQL is bound to localhost. Migrations and their integration tests are added by this stage. `make schema-test` always creates a disposable database; it must never be used against the persistent development database.

## Local MinIO

MinIO is the local S3-compatible object store for evidence bytes. Its API is bound to localhost and its console is intentionally not published. The API reads `FRAMEOPS_EVIDENCE_S3_ENDPOINT`, `FRAMEOPS_EVIDENCE_S3_BUCKET`, `FRAMEOPS_EVIDENCE_S3_REGION`, `FRAMEOPS_EVIDENCE_S3_ACCESS_KEY`, and `FRAMEOPS_EVIDENCE_S3_SECRET_KEY`, creates the bucket if it is absent, and refuses to start when object storage is unreachable: evidence capture has no degraded mode. The HTTP integration tests need the same variables and are skipped without them. A real deployment uses a dedicated key scoped to that bucket, never the object-storage root credential used locally. Signed download URLs do not exist yet; access is authorized by the server.

## Data handling

`.env.example` contains placeholders only. Do not commit production credentials, tokens, passwords, customer information, or real evidence. Do not use client data or real evidence in the local environment, tests, fixtures, logs, or documentation examples.
