-- +goose Up
LOCK TABLE report_pdfs IN ACCESS EXCLUSIVE MODE;

DROP INDEX report_pdfs_one_stored_per_revision_idx;
ALTER TABLE report_pdfs DROP CONSTRAINT report_pdfs_state_check;
ALTER TABLE report_pdfs DROP CONSTRAINT report_pdfs_state_consistency;
ALTER TABLE report_pdfs ADD COLUMN failed_at TIMESTAMPTZ;
ALTER TABLE report_pdfs ADD CONSTRAINT report_pdfs_state_check CHECK (state IN ('pending', 'stored', 'failed'));
ALTER TABLE report_pdfs ADD CONSTRAINT report_pdfs_state_consistency CHECK (
    (state = 'pending' AND stored_at IS NULL AND failed_at IS NULL)
    OR (state = 'stored' AND stored_at IS NOT NULL AND failed_at IS NULL)
    OR (state = 'failed' AND stored_at IS NULL AND failed_at IS NOT NULL)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION prevent_report_pdf_rewrite() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'derived report PDFs are immutable'; END IF;
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.engagement_id IS DISTINCT FROM OLD.engagement_id OR NEW.revision_id IS DISTINCT FROM OLD.revision_id OR NEW.source_sha256 IS DISTINCT FROM OLD.source_sha256 OR NEW.converter IS DISTINCT FROM OLD.converter OR NEW.sha256 IS DISTINCT FROM OLD.sha256 OR NEW.byte_size IS DISTINCT FROM OLD.byte_size OR NEW.derived_at IS DISTINCT FROM OLD.derived_at OR NEW.derived_by IS DISTINCT FROM OLD.derived_by THEN RAISE EXCEPTION 'derived report PDF provenance is immutable'; END IF;
    IF OLD.state = 'pending' AND NEW.state = 'stored' AND OLD.stored_at IS NULL AND NEW.stored_at IS NOT NULL AND NEW.failed_at IS NULL THEN RETURN NEW; END IF;
    IF OLD.state = 'pending' AND NEW.state = 'failed' AND OLD.failed_at IS NULL AND NEW.failed_at IS NOT NULL AND NEW.stored_at IS NULL THEN RETURN NEW; END IF;
    RAISE EXCEPTION 'derived report PDFs allow only pending to stored or failed transitions';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Preserve every legacy reservation while deterministically retiring duplicate
-- pending rows that the old stored-only index allowed.
WITH ranked AS (
    SELECT id, state, row_number() OVER (
        PARTITION BY organization_id, revision_id
        ORDER BY (state = 'stored') DESC, derived_at, id
    ) AS position
    FROM report_pdfs
    WHERE state IN ('pending', 'stored')
)
UPDATE report_pdfs pdf
SET state = 'failed', failed_at = now()
FROM ranked
WHERE pdf.id = ranked.id
  AND ranked.state = 'pending'
  AND (ranked.position > 1 OR pdf.derived_at <= now() - interval '5 minutes');

CREATE UNIQUE INDEX report_pdfs_one_effective_per_revision_key
    ON report_pdfs (organization_id, revision_id) WHERE state IN ('pending', 'stored');

-- +goose Down
LOCK TABLE report_pdfs IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM report_pdfs WHERE state = 'failed') THEN
        RAISE EXCEPTION 'cannot remove report PDF recovery while failed reservations exist';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP INDEX report_pdfs_one_effective_per_revision_key;
ALTER TABLE report_pdfs DROP CONSTRAINT report_pdfs_state_check;
ALTER TABLE report_pdfs DROP CONSTRAINT report_pdfs_state_consistency;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION prevent_report_pdf_rewrite() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'derived report PDFs are immutable'; END IF;
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.engagement_id IS DISTINCT FROM OLD.engagement_id OR NEW.revision_id IS DISTINCT FROM OLD.revision_id OR NEW.source_sha256 IS DISTINCT FROM OLD.source_sha256 OR NEW.converter IS DISTINCT FROM OLD.converter OR NEW.sha256 IS DISTINCT FROM OLD.sha256 OR NEW.byte_size IS DISTINCT FROM OLD.byte_size OR NEW.derived_at IS DISTINCT FROM OLD.derived_at OR NEW.derived_by IS DISTINCT FROM OLD.derived_by THEN RAISE EXCEPTION 'derived report PDF provenance is immutable'; END IF;
    IF OLD.state = 'pending' AND NEW.state = 'stored' AND OLD.stored_at IS NULL AND NEW.stored_at IS NOT NULL THEN RETURN NEW; END IF;
    RAISE EXCEPTION 'derived report PDFs allow only the pending to stored transition';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

ALTER TABLE report_pdfs DROP COLUMN failed_at;
ALTER TABLE report_pdfs ADD CONSTRAINT report_pdfs_state_check CHECK (state IN ('pending', 'stored'));
ALTER TABLE report_pdfs ADD CONSTRAINT report_pdfs_state_consistency CHECK (
    (state = 'pending' AND stored_at IS NULL)
    OR (state = 'stored' AND stored_at IS NOT NULL)
);
CREATE UNIQUE INDEX report_pdfs_one_stored_per_revision_idx
    ON report_pdfs (organization_id, revision_id) WHERE state = 'stored';