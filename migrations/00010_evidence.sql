-- +goose Up
-- Evidence is proof, not an attachment. The row records the custody metadata of
-- one capture: the digest and size the server computed over the bytes it read,
-- the media type it detected from those bytes, the media type the client merely
-- declared, the capture instant the client reported and the instant the server
-- received it. PostgreSQL and the object store do not share a transaction, so
-- 'pending' is the explicit intermediate state of a capture whose bytes the
-- object store has not confirmed yet, and it stays readable for reconciliation
-- instead of being deleted. Only that one state change may ever be applied.
CREATE TABLE evidence (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    finding_id UUID NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'stored')),
    -- The object key is derived by the database from identifiers this row already
    -- owns, so no client input ever reaches the object store's namespace and two
    -- captures can never address the same object.
    storage_key TEXT NOT NULL GENERATED ALWAYS AS (
        'organizations/' || organization_id::TEXT
        || '/engagements/' || engagement_id::TEXT
        || '/evidence/' || id::TEXT
    ) STORED,
    filename TEXT NOT NULL CHECK (btrim(filename) <> ''),
    -- Declared by the client and never trusted; detected by the server from the
    -- persisted bytes and used as the stored truth.
    declared_media_type TEXT NOT NULL,
    detected_media_type TEXT NOT NULL CHECK (btrim(detected_media_type) <> ''),
    sha256 BYTEA NOT NULL CHECK (octet_length(sha256) = 32),
    byte_size BIGINT NOT NULL CHECK (byte_size > 0),
    captured_at TIMESTAMPTZ,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    stored_at TIMESTAMPTZ,
    captured_by UUID NOT NULL,
    CONSTRAINT evidence_storage_key_key UNIQUE (storage_key),
    CONSTRAINT evidence_state_consistency CHECK (
        (state = 'pending' AND stored_at IS NULL)
        OR (state = 'stored' AND stored_at IS NOT NULL)
    ),
    -- Evidence never cascades with its finding: the finding cannot be removed
    -- while its chain of custody exists.
    CONSTRAINT evidence_organization_id_engagement_id_finding_id_fkey
        FOREIGN KEY (organization_id, engagement_id, finding_id)
        REFERENCES findings (organization_id, engagement_id, id) ON DELETE RESTRICT,
    CONSTRAINT evidence_organization_id_captured_by_fkey
        FOREIGN KEY (organization_id, captured_by)
        REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

CREATE INDEX evidence_organization_id_finding_id_idx ON evidence (organization_id, finding_id);

-- Authorized physical discard is an exceptional, still undecided policy; it will
-- advance the schema and preserve a tombstone. Until then no path rewrites bytes,
-- digest, or historical metadata, and a correction is a new evidence row.
-- +goose StatementBegin
CREATE FUNCTION prevent_evidence_rewrite() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'evidence is logically immutable and cannot be deleted';
    END IF;
    IF OLD.state <> 'pending' OR NEW.state <> 'stored' THEN
        RAISE EXCEPTION 'evidence state advances only once, from pending to stored';
    END IF;
    IF (NEW.id, NEW.organization_id, NEW.engagement_id, NEW.finding_id, NEW.filename,
        NEW.declared_media_type, NEW.detected_media_type, NEW.sha256, NEW.byte_size,
        NEW.captured_at, NEW.received_at, NEW.captured_by)
       IS DISTINCT FROM
       (OLD.id, OLD.organization_id, OLD.engagement_id, OLD.finding_id, OLD.filename,
        OLD.declared_media_type, OLD.detected_media_type, OLD.sha256, OLD.byte_size,
        OLD.captured_at, OLD.received_at, OLD.captured_by) THEN
        RAISE EXCEPTION 'evidence custody metadata is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER evidence_reject_rewrite
    BEFORE UPDATE OR DELETE ON evidence
    FOR EACH ROW EXECUTE FUNCTION prevent_evidence_rewrite();

-- +goose Down
DROP TRIGGER evidence_reject_rewrite ON evidence;
DROP FUNCTION prevent_evidence_rewrite();
DROP TABLE evidence;
