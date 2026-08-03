# FrameOPS Stage 2 — Infrastructure and Schema Implementation Plan

> **For Hermes:** Execute this plan task-by-task with test-driven development. Do not proceed past the decision gate in Task 1 without the responsible person's explicit approval.

**Goal:** Add reproducible local MinIO infrastructure and an initial PostgreSQL schema whose database constraints enforce organization ownership and append-only audit records, without implementing domain behavior, HTTP, authentication, CRUD, evidence uploads, CLI behavior, or UI.

**Architecture:** PostgreSQL remains the system of record for structured entities, ownership and audit history. A local MinIO service supplies only the later S3-compatible storage boundary; this stage does not create buckets, upload files, issue signed URLs, or write storage adapters. The schema establishes the minimum identities and operational hierarchy needed to enforce organization-scoped foreign keys: organization → client → engagement → asset, plus user membership and audit events.

**Tech Stack:** Go 1.26.5; PostgreSQL 18.4 pinned by digest; Docker Compose; MinIO pinned by exact tag and SHA-256 digest; pgx v5; goose migrations; pgx-based integration tests; pnpm workspace unchanged.

---

## Approved Stage 2 Boundary

**Included**
- A digest-pinned MinIO Compose service, local-only port binding and health check.
- Version-controlled SQL migrations and a reproducible migration command.
- Database extensions and tables for organizations, users, organization memberships, clients, engagements, assets and audit events.
- Constraints that prevent cross-organization links where the relationship is represented in this initial schema.
- Database-level immutability of audit events and integration tests against the real Compose PostgreSQL service.
- Documentation and `make` targets for migration lifecycle and schema tests.

**Explicitly excluded**
- Password hashes, sessions, PATs, bootstrap credentials, login or authorization queries (Stage 4).
- CVSS, finding state machines, templates, findings, retests, reports and evidence records (Stages 3, 5, 6, 7, 8 and 10).
- Bucket provisioning, object bytes, upload/reconciliation, signed URLs or a MinIO/pgx storage adapter.
- HTTP handlers, API routes, executable servers, functional CLI commands and product UI.
- Cascading deletion of any record; this stage introduces no destructive product operation.

## Decision Gate — Must Be Approved Before Task 2

The Stage 0 documents approve organization ownership and `admin`/`member` roles, but do not define a public user identifier or the full audit-event contract. Record this decision in a new Stage 2 specification before creating migrations:

1. **User identifier:** use a case-insensitive e-mail address as the initial unique identifier, stored with a UUID primary key. This supports the future authentication flow while avoiding a username policy that is not specified. No password or authentication-secret column is added in Stage 2.
2. **Schema hierarchy:** create organization-scoped clients, engagements and assets now. Each child has its own UUID primary key plus a unique `(organization_id, id)` key; child foreign keys include `organization_id` so PostgreSQL rejects a cross-organization parent link.
3. **Audit baseline:** append-only `audit_events` captures organization, optional actor user, action, target type, target UUID, outcome, correlation UUID, request metadata JSON and server timestamp. No secrets, tokens or evidence content may be persisted. The application-level event vocabulary is deferred to Stage 4.
4. **Deletion policy:** no cascade delete. Parent rows must be rejected while referenced; later logical deletion/tombstone behavior will arrive with its owning domain stage.

If the responsible person rejects any item, revise this plan and the Stage 2 spec before code is written.

## Task 1: Record and approve the Stage 2 schema decisions

**Objective:** Turn the boundary above into a reviewable decision record so migrations do not encode unapproved product policy.

**Files:**
- Create: `docs/superpowers/specs/2026-08-03-frameops-stage-2-infra-schema.md`
- Modify: `docs/development.md`

**Step 1: Write the decision record**

Document the approved boundary, the four decisions above, exclusions, and the acceptance criteria. Mark any unresolved item as a blocker rather than guessing.

**Step 2: Add an infrastructure note to development documentation**

Document that MinIO is a local development dependency only at this stage, exposes no product behavior, and uses non-secret placeholders in `.env.example`.

**Step 3: Review the record**

Run:

```bash
test -f docs/superpowers/specs/2026-08-03-frameops-stage-2-infra-schema.md
git diff --check
```

