# FrameOPS Stage 1 — Project Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish a reproducible FrameOPS workspace with the approved toolchains, a Go module, a locked frontend workspace, local PostgreSQL, quality gates, and development documentation.

**Architecture:** This stage creates only the project boundary and delivery tooling. The Go module is rooted at `github.com/sayseven7/frameops`; the future API, CLI, domain, storage, renderer, database schema, and UI are represented by directory boundaries, but no product behavior, migration, credential store, or HTTP route is introduced.

**Tech Stack:** Go 1.26.5; Node.js 22.12+; pnpm 10.x; PostgreSQL 18.x; Docker Compose; Next.js 16.2.x; React 19.2.x; Tailwind CSS 4.3.x; Python 3.13.x and docxtpl 0.20.2 reserved for the later isolated renderer.

## Global Constraints

- The Go module path is exactly `github.com/sayseven7/frameops`.
- Use Go 1.26.5 and Node.js 22.12 or newer; every toolchain check must fail with an actionable message below these floors.
- Use pnpm 10.x exclusively for the frontend. Version `pnpm-lock.yaml`; do not add npm or Yarn lockfiles.
- Use PostgreSQL 18.x for local development, pinned to a specific image tag and digest in Compose; never use a floating `latest` image.
- Next.js stays on 16.2.x, React on 19.2.x, and Tailwind on 4.3.x until an intentional reviewed upgrade.
- Keep secrets and real client data out of Git, fixtures, logs, documentation examples, and `.env.example` values.
- No database migration, object-storage service, API endpoint, authentication, finding, evidence, report, renderer, template, or CLI behavior belongs in this stage.
- Preserve the approved domain boundary: later domain code must not import HTTP, SQL, S3, CLI, or renderer packages.

---

## Planned File Structure

| Path | Responsibility |
|---|---|
| `.gitignore` | Excludes local environment files, generated binaries, caches, coverage, and editor/OS artefacts without excluding committed lockfiles. |
| `.tool-versions` | Pins developer-visible Go, Node.js, pnpm, Python, and Docker Compose version floors. |
| `go.mod` | Defines the approved module path and Go language version. |
| `go.sum` | Go module integrity lockfile, created only when a Go dependency is actually added. |
| `package.json` | pnpm workspace root with the approved package-manager declaration. |
| `pnpm-workspace.yaml` | Defines `web` as the sole pnpm workspace package. |
| `web/package.json` | Pins frontend runtime/tooling dependencies and scripts. |
| `pnpm-lock.yaml` | Frozen pnpm dependency graph for the complete workspace. |
| `web/next.config.ts` | Minimal Next configuration; no product routes or UI. |
| `web/tsconfig.json` | Strict TypeScript configuration for the future Next app. |
| `web/app/layout.tsx` | Minimal Next layout used only to prove the toolchain compiles; contains no visible product strings. |
| `web/app/page.tsx` | Minimal route used only for framework build verification; no visual-system implementation. |
| `web/app/globals.css` | Tailwind import only; no palette, tokens, or component styling before Stage 11 visual approval. |
| `compose.yaml` | Local PostgreSQL 18.x service with named volume, health check, port mapping, and required environment-variable names. |
| `.env.example` | Non-secret variable names and safe placeholders required by Compose. |
| `Makefile` | Stable commands for toolchain checks, formatting, linting, tests, compose lifecycle, and the aggregate `check`. |
| `.golangci.yml` | Go lint configuration without suppressing correctness or security checks. |
| `scripts/check-toolchains.sh` | Verifies version floors and the exclusive pnpm lockfile policy. |
| `scripts/check-compose.sh` | Validates rendered Compose configuration without starting services. |
| `internal/domain/doc.go` | Package declaration and architecture documentation only; contains no domain behavior. |
| `internal/application/doc.go` | Package declaration for future use cases and ports. |
| `internal/httpapi/doc.go` | Package declaration for future HTTP adapters. |
| `internal/store/postgres/doc.go` | Package declaration for the future PostgreSQL adapter. |
| `cmd/frameops-api/doc.go` | Future API binary boundary; no executable server in this stage. |
| `cmd/fops/doc.go` | Future CLI boundary; no executable command in this stage. |
| `docs/development.md` | Exact local setup, quality checks, Compose lifecycle, and the explicit no-real-data policy. |
| `scripts/check_toolchains_test.sh` | Shell test that proves the toolchain validator accepts the approved versions and rejects lower floors using isolated fake executables. |

## Task 1: Establish repository and toolchain contracts

