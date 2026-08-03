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

MinIO is only a local S3-compatible dependency in Stage 2: its API is bound to localhost, it does not provide a bucket, object-storage adapter, upload path, signed URL, or product data behavior. The MinIO console is intentionally not published.

## Data handling

`.env.example` contains placeholders only. Do not commit production credentials, tokens, passwords, customer information, or real evidence. Do not use client data or real evidence in the local environment, tests, fixtures, logs, or documentation examples.
