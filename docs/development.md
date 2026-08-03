# FrameOPS development

## Prerequisites

Install Go 1.26.5 or newer, Node.js 22.12.0 or newer, pnpm 10.x, Python 3.13 or newer, Docker Compose, and `golangci-lint`. Verify the local environment with `bash scripts/check-toolchains.sh`; it reports each unmet prerequisite with remediation guidance.

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

Start only the development PostgreSQL service with:

```bash
docker compose --env-file .env up -d postgres
```

Stop the containers without deleting the named volume with:

```bash
docker compose --env-file .env down
```

The Compose definition is restricted to PostgreSQL and binds its port to localhost. It does not provide migrations, application services, object storage, or product data.

## Data handling

`.env.example` contains placeholders only. Do not commit production credentials, tokens, passwords, customer information, or real evidence. Do not use client data or real evidence in the local environment, tests, fixtures, logs, or documentation examples.
