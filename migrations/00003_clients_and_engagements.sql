-- +goose Up
CREATE TABLE clients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (btrim(name) <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT clients_organization_id_id_key UNIQUE (organization_id, id),
    CONSTRAINT clients_organization_id_fkey
        FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT
);

CREATE TABLE engagements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    client_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (btrim(name) <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT engagements_organization_id_id_key UNIQUE (organization_id, id),
    CONSTRAINT engagements_organization_id_fkey
        FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
    CONSTRAINT engagements_organization_id_client_id_fkey
        FOREIGN KEY (organization_id, client_id)
        REFERENCES clients (organization_id, id) ON DELETE RESTRICT
);

-- +goose Down
DROP TABLE engagements;
DROP TABLE clients;
