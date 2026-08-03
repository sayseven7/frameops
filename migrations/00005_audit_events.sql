-- +goose Up
CREATE TABLE audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    actor_user_id UUID,
    action TEXT NOT NULL CHECK (btrim(action) <> ''),
    target_type TEXT NOT NULL CHECK (btrim(target_type) <> ''),
    target_id UUID NOT NULL,
    outcome TEXT NOT NULL CHECK (btrim(outcome) <> ''),
    correlation_id UUID NOT NULL,
    context JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT audit_events_organization_id_fkey
        FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
    CONSTRAINT audit_events_organization_id_actor_user_id_fkey
        FOREIGN KEY (organization_id, actor_user_id)
        REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

-- Audit retention and tombstones are future policy; they do not permit deleting audit events.
-- +goose StatementBegin
CREATE FUNCTION prevent_audit_event_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'audit events are append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER audit_events_reject_mutation
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_event_mutation();

-- +goose Down
DROP TRIGGER audit_events_reject_mutation ON audit_events;
DROP FUNCTION prevent_audit_event_mutation();
DROP TABLE audit_events;
