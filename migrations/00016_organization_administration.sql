-- +goose Up
ALTER TABLE organization_memberships ADD COLUMN is_active BOOLEAN NOT NULL DEFAULT TRUE;
CREATE INDEX audit_events_organization_created_at_id_idx ON audit_events (organization_id, created_at DESC, id DESC);

-- +goose Down
DROP INDEX audit_events_organization_created_at_id_idx;
ALTER TABLE organization_memberships DROP COLUMN is_active;
