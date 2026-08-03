-- +goose Up
CREATE TABLE report_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'stored')),
    storage_key TEXT NOT NULL GENERATED ALWAYS AS ('organizations/' || organization_id::TEXT || '/engagements/' || engagement_id::TEXT || '/reports/' || id::TEXT) STORED,
    filename TEXT NOT NULL CHECK (btrim(filename) <> ''),
    sha256 BYTEA NOT NULL CHECK (octet_length(sha256) = 32),
    byte_size BIGINT NOT NULL CHECK (byte_size > 0),
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    stored_at TIMESTAMPTZ,
    approved_at TIMESTAMPTZ,
    imported_by UUID NOT NULL,
    approved_by UUID,
    CONSTRAINT report_revisions_state_consistency CHECK ((state = 'pending' AND stored_at IS NULL AND approved_at IS NULL AND approved_by IS NULL) OR (state = 'stored' AND stored_at IS NOT NULL)),
    FOREIGN KEY (organization_id, engagement_id) REFERENCES engagements (organization_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (organization_id, imported_by) REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT,
    FOREIGN KEY (organization_id, approved_by) REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT,
    UNIQUE (storage_key)
);
CREATE UNIQUE INDEX report_revisions_one_approved_per_engagement_idx ON report_revisions (organization_id, engagement_id) WHERE approved_at IS NOT NULL;
CREATE INDEX report_revisions_organization_id_engagement_id_idx ON report_revisions (organization_id, engagement_id);

-- +goose StatementBegin
CREATE FUNCTION prevent_report_revision_rewrite() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'report revisions are immutable'; END IF;
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.organization_id IS DISTINCT FROM OLD.organization_id OR NEW.engagement_id IS DISTINCT FROM OLD.engagement_id OR NEW.filename IS DISTINCT FROM OLD.filename OR NEW.sha256 IS DISTINCT FROM OLD.sha256 OR NEW.byte_size IS DISTINCT FROM OLD.byte_size OR NEW.received_at IS DISTINCT FROM OLD.received_at OR NEW.imported_by IS DISTINCT FROM OLD.imported_by THEN RAISE EXCEPTION 'report revision metadata is immutable'; END IF;
    IF OLD.state = 'pending' AND NEW.state = 'stored' AND OLD.stored_at IS NULL AND NEW.stored_at IS NOT NULL AND OLD.approved_at IS NULL AND NEW.approved_at IS NULL AND OLD.approved_by IS NULL AND NEW.approved_by IS NULL THEN RETURN NEW; END IF;
    IF OLD.state = 'stored' AND NEW.state = 'stored' AND OLD.stored_at = NEW.stored_at AND OLD.approved_at IS NULL AND NEW.approved_at IS NOT NULL AND OLD.approved_by IS NULL AND NEW.approved_by IS NOT NULL THEN RETURN NEW; END IF;
    RAISE EXCEPTION 'report revisions allow only pending to stored and one stored approval transition';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER report_revisions_reject_rewrite BEFORE UPDATE OR DELETE ON report_revisions FOR EACH ROW EXECUTE FUNCTION prevent_report_revision_rewrite();

-- +goose Down
DROP TRIGGER report_revisions_reject_rewrite ON report_revisions;
DROP FUNCTION prevent_report_revision_rewrite();
DROP INDEX report_revisions_one_approved_per_engagement_idx;
DROP TABLE report_revisions;