**Files:**
- Create: `.gitignore`
- Create: `.tool-versions`
- Create: `go.mod`
- Create: `scripts/check-toolchains.sh`
- Create: `scripts/check_toolchains_test.sh`
- Modify: `PRODUCT.md`
- Test: `scripts/check_toolchains_test.sh`

**Interfaces:**
- Consumes: `go`, `node`, `pnpm`, `python3`, and `docker compose` commands on `PATH`.
- Produces: `scripts/check-toolchains.sh`, which exits `0` only when Go is at least 1.26.5, Node is at least 22.12.0, pnpm has major version 10, Python has major/minor 3.13, and Docker Compose is available.

- [ ] **Step 1: Write the failing validator tests**

Create `scripts/check_toolchains_test.sh` with an isolated temporary `PATH` containing executable shims. Cover these cases:

```bash
run_case "accepts approved floors" "go version go1.26.5" "v22.12.0" "10.0.0" "Python 3.13.0" 0
run_case "rejects old Go" "go version go1.25.9" "v22.12.0" "10.0.0" "Python 3.13.0" 1
run_case "rejects old Node" "go version go1.26.5" "v22.11.9" "10.0.0" "Python 3.13.0" 1
run_case "rejects pnpm outside major ten" "go version go1.26.5" "v22.12.0" "9.15.0" "Python 3.13.0" 1
run_case "rejects old Python" "go version go1.26.5" "v22.12.0" "10.0.0" "Python 3.12.9" 1
```

- [ ] **Step 2: Run the validator test to verify it fails**

Run: `bash scripts/check_toolchains_test.sh`

Expected: FAIL because `scripts/check-toolchains.sh` does not exist.

- [ ] **Step 3: Implement the minimal repository contracts**

Create `.tool-versions` with these lines:

```text
golang 1.26.5
nodejs 22.12.0
pnpm 10
python 3.13
```

Create `go.mod`:

```go
module github.com/sayseven7/frameops

go 1.26.5
```

Implement `scripts/check-toolchains.sh` to parse the command outputs listed above, print the detected version and a remediation command/message for each failed floor, and exit non-zero when any floor is unmet. Add `.gitignore` entries for `.env`, `.env.*` except `.env.example`, `bin/`, `coverage/`, `.next/`, `node_modules/`, and Python cache files. Do not ignore `go.sum` or `pnpm-lock.yaml`.

- [ ] **Step 4: Run the validator test to verify it passes**

Run: `bash scripts/check_toolchains_test.sh`

Expected: PASS for the approved-floor case and expected non-zero exits for every old-version case.

- [ ] **Step 5: Verify the local environment**

Run: `bash scripts/check-toolchains.sh`

Expected: all detected local versions satisfy the approved floors, or the command exits with the exact unmet prerequisite.

- [ ] **Step 6: Commit**

```bash
git add .gitignore .tool-versions go.mod scripts/check-toolchains.sh scripts/check_toolchains_test.sh
git commit -m "build: lock FrameOPS toolchain floors"
```

## Task 2: Add the local PostgreSQL development service

**Files:**
- Create: `.env.example`
- Create: `compose.yaml`
- Create: `scripts/check-compose.sh`
- Test: `scripts/check-compose.sh`

**Interfaces:**
- Consumes: `.env` values `FRAMEOPS_POSTGRES_USER`, `FRAMEOPS_POSTGRES_PASSWORD`, `FRAMEOPS_POSTGRES_DB`, and `FRAMEOPS_POSTGRES_PORT`.
- Produces: a Compose service named `postgres` with a named volume `frameops-postgres-data` and a passing `pg_isready` health check.

- [ ] **Step 1: Write the failing Compose contract test**

Create `scripts/check-compose.sh` that runs `docker compose --env-file .env.example config --quiet`, then asserts the rendered configuration includes:

```text
services.postgres
healthcheck.test
volumes.frameops-postgres-data
image: postgres:18.
```

The script must also reject an image ending in `:latest` and reject an image reference without `@sha256:`.

- [ ] **Step 2: Run the Compose contract test to verify it fails**

Run: `bash scripts/check-compose.sh`

Expected: FAIL because `compose.yaml` and `.env.example` do not exist.

- [ ] **Step 3: Implement the minimal Compose service**

Create `.env.example` with variable names and explicitly non-secret development placeholders. Create `compose.yaml` with only PostgreSQL, using an approved PostgreSQL 18.x image tag plus its digest, a named volume, a `pg_isready` health check, and a localhost-only port binding. Do not add MinIO, migrations, application services, or credentials beyond the local development placeholders.

