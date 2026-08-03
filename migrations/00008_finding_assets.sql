-- +goose Up
ALTER TABLE assets ADD CONSTRAINT assets_organization_id_engagement_id_id_key UNIQUE (organization_id, engagement_id, id);
ALTER TABLE findings ADD CONSTRAINT findings_organization_id_engagement_id_id_key UNIQUE (organization_id, engagement_id, id);

-- The engagement is carried on every row so both ends of the link are proven, by
-- foreign key, to belong to the same organization and the same engagement.
CREATE TABLE finding_assets (
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    finding_id UUID NOT NULL,
    asset_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (finding_id, asset_id),
    CONSTRAINT finding_assets_organization_id_engagement_id_finding_id_fkey
        FOREIGN KEY (organization_id, engagement_id, finding_id)
        REFERENCES findings (organization_id, engagement_id, id) ON DELETE RESTRICT,
    CONSTRAINT finding_assets_organization_id_engagement_id_asset_id_fkey
        FOREIGN KEY (organization_id, engagement_id, asset_id)
        REFERENCES assets (organization_id, engagement_id, id) ON DELETE RESTRICT
);

CREATE INDEX finding_assets_organization_id_asset_id_idx ON finding_assets (organization_id, asset_id);

-- +goose Down
DROP TABLE finding_assets;
ALTER TABLE findings DROP CONSTRAINT findings_organization_id_engagement_id_id_key;
ALTER TABLE assets DROP CONSTRAINT assets_organization_id_engagement_id_id_key;