Expected: the decision record is present, contains no credential value, and has no whitespace errors.

**Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-08-03-frameops-stage-2-infra-schema.md docs/development.md
git commit -m "docs: approve Stage 2 schema boundary"
```

## Task 2: Add a pinned local MinIO service and Compose contract coverage

**Objective:** Extend the local stack with an immutable MinIO image without weakening PostgreSQL's existing Compose contract.

**Files:**
- Modify: `compose.yaml`
- Modify: `.env.example`
- Modify: `scripts/check-compose.sh`
- Modify: `docs/development.md`

**Step 1: Resolve the exact MinIO image reference**

Use the registry to select a current, stable `minio/minio` release compatible with the chosen local Compose environment. Record the exact tag and `@sha256:` digest in the Stage 2 decision record. Do not use `latest` or an unpinned tag.

**Step 2: Write the failing Compose assertions**

Extend `scripts/check-compose.sh` so it fails unless the rendered JSON configuration has:
- `services.minio`;
- the exact documented immutable image reference;
- exactly one localhost-only S3 API mapping, from `127.0.0.1:${FRAMEOPS_MINIO_PORT}` to container port `9000`;
- a named `frameops-minio-data` volume mounted at `/data`;
- a health check against MinIO's live-health endpoint;
- root user and password read from required environment-variable names; and
- no MinIO console port published.

Keep all existing PostgreSQL assertions unchanged.

**Step 3: Run the contract test and confirm RED**

Run:

```bash
bash scripts/check-compose.sh
```

Expected: FAIL because MinIO is not yet in the Compose file.

**Step 4: Implement the minimum service**

Add a `minio` service with command `server /data`, exact immutable image, named volume, localhost-only S3 port, health check and required variable expansion. Add only clearly fake local placeholders to `.env.example`; do not add a bucket, policy, application credentials or production-looking secret.

**Step 5: Verify rendered configuration and runtime health**

Run:

```bash
bash scripts/check-compose.sh
docker compose --env-file .env.example up -d postgres minio
docker compose --env-file .env.example ps
docker compose --env-file .env.example down
```

Expected: the contract check passes; both services become healthy; shutdown removes only containers and network, not named volumes.

**Step 6: Commit**

```bash
git add compose.yaml .env.example scripts/check-compose.sh docs/development.md docs/superpowers/specs/2026-08-03-frameops-stage-2-infra-schema.md
git commit -m "build: add pinned local MinIO service"
```

## Task 3: Add migration tooling and an empty migration lifecycle test

**Objective:** Establish an immutable, repeatable migration mechanism before adding product tables.

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `migrations/`
- Create: `scripts/migrate.sh`
- Create: `scripts/schema_test.sh`
- Modify: `Makefile`
- Modify: `docs/development.md`

**Step 1: Pin migration and database dependencies**

Add only the approved migration runner (`github.com/pressly/goose/v3`) and PostgreSQL driver (`github.com/jackc/pgx/v5`) at explicit reviewed versions. Use `go mod tidy` and inspect `go.mod`/`go.sum`; do not introduce an ORM, HTTP framework or application behavior.

**Step 2: Write a failing lifecycle test**

Create `scripts/schema_test.sh` that:
1. starts the real PostgreSQL service with `.env.example`;
2. creates a uniquely named disposable database through the admin database;
3. applies migrations to that database through `scripts/migrate.sh up`;
4. asserts that the database reaches the expected migration version;
5. runs `down-to 0` (or drops the disposable database only after successful assertions); and
6. always performs cleanup using a shell `trap`.

The script must fail when no migration directory or migration command exists and must never target the regular development database.

**Step 3: Confirm RED**

Run:

```bash
bash scripts/schema_test.sh
```

Expected: FAIL because migration tooling and migrations have not yet been created.

**Step 4: Implement migration runner and Make targets**

Implement `scripts/migrate.sh` with explicit `up`, `status` and `down-to` subcommands. Require `FRAMEOPS_DATABASE_URL`; reject an empty value and reject a URL not explicitly supplied by the caller. Add `make migrate-status`, `make migrate-up` and `make schema-test`; make `check` invoke `schema-test` only after Compose validation.

**Step 5: Confirm GREEN**

Run:

```bash
bash scripts/schema_test.sh
make schema-test
```

Expected: both exit zero against a newly created disposable PostgreSQL database. No migration should target the persistent local development database during test execution.

**Step 6: Commit**

```bash
git add go.mod go.sum migrations scripts/migrate.sh scripts/schema_test.sh Makefile docs/development.md
git commit -m "build: add reproducible PostgreSQL migration lifecycle"
```

## Task 4: Create the identity and organization ownership migration

**Objective:** Persist the approved ownership boundary and role vocabulary with constraints enforced by PostgreSQL.

**Files:**
- Create: `migrations/00001_identity_and_organizations.sql`
- Modify: `scripts/schema_test.sh` or create `internal/store/postgres/schema_test.go`

**Step 1: Write failing real-database tests**

Add integration assertions that prove:
- UUID defaults are generated server-side for organizations and users;
- a user e-mail cannot be duplicated case-insensitively;
- a membership can contain only `admin` or `member` roles;
- a membership cannot point to a nonexistent organization or user; and
- duplicate membership for the same organization/user pair is rejected.

Use generated UUIDs and synthetic addresses only. Do not use credentials, customer names or real evidence.

**Step 2: Confirm RED**

Run the focused schema test against a freshly migrated disposable database.

Expected: FAIL because the identity and ownership tables do not exist.

**Step 3: Add the migration**

In an immutable new migration:
- enable only required PostgreSQL extensions (`pgcrypto` for UUID generation and `citext` for the canonical e-mail uniqueness constraint);
- create `organizations` with a UUID key, non-empty name and server-managed creation timestamp;
- create `users` with UUID key, non-empty display name, case-insensitive unique e-mail, active flag and creation timestamp, but no password or auth secret;
- create `organization_memberships` with a composite primary key `(organization_id, user_id)`, constrained role vocabulary, and restrictive foreign keys; and
- include an explicit `Down` section which drops in dependency-safe order.

Use `ON DELETE RESTRICT`/the PostgreSQL restrictive default rather than `CASCADE`.

**Step 4: Confirm GREEN**

Run the focused integration assertions and `bash scripts/schema_test.sh`.

Expected: all allowed inserts work and each invalid case is rejected by PostgreSQL.

**Step 5: Commit**

```bash
git add migrations/00001_identity_and_organizations.sql scripts/schema_test.sh internal/store/postgres/schema_test.go
git commit -m "feat: enforce organization membership ownership"
```

## Task 5: Create the organization-scoped operational hierarchy migration

**Objective:** Enforce ownership propagation for clients, engagements and assets at the database boundary.

**Files:**
- Create: `migrations/00002_operational_hierarchy.sql`
- Modify: `internal/store/postgres/schema_test.go` or `scripts/schema_test.sh`

**Step 1: Write failing cross-organization tests**

Using two organizations and fully synthetic rows, prove that:
- a client belongs to exactly one organization;
- an engagement accepts a client only from the same organization;
- an asset accepts an engagement only from the same organization;
- removing a referenced organization/client/engagement is rejected; and
- valid same-organization inserts succeed.

**Step 2: Confirm RED**

Run the focused test. Expected: FAIL because the hierarchy tables and composite ownership keys do not exist.

**Step 3: Add the migration**

Create `clients`, `engagements` and `assets` with UUID primary keys, `organization_id`, required human-readable names, creation timestamps, and unique `(organization_id, id)` keys. Add composite foreign keys:

```text
engagements (organization_id, client_id) → clients (organization_id, id)
assets (organization_id, engagement_id) → engagements (organization_id, id)
```

Add no domain statuses, scope details, ROE, methodology, findings or evidence columns; those are owned by later stages.

**Step 4: Confirm GREEN**

Run the focused cross-organization test and the full migration lifecycle test.

Expected: same-owner relationships succeed; every cross-owner relationship and destructive parent operation is rejected.

**Step 5: Commit**

```bash
git add migrations/00002_operational_hierarchy.sql internal/store/postgres/schema_test.go scripts/schema_test.sh
git commit -m "feat: enforce organization-scoped project hierarchy"
```

## Task 6: Add append-only audit schema and database trigger coverage

**Objective:** Make the audit table immutable at the database boundary before future structured mutations are added.

**Files:**
- Create: `migrations/00003_audit_events.sql`
- Modify: `internal/store/postgres/schema_test.go` or `scripts/schema_test.sh`
- Modify: `docs/development.md`

**Step 1: Write failing audit tests**

Prove with real PostgreSQL that:
- a valid audit event can be inserted;
- an event actor, when supplied, must be a member of the same organization;
- update and delete attempts against `audit_events` are rejected even when issued directly through the database role used by the integration test; and
- a new event is still insertable after rejected mutation attempts.

**Step 2: Confirm RED**

Run the focused test. Expected: FAIL because audit event storage and immutable triggers do not exist.

**Step 3: Add the migration**

Create `audit_events` with UUID key, organization ownership, nullable actor user, action, target type, target UUID, outcome, correlation UUID, request metadata JSONB and server timestamp. Add a composite foreign key for `(organization_id, actor_user_id)` to the membership boundary when actor is non-null. Add `BEFORE UPDATE OR DELETE` triggers that raise an exception. The trigger function and comments must explicitly state that audit retention/tombstones are future policy, not permission to remove audit rows.

**Step 4: Confirm GREEN**

Run the focused audit test, then the full migration lifecycle test.

Expected: append succeeds, invalid actor/update/delete fail, and migration replays cleanly.

**Step 5: Commit**

```bash
git add migrations/00003_audit_events.sql internal/store/postgres/schema_test.go scripts/schema_test.sh docs/development.md
git commit -m "feat: add append-only audit event schema"
```

## Task 7: Run the complete Stage 2 acceptance gate and review the worktree

**Objective:** Produce evidence that the infrastructure and schema are reproducible and satisfy the approved boundary.

**Files:**
- Modify only files needed to fix a failure discovered by validation.

**Step 1: Run all quality gates**

Run:

```bash
make check
bash scripts/schema_test.sh
docker compose --env-file .env.example up -d postgres minio
docker compose --env-file .env.example ps
docker compose --env-file .env.example down
git diff --check
git status --short
```

Expected: toolchain, Compose contract, Go format/lint/test, frozen frontend checks, real schema tests and both dependency health checks pass. The worktree contains only intentional Stage 2 files plus the pre-existing untracked local agent/prompt/plan artifacts.

**Step 2: Confirm Stage 2 acceptance criteria**

- [ ] PostgreSQL and MinIO use exact image tags and SHA-256 digests; no floating images are introduced.
- [ ] MinIO does not expose its console and contains no bucket or object-storage product behavior.
- [ ] Migrations can apply from zero and replay on a fresh disposable database.
- [ ] Migrations are versioned and future corrections are new migrations, never edits to an applied one.
- [ ] The schema rejects invalid roles, missing owners, duplicate organization memberships and cross-organization client/engagement/asset links.
- [ ] Database audit events cannot be updated or deleted.
- [ ] No cascade delete can remove historical data through the initial hierarchy.
- [ ] No endpoint, authentication flow, CRUD workflow, evidence upload, CLI function or product UI is introduced.
- [ ] No secrets, client data or real evidence appear in the repository, tests, documentation or logs.

**Step 3: Commit**

```bash
git add Makefile compose.yaml .env.example go.mod go.sum migrations scripts internal/store/postgres docs/development.md docs/superpowers/specs/2026-08-03-frameops-stage-2-infra-schema.md
git commit -m "test: verify Stage 2 infrastructure and schema"
```

## Risks and Deliberate Deferrals

- Authentication and bootstrap policy remains owned by Stage 4; this plan intentionally avoids storing credential material.
- The audit schema is immutable but does not yet implement application event vocabulary or transaction wiring; both require authenticated actors and mutable API flows.
- MinIO has no buckets or product access path in this stage. Introducing one would prematurely implement Stage 6.
- Resource limits, retention and legal-hold policy remain deferred to the stages that own evidence and retention behavior.
- The initial hierarchy uses restrictive deletion. Later lifecycle semantics require a new migration and explicit domain policy, never an edit of an applied migration.
