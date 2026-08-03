-- +goose Up
CREATE TABLE organization_memberships (
    organization_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'member')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id),
    CONSTRAINT organization_memberships_organization_id_fkey
        FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
    CONSTRAINT organization_memberships_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- +goose Down
DROP TABLE organization_memberships;
