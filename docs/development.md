# FrameOPS development

## Prerequisites

Install Go 1.26.5 or newer, Node.js 22.12.0 or newer, pnpm 10.x, Python 3.13 or newer, Docker Compose, and `golangci-lint` 2.9.0 built with Go 1.26.5. Verify the local environment with `bash scripts/check-toolchains.sh`; it reports each unmet prerequisite with remediation guidance.

## Quick start

Run the reproducible local runtime launcher. It builds the Compose API and UI
and waits for their readiness chain:

```bash
bash scripts/local-runtime.sh
```

It inventories the required ports, writes all state under `~/.local/state/frameops`
(or `FRAMEOPS_LOCAL_STATE_DIR`) with a `0700` directory and `0600` files, starts
digest-pinned loopback-only Compose services, migrates, builds the PDF worker,
proves MinIO COMPLIANCE Object Lock rejects overwrite and delete, performs the
single transactional bootstrap, and starts API `127.0.0.1:8081` and UI
`localhost:3000`. It never prints generated secrets. The script refuses an
occupied port, so it does not interfere with another local preview.

Stop the same isolated project and delete only its project-scoped named volumes:

```bash
bash scripts/local-runtime.sh down
```

This launcher is disposable: `down` passes the exact generated Compose project
name and `--volumes`, so its PostgreSQL and MinIO data is removed. Volumes outside
that isolated project are not selected. Use manual Compose instead when local data
must persist across shutdowns.

For a manually managed Compose deployment, copy `.env.example` to a local `.env`,
provide local MinIO root-credential FIFOs, then run `docker compose up --build
--wait`. Migration must complete before API readiness; UI readiness requires its
own HTTP response after the API health check. Use `docker compose down --timeout
10` for graceful shutdown.

The UI uses its same-origin `/v1` proxy. The API keeps its production `Secure`
cookie; browsers treat `localhost` as a secure local context, so local use does
not relax cookie flags or enable CORS. Use `http://localhost:3000`, not a remote
host, for that flow.

Run the complete verification gate separately:

```bash
make check
```

## Quality checks

`make check` runs the toolchain and Compose validators, Go formatting, linting and tests, then the frozen frontend installation, lint, type check, and production build. The frontend must be installed only with pnpm and its committed `pnpm-lock.yaml`.

Individual commands are available through `make fmt`, `make lint`, `make test`, and `make web-check`.

## Contrato operacional do Compose

O `compose.yaml` é um runtime local, isolado por portas loopback; ele não é um
runbook de produção nem fornece backup, restore ou rollback automatizados. A
decisão e os limites verificáveis estão em
`docs/decisions/0004-contrato-operacional-compose.md`.

Antes de iniciar, valide as versões e a configuração renderizada:

```bash
bash scripts/check-toolchains.sh
```

`postgres` e `minio` mantêm os dados somente nos volumes nomeados
`frameops-postgres-data` e `frameops-minio-data`. O diretório compartilhado do
socket do renderer é `frameops-render-socket` e não é um backup. No fluxo Compose
manual, não use `docker compose down -v`, `--remove-orphans` ou remoção manual de
volumes para parar o runtime persistente. O launcher efêmero é a exceção explícita:
`bash scripts/local-runtime.sh down` seleciona seu project-name derivado do
diretório de estado e usa `--volumes`, removendo somente os volumes nomeados desse
projeto isolado.

O MinIO lê suas credenciais raiz uma única vez por FIFOs e, por isso, tem
`restart: "no"`. Para uma subida manual, exponha os valores já escolhidos para
o ambiente local, crie FIFOs temporários e publique cada valor uma vez:

