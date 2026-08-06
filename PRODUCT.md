# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Go backend and HTTP API; Next.js/React frontend; PostgreSQL for structured data; MinIO/S3-compatible storage for evidence and documents; pnpm for frontend package management. Exact supported versions remain to be approved in Stage 0.

## Users

The initial users are internal pentesters working on multiple client engagements. They need to capture findings and evidence during testing, collaborate inside an organization, perform retests, and produce auditable deliverables without maintaining parallel notes.

## Product Purpose

FrameOPS manages pentest engagements, evidence, findings, retests, and reports. Its success criterion is completing a real pentest from planning through the delivered report without relying on Obsidian or another parallel record.

## Positioning

Capture once at the moment of discovery, then turn the same structured and traceable data into an approved DOCX and its derived PDF. Evidence integrity, custody, and ownership isolation are product requirements rather than optional workflow features.

## Operating Context

Operators use a Go CLI during testing, tool-output ingestion, and a web UI for planning, review, and consolidation. The web UI is multilingual from the start: `pt-BR` is the initial locale and `en` is supported in the MVP. The backend API is the only regular entry point for CLI, UI, and ingestors; only the local first-admin bootstrap CLI may access PostgreSQL directly.

## Capabilities and Constraints

- Ownership is organization-based: users are organization members with `admin` or `member` roles; there is no cross-organization superadministrator in the MVP.
- Cross-owner access returns 404 to non-administrative callers and structured mutations are audited.
- Evidence is logically immutable and has no automatic expiration. Authorized physical discard is exceptional, requires an explicit legal obligation or authorization, preserves an auditable tombstone and metadata, and must respect legal hold.
- PostgreSQL and object storage use explicit intermediate states and reconciliation, never a claimed distributed transaction.
- The MVP CLI is online-only. Offline capture queues are out of scope.
- Document generation and PDF conversion run in an isolated worker with no direct database, object-storage, or internet access.
- The MVP excludes storage of test credentials and secrets.
- CVSS v3.1 is the supported scoring version, tested against FIRST vectors; later versions must use a versioned boundary.
- Methodology templates are organization-scoped, customizable, and copied as immutable checklist versions when an engagement is created. They are original structured content inspired by cited sources, with source version and attribution per template.
- Templates can be created in the UI and imported as validated, versioned JSON packages. A duplicate template identifier always creates a new version; imports never overwrite existing versions.
- A `member` may create and edit their own template drafts. Only an `admin` may import packages or publish a shared-library version.
- Report DOCX revisions are immutable. At most one revision is approved at a time, and a PDF is derived only from that exact approved DOCX revision.
- First-admin creation is a local CLI operation protected by an out-of-database bootstrap credential that is destroyed after use; no HTTP bootstrap endpoint exists.
- Exact resource limits and the supported-version matrix remain open decisions.

## Brand Commitments

The product communicates security, precision, technical confidence, and legibility through the approved FrameSeven Ops Deck visual direction: a dense, near-black operational desk where green denotes authorized scope/progress, cyan identifies data/evidence, and orange signals review. Light and system are semantic variations of this same identity, not separate brands. The UI uses semantic design tokens, keyboard navigation, visible focus, and written severity/state labels so color never carries meaning alone.

## Evidence on Hand

The repository currently contains the canonical product brief at `FrameOPS-prompt-mestre.md`. There are no implementation files, design assets, customer proof points, or real evidence fixtures. Future UI and documentation must not fabricate any of them.

## Product Principles

1. Integrity and confidentiality outrank convenience.
2. Capture must fit the pentester's existing flow.
3. Historical records and approved deliverables are traceable and immutable.
4. Ownership isolation is enforced by the schema and server, not user discipline.
5. Human editorial review remains part of report delivery.

## Accessibility & Inclusion

The web UI requires accessible keyboard navigation, visible focus, adequate contrast, localized strings, and non-color-only status communication.
