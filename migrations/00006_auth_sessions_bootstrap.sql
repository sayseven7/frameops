-- +goose Up
ALTER TABLE users ADD COLUMN password_hash TEXT;

CREATE TABLE bootstrap_consumptions (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    consumed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    organization_id UUID NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    csrf_hash BYTEA NOT NULL CHECK (octet_length(csrf_hash) = 32),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sessions_organization_id_user_id_fkey
        FOREIGN KEY (organization_id, user_id)
        REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

CREATE INDEX sessions_active_token_hash_idx ON sessions (token_hash) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE sessions;
DROP TABLE bootstrap_consumptions;
ALTER TABLE users DROP COLUMN password_hash;