```bash
set -a; source .env; set +a
fifo_dir=$(mktemp -d)
trap 'rm -rf "$fifo_dir"' EXIT
mkfifo "$fifo_dir/minio-root-user" "$fifo_dir/minio-root-password"
(printf '%s' "$FRAMEOPS_MINIO_ROOT_USER" >"$fifo_dir/minio-root-user") &
(printf '%s' "$FRAMEOPS_MINIO_ROOT_PASSWORD" >"$fifo_dir/minio-root-password") &
FRAMEOPS_MINIO_ROOT_USER_FIFO="$fifo_dir/minio-root-user" \
FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO="$fifo_dir/minio-root-password" \
  docker compose --env-file .env config --quiet
FRAMEOPS_MINIO_ROOT_USER_FIFO="$fifo_dir/minio-root-user" \
FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO="$fifo_dir/minio-root-password" \
  docker compose --env-file .env up --build --wait
```

Não coloque esses valores em histórico de shell, logs ou documentação. Para o
launcher local, que gera e guarda seu próprio estado com permissões restritas,
prefira `bash scripts/local-runtime.sh`.

O `up --wait` só retorna depois dos healthchecks declarados: PostgreSQL usa
`pg_isready`, MinIO usa `/minio/health/live`, renderer testa o socket Unix, API
usa `GET /health` e web usa `GET /login`. Confira o estado sem modificar o
projeto:

```bash
docker compose --env-file .env ps
curl --fail "http://127.0.0.1:$FRAMEOPS_API_PORT/health"
```

Pare graciosamente sem apagar estado com `docker compose --env-file .env down
--timeout 10`. Para o launcher isolado e descartável, `bash
scripts/local-runtime.sh down` também apaga os volumes nomeados do projeto; não o
use como parada preservadora. Uma falha antes de a migração completar no fluxo
manual pode ser recuperada parando sem volumes, corrigindo a configuração e
repetindo a subida. Depois de uma migração
concluída, não execute `down-to` como rollback rotineiro: o binário o expõe para
intervenção explícita, mas não há procedimento de compatibilidade nem restore
consistente de PostgreSQL e MinIO neste repositório. Preserve e valide backups
externos dos dois stores antes de qualquer upgrade; sem eles, a recuperação de
perda de dados não é suportada.

## Web portfolio

The web app calls its same-origin `/v1` routes and keeps the session cookie in the browser. Configure the Next.js reverse proxy with the non-secret API origin before starting or building the web app:

```bash
export FRAMEOPS_API_URL=https://frameops.example.test
pnpm --filter @frameops/web dev
```

Keeping the browser same-origin avoids enabling CORS for the secure session cookie. `FRAMEOPS_API_URL` is required and must be an HTTP(S) origin; the web build fails explicitly when it is absent or invalid. The app has local pt-BR and English labels; it does not contain a fallback API address or sample portfolio data.

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

## Operator CLI

`cmd/fops` is the operator CLI. Every command except the local first-admin
bootstrap works through the HTTP API and has no database dependency at all; the
`internal/e2e` check asserts that dependency boundary and drives the built binary
against a running API.

```bash
go build -o "$PWD/bin/fops" ./cmd/fops
bin/fops login --api https://frameops.example.test --email operator@example.test --password-file ./password
bin/fops ingest nmap ./scan.xml --engagement "$ENGAGEMENT_ID"
```

`login` stores only the session the API issued, in a `0600` file inside a `0700`
directory under `$FRAMEOPS_CONFIG_HOME`, `$XDG_CONFIG_HOME/frameops` or
`~/.config/frameops`. The session cookie is `Secure`, so the CLI refuses an API
address that is not `https://` unless it is loopback, where the local development
server runs.

`ingest nmap` uploads the artifact exactly as Nmap wrote it; the API parses it,
applies the limits, and answers with the summary of what the import created,
reused, ignored and rejected. Hosts become engagement assets marked
`"source":"ingest"`, an already known host is reused rather than duplicated, and
re-uploading the same artifact into the same engagement is refused as
`duplicate_artifact`. The CLI is online-only: there is no local queue, and a
failed upload is repeated. `docs/decisions/0003-ingestao-de-output-de-ferramenta.md`
records the deduplication and identity defaults in force and what remains
pending.

## Data handling

`.env.example` contains placeholders only. Do not commit production credentials, tokens, passwords, customer information, or real evidence. Do not use client data or real evidence in the local environment, tests, fixtures, logs, or documentation examples.
