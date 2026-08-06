-- +goose Up
CREATE TABLE engagement_plans (
    organization_id UUID NOT NULL,
    engagement_id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'closed')),
    starts_on DATE NOT NULL,
    ends_on DATE NOT NULL,
    rules_of_engagement TEXT NOT NULL CHECK (btrim(rules_of_engagement) <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_on >= starts_on),
    CONSTRAINT engagement_plans_organization_engagement_key UNIQUE (organization_id, engagement_id),
    CONSTRAINT engagement_plans_organization_engagement_fkey FOREIGN KEY (organization_id, engagement_id) REFERENCES engagements (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT engagement_plans_owner_fkey FOREIGN KEY (organization_id, owner_user_id) REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

CREATE TABLE engagement_scope_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    version_number INTEGER NOT NULL CHECK (version_number > 0),
    targets JSONB NOT NULL CHECK (jsonb_typeof(targets) = 'array' AND jsonb_array_length(targets) > 0),
    exclusions JSONB NOT NULL DEFAULT '[]'::JSONB CHECK (jsonb_typeof(exclusions) = 'array'),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT engagement_scope_versions_number_key UNIQUE (organization_id, engagement_id, version_number),
    CONSTRAINT engagement_scope_versions_engagement_fkey FOREIGN KEY (organization_id, engagement_id) REFERENCES engagements (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT engagement_scope_versions_creator_fkey FOREIGN KEY (organization_id, created_by) REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

CREATE TABLE engagement_team_members (
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('lead', 'tester', 'reviewer')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, engagement_id, user_id),
    CONSTRAINT engagement_team_members_engagement_fkey FOREIGN KEY (organization_id, engagement_id) REFERENCES engagements (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT engagement_team_members_user_fkey FOREIGN KEY (organization_id, user_id) REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

CREATE TABLE engagement_milestones (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    title TEXT NOT NULL CHECK (btrim(title) <> ''),
    due_on DATE NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT engagement_milestones_organization_id_id_key UNIQUE (organization_id, id),
    CONSTRAINT engagement_milestones_engagement_fkey FOREIGN KEY (organization_id, engagement_id) REFERENCES engagements (organization_id, id) ON DELETE RESTRICT
);

-- +goose StatementBegin
CREATE FUNCTION prevent_engagement_scope_version_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'engagement scope versions are immutable';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER engagement_scope_versions_reject_mutation
    BEFORE UPDATE OR DELETE ON engagement_scope_versions
    FOR EACH ROW EXECUTE FUNCTION prevent_engagement_scope_version_mutation();

-- +goose Down
DROP TRIGGER engagement_scope_versions_reject_mutation ON engagement_scope_versions;
DROP FUNCTION prevent_engagement_scope_version_mutation();
DROP TABLE engagement_milestones;
DROP TABLE engagement_team_members;
DROP TABLE engagement_scope_versions;
DROP TABLE engagement_plans;
