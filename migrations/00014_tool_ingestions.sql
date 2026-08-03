-- +goose Up
CREATE TABLE tool_ingestions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    tool TEXT NOT NULL CHECK (tool IN ('nmap')),
    format_version TEXT NOT NULL CHECK (btrim(format_version) <> ''),
    filename TEXT NOT NULL CHECK (btrim(filename) <> ''),
    sha256 BYTEA NOT NULL CHECK (octet_length(sha256) = 32),
    byte_size BIGINT NOT NULL CHECK (byte_size > 0),
    items_read INTEGER NOT NULL CHECK (items_read >= 0),
    items_created INTEGER NOT NULL CHECK (items_created >= 0),
    items_reused INTEGER NOT NULL CHECK (items_reused >= 0),
    items_ignored INTEGER NOT NULL CHECK (items_ignored >= 0),
    items_rejected INTEGER NOT NULL CHECK (items_rejected >= 0),
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    imported_by UUID NOT NULL,
    -- One summary can only describe the items it read, so an incoherent report
    -- of what an import did to an engagement cannot be persisted at all.
    CONSTRAINT tool_ingestions_summary_consistency
        CHECK (items_read = items_created + items_reused + items_ignored + items_rejected),
    CONSTRAINT tool_ingestions_organization_id_id_key UNIQUE (organization_id, id),
    -- The same artifact is imported at most once into one engagement, so a
    -- replayed upload is an explicit conflict instead of a silent second import.
    CONSTRAINT tool_ingestions_artifact_key UNIQUE (organization_id, engagement_id, sha256),
    CONSTRAINT tool_ingestions_organization_id_engagement_id_fkey
        FOREIGN KEY (organization_id, engagement_id)
        REFERENCES engagements (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT tool_ingestions_imported_by_fkey
        FOREIGN KEY (organization_id, imported_by)
        REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);
CREATE INDEX tool_ingestions_organization_id_engagement_id_idx
    ON tool_ingestions (organization_id, engagement_id);

-- +goose StatementBegin
CREATE FUNCTION prevent_tool_ingestion_rewrite() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'tool ingestions are immutable';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER tool_ingestions_reject_rewrite BEFORE UPDATE OR DELETE ON tool_ingestions
    FOR EACH ROW EXECUTE FUNCTION prevent_tool_ingestion_rewrite();

-- An asset carries where it came from: an operator typed it, or exactly one
-- ingestion created it. The identifier is deferrable because the summary of an
-- ingestion is only known after its assets are inserted.
ALTER TABLE assets
    ADD COLUMN source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'ingest')),
    ADD COLUMN ingestion_id UUID,
    ADD CONSTRAINT assets_source_consistency
        CHECK ((source = 'manual' AND ingestion_id IS NULL) OR (source = 'ingest' AND ingestion_id IS NOT NULL)),
    ADD CONSTRAINT assets_ingestion_id_fkey
        FOREIGN KEY (organization_id, ingestion_id)
        REFERENCES tool_ingestions (organization_id, id) ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED;

-- Inside one engagement an asset is identified by its name, so a re-imported
-- host is reused instead of duplicated.
CREATE UNIQUE INDEX assets_organization_id_engagement_id_name_key
    ON assets (organization_id, engagement_id, name);

-- +goose Down
DROP INDEX assets_organization_id_engagement_id_name_key;
ALTER TABLE assets
    DROP CONSTRAINT assets_ingestion_id_fkey,
    DROP CONSTRAINT assets_source_consistency,
    DROP COLUMN ingestion_id,
    DROP COLUMN source;
DROP TRIGGER tool_ingestions_reject_rewrite ON tool_ingestions;
DROP FUNCTION prevent_tool_ingestion_rewrite();
DROP TABLE tool_ingestions;