Pin the exact image tag and digest by resolving `postgres:18` from the registry at implementation time, recording the command output in the task commit message or accompanying documentation. This pin must replace the `postgres:18.` assertion above with the exact chosen reference.

- [ ] **Step 4: Run the Compose contract test to verify it passes**

Run: `bash scripts/check-compose.sh`

Expected: PASS; Compose renders without warnings, the service is named `postgres`, and its image is an immutable PostgreSQL 18.x reference.

- [ ] **Step 5: Start and verify PostgreSQL**

Run: `docker compose --env-file .env.example up -d postgres && docker compose --env-file .env.example ps`

Expected: the `postgres` service reaches `healthy`.

- [ ] **Step 6: Stop the local service without removing its volume**

Run: `docker compose --env-file .env.example down`

Expected: containers and network stop; `frameops-postgres-data` remains available for the next startup.

- [ ] **Step 7: Commit**

```bash
git add .env.example compose.yaml scripts/check-compose.sh
git commit -m "build: add pinned local PostgreSQL service"
```

## Task 3: Create Go package boundaries and quality gate

**Files:**
- Create: `internal/domain/doc.go`
- Create: `internal/application/doc.go`
- Create: `internal/httpapi/doc.go`
- Create: `internal/store/postgres/doc.go`
- Create: `cmd/frameops-api/doc.go`
- Create: `cmd/fops/doc.go`
- Create: `.golangci.yml`
- Create: `Makefile`
- Test: `go test ./...`

**Interfaces:**
- Consumes: the module declared in `go.mod`.
- Produces: compilable package boundaries and `make fmt`, `make lint`, `make test`, and `make check` commands.

- [ ] **Step 1: Write the failing build check**

Run: `go test ./...`

Expected: FAIL because no Go package directories exist yet.

- [ ] **Step 2: Create documented, empty package boundaries**

Create one `doc.go` per listed directory. Each must declare exactly one package and contain a package comment defining its boundary. Use these package names:

```go
package domain
package application
package httpapi
package postgres
package main
```

The comments must state that `domain` cannot import transport or persistence concerns; `application` will own use cases and ports; `httpapi` adapts HTTP; `postgres` implements persistence; and the `cmd` packages are executable boundaries only. Do not create imports, endpoint handlers, database code, or CLI commands.

- [ ] **Step 3: Re-run the build check to verify it passes**

Run: `go test ./...`

Expected: PASS for every package.

- [ ] **Step 4: Add lint and aggregate commands**

Create `.golangci.yml` enabling default correctness checks and `govet`, with no global exclusion. Create `Makefile` targets:

```make
fmt: ## format Go sources
	go fmt ./...

lint: ## run Go lint
	golangci-lint run ./...

test: ## run all Go tests
	go test ./...

check: ## run the complete Stage 1 gate
	bash scripts/check-toolchains.sh
	bash scripts/check-compose.sh
	$(MAKE) fmt
	$(MAKE) lint
	$(MAKE) test
```

Use literal tabs in the final Makefile.

- [ ] **Step 5: Run the Stage 1 Go quality gate**

Run: `make fmt && make lint && make test`

Expected: all three commands exit `0` without altering files after the format pass.

- [ ] **Step 6: Commit**

```bash
git add internal cmd .golangci.yml Makefile
git commit -m "build: add Go package boundaries and quality gate"
```

## Task 4: Lock the frontend workspace without designing UI

**Files:**
- Create: `package.json`
- Create: `pnpm-workspace.yaml`
- Create: `web/package.json`
- Create: `pnpm-lock.yaml`
- Create: `web/next.config.ts`
- Create: `web/tsconfig.json`
- Create: `web/app/layout.tsx`
- Create: `web/app/page.tsx`
- Create: `web/app/globals.css`
- Test: `pnpm --filter @frameops/web lint && pnpm --filter @frameops/web build`

**Interfaces:**
- Consumes: Node.js 22.12+, pnpm 10.x, and the pinned Next/React/Tailwind dependency ranges.
- Produces: a frozen, buildable Next.js App Router workspace with no product UI or visible hard-coded product copy.

- [ ] **Step 1: Write a failing frontend build invocation**

Run: `pnpm install --frozen-lockfile && pnpm --filter @frameops/web build`

Expected: FAIL because the frontend workspace and lockfile do not exist.

- [ ] **Step 2: Create the frontend package and minimal compile surface**

Create root `package.json` with `private: true` and `packageManager` set to a pnpm 10.x release. Create `pnpm-workspace.yaml` containing `packages: ["web"]`. In `web/package.json`, set `name` to `@frameops/web`, pin Next to 16.2.x, React/React DOM to 19.2.x, and Tailwind to 4.3.x. Include only `dev`, `build`, `lint`, and `typecheck` scripts.

Create a strict TypeScript Next App Router configuration. `layout.tsx` must render its children and import `globals.css`; `page.tsx` may return an empty semantic `main` landmark used only for this build probe. `globals.css` imports Tailwind and contains no colors, arbitrary values, tokens, or component styles. Do not add i18n, theme providers, icons, components, or visual decisions in this stage.

- [ ] **Step 3: Generate and verify the frozen lockfile**

Run: `pnpm install --ignore-scripts`

Expected: `pnpm-lock.yaml` is generated and no lifecycle script runs.

Run: `pnpm install --frozen-lockfile --ignore-scripts && pnpm --filter @frameops/web lint && pnpm --filter @frameops/web typecheck && pnpm --filter @frameops/web build`

Expected: all commands PASS, creating only ignored build artefacts.

- [ ] **Step 4: Verify pnpm exclusivity**

Run: `rg --files -g 'package-lock.json' -g 'yarn.lock' -g 'bun.lock*'`

Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add package.json pnpm-workspace.yaml pnpm-lock.yaml web/package.json web/next.config.ts web/tsconfig.json web/app
git commit -m "build: establish locked Next.js workspace"
```

## Task 5: Document and run the complete reproducibility gate

**Files:**
- Create: `docs/development.md`
- Modify: `Makefile`
- Test: `make check`

**Interfaces:**
- Consumes: the commands created by Tasks 1–4.
- Produces: a documented, single-command local verification path for a new contributor.

- [ ] **Step 1: Write a failing documentation command check**

Run: `test -f docs/development.md && rg -n '^## (Prerequisites|Quick start|Quality checks|Local PostgreSQL|Data handling)$' docs/development.md`

Expected: FAIL because the development guide does not exist.

- [ ] **Step 2: Write the development guide**

Create `docs/development.md` with these exact sections:

```markdown
## Prerequisites
## Quick start
## Quality checks
## Local PostgreSQL
## Data handling
```

Document `cp .env.example .env`, `make check`, `docker compose --env-file .env up -d postgres`, and `docker compose --env-file .env down`. State that `.env.example` has placeholders only, production credentials must not be committed, and no client data or real evidence may be used locally.

Add `web-check` to `Makefile`:

```make
web-check: ## verify the frozen frontend workspace
	pnpm install --frozen-lockfile --ignore-scripts
	pnpm --filter @frameops/web lint
	pnpm --filter @frameops/web typecheck
	pnpm --filter @frameops/web build
```

Append `$(MAKE) web-check` to `check` after the Go test command.

- [ ] **Step 3: Re-run the documentation command check**

Run: `test -f docs/development.md && rg -n '^## (Prerequisites|Quick start|Quality checks|Local PostgreSQL|Data handling)$' docs/development.md`

Expected: PASS with all five required headings.

- [ ] **Step 4: Run the complete Stage 1 gate**

Run: `make check`

Expected: toolchain, Compose rendering, Go format/lint/test, and frozen frontend lint/typecheck/build all PASS.

- [ ] **Step 5: Inspect the final worktree**

Run: `git status --short && git diff --check`

Expected: only the documented Stage 1 files are changed; no whitespace errors, secrets, real evidence, generated builds, or non-pnpm lockfiles appear.

- [ ] **Step 6: Commit**

```bash
git add Makefile docs/development.md
git commit -m "docs: add reproducible development guide"
```

## Plan Self-Review

- **Spec coverage:** Tasks 1–5 cover the approved Stage 1 deliverable: directory boundaries, fixed toolchain floors, PostgreSQL Compose, lint, minimal builds/tests, pnpm lockfile, and development documentation. The plan intentionally excludes schema, MinIO, product rules, renderer, API behavior, authentication, CLI behavior, and UI design because later stages own them.
- **Placeholder scan:** No work item relies on an undefined future function, unnamed test, or unspecified file location. The PostgreSQL image digest is intentionally resolved during the implementation task because it is a mutable external registry fact; the task requires recording an immutable result before Compose is accepted.
- **Consistency:** `make check` invokes the validators and quality gates introduced in earlier tasks. The frontend remains inside `web`, and the workspace lockfile is consistently named `pnpm-lock.yaml` at the repository root.
