-- +goose Up
-- A delivered PDF is never an independent rendering of project data: it is the
-- conversion of the exact DOCX revision an approver accepted. This unique key
-- lets the derived PDF reference the revision together with the digest of the
-- bytes that were approved, so provenance is enforced by the schema instead of
-- by the caller passing the digest it believes is right.
ALTER TABLE report_revisions
    ADD CONSTRAINT report_revisions_organization_id_id_sha256_key UNIQUE (organization_id, id, sha256);

CREATE TABLE report_pdfs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    revision_id UUID NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'stored')),
    -- The object key is derived by the database from identifiers this row already
    -- owns, and it keeps the derived PDF beside the revision it came from.
    storage_key TEXT NOT NULL GENERATED ALWAYS AS (
        'organizations/' || organization_id::TEXT
        || '/engagements/' || engagement_id::TEXT
        || '/reports/' || revision_id::TEXT
        || '/pdf/' || id::TEXT
    ) STORED,
    -- Provenance of the conversion: the digest of the approved DOCX and the
    -- identification of the converter that read exactly those bytes.
    source_sha256 BYTEA NOT NULL CHECK (octet_length(source_sha256) = 32),
    converter TEXT NOT NULL CHECK (btrim(converter) <> ''),
    sha256 BYTEA NOT NULL CHECK (octet_length(sha256) = 32),
    byte_size BIGINT NOT NULL CHECK (byte_size > 0),
    derived_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    stored_at TIMESTAMPTZ,
    derived_by UUID NOT NULL,
    CONSTRAINT report_pdfs_storage_key_key UNIQUE (storage_key),
    -- PostgreSQL and the object store share no transaction, so 'pending' is the
    -- explicit intermediate state of a conversion whose bytes the object store
    -- has not confirmed yet. A conversion that never stores stays readable for
    -- reconciliation and is never reported as a delivered PDF.
    CONSTRAINT report_pdfs_state_consistency CHECK (
        (state = 'pending' AND stored_at IS NULL)
        OR (state = 'stored' AND stored_at IS NOT NULL)
    ),
    CONSTRAINT report_pdfs_organization_id_revision_id_source_sha256_fkey
        FOREIGN KEY (organization_id, revision_id, source_sha256)
        REFERENCES report_revisions (organization_id, id, sha256) ON DELETE RESTRICT,
    CONSTRAINT report_pdfs_organization_id_engagement_id_fkey
        FOREIGN KEY (organization_id, engagement_id)
        REFERENCES engagements (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT report_pdfs_organization_id_derived_by_fkey
        FOREIGN KEY (organization_id, derived_by)
        REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

-- One revision delivers at most one PDF. The index covers stored rows only, so a
-- conversion whose upload failed leaves an auditable pending row without
-- permanently blocking a new attempt.
CREATE UNIQUE INDEX report_pdfs_one_stored_per_revision_idx
    ON report_pdfs (organization_id, revision_id) WHERE state = 'stored';
CREATE INDEX report_pdfs_organization_id_revision_id_idx ON report_pdfs (organization_id, revision_id);

-- A derived PDF is as immutable as the revision it came from: only the single
-- pending to stored transition is allowed, and provenance is never rewritten.
-- +goose StatementBegin
CREATE FUNCTION prevent_report_pdf_rewrite() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'derived report PDFs are immutable'; END IF;
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.engagement_id IS DISTINCT FROM OLD.engagement_id OR NEW.revision_id IS DISTINCT FROM OLD.revision_id OR NEW.source_sha256 IS DISTINCT FROM OLD.source_sha256 OR NEW.converter IS DISTINCT FROM OLD.converter OR NEW.sha256 IS DISTINCT FROM OLD.sha256 OR NEW.byte_size IS DISTINCT FROM OLD.byte_size OR NEW.derived_at IS DISTINCT FROM OLD.derived_at OR NEW.derived_by IS DISTINCT FROM OLD.derived_by THEN RAISE EXCEPTION 'derived report PDF provenance is immutable'; END IF;
    IF OLD.state = 'pending' AND NEW.state = 'stored' AND OLD.stored_at IS NULL AND NEW.stored_at IS NOT NULL THEN RETURN NEW; END IF;
    RAISE EXCEPTION 'derived report PDFs allow only the pending to stored transition';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER report_pdfs_reject_rewrite BEFORE UPDATE OR DELETE ON report_pdfs FOR EACH ROW EXECUTE FUNCTION prevent_report_pdf_rewrite();

-- +goose Down
DROP TRIGGER report_pdfs_reject_rewrite ON report_pdfs;
DROP FUNCTION prevent_report_pdf_rewrite();
DROP INDEX report_pdfs_organization_id_revision_id_idx;
DROP INDEX report_pdfs_one_stored_per_revision_idx;
DROP TABLE report_pdfs;
ALTER TABLE report_revisions DROP CONSTRAINT report_revisions_organization_id_id_sha256_key;
