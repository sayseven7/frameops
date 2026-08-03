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

## Isolated document worker

`cmd/frameops-render` is the only component that runs a document converter. The
API locates it through `FRAMEOPS_PDF_WORKER`, the absolute path of the built
worker executable, and refuses to start without it: a PDF is only ever the
conversion of an approved DOCX revision, so there is no degraded delivery mode.

The worker converts one file to one file. It inherits no environment, refuses to
start when it can see any variable outside `PATH`, `HOME`, `TMPDIR`, `LANG`,
`LC_ALL` and `TZ`, and runs LibreOffice headless inside an empty network
namespace (`unshare --net --map-root-user`). Local development therefore needs
`soffice` and `unshare` available; the conversion tests are skipped without them.
`POST /v1/report-revisions/{id}/pdf` answers `conversion_failed` when the worker
cannot convert, and no approved revision is ever recorded as delivered in that
case. `docs/decisions/0002-conversor-pdf-isolado.md` records the boundary that is
in force and what remains pending.

Build the worker before running the API:

```bash
go build -o "$PWD/bin/frameops-render" ./cmd/frameops-render
export FRAMEOPS_PDF_WORKER="$PWD/bin/frameops-render"
```

## Data handling

`.env.example` contains placeholders only. Do not commit production credentials, tokens, passwords, customer information, or real evidence. Do not use client data or real evidence in the local environment, tests, fixtures, logs, or documentation examples.
