-- +goose Up
-- A methodology template is the organization's own executable checklist of what
-- to test. Its content lives in versions rather than in the template itself: a
-- draft its author may still rewrite, and a published version the whole
-- organization shares and nobody may rewrite. The template row is the identity
-- those versions belong to.
CREATE TABLE methodology_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT methodology_templates_organization_id_id_key UNIQUE (organization_id, id),
    CONSTRAINT methodology_templates_organization_id_fkey
        FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
    CONSTRAINT methodology_templates_organization_id_created_by_fkey
        FOREIGN KEY (organization_id, created_by)
        REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

-- The content is original and structured, so each version carries the source it
-- was derived from, that source's own version and the attribution required to
-- use it. A version is a draft until an administrator publishes it, and
-- publication is the only state change the schema accepts.
CREATE TABLE methodology_template_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    template_id UUID NOT NULL,
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    state TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'published')),
    name TEXT NOT NULL CHECK (btrim(name) <> ''),
    source_name TEXT NOT NULL CHECK (btrim(source_name) <> ''),
    source_version TEXT NOT NULL CHECK (btrim(source_version) <> ''),
    attribution TEXT NOT NULL CHECK (btrim(attribution) <> ''),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_by UUID,
    published_at TIMESTAMPTZ,
    CONSTRAINT methodology_template_versions_organization_id_id_key UNIQUE (organization_id, id),
    CONSTRAINT methodology_template_versions_template_id_version_number_key UNIQUE (template_id, version_number),
    -- An engagement checklist copies one exact published version, so the state
    -- is part of the key it references.
    CONSTRAINT methodology_template_versions_organization_id_id_state_key UNIQUE (organization_id, id, state),
    CONSTRAINT methodology_template_versions_publication_consistency CHECK (
        (state = 'draft' AND published_by IS NULL AND published_at IS NULL)
        OR (state = 'published' AND published_by IS NOT NULL AND published_at IS NOT NULL)
    ),
    CONSTRAINT methodology_template_versions_organization_id_template_id_fkey
        FOREIGN KEY (organization_id, template_id)
        REFERENCES methodology_templates (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT methodology_template_versions_organization_id_created_by_fkey
        FOREIGN KEY (organization_id, created_by)
        REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT,
    CONSTRAINT methodology_template_versions_organization_id_published_by_fkey
        FOREIGN KEY (organization_id, published_by)
        REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

-- An item states how to verify a control, not merely its name, so the objective
-- and the procedure are required. Its position is assigned from the order the
-- author submitted, never from client-supplied numbering.
CREATE TABLE methodology_template_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    version_id UUID NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 1),
    title TEXT NOT NULL CHECK (btrim(title) <> ''),
    objective TEXT NOT NULL CHECK (btrim(objective) <> ''),
    preconditions TEXT NOT NULL DEFAULT '',
    procedure TEXT NOT NULL CHECK (btrim(procedure) <> ''),
    expected_evidence TEXT NOT NULL DEFAULT '',
    reference TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    CONSTRAINT methodology_template_items_version_id_position_key UNIQUE (version_id, position),
    CONSTRAINT methodology_template_items_organization_id_version_id_fkey
        FOREIGN KEY (organization_id, version_id)
        REFERENCES methodology_template_versions (organization_id, id) ON DELETE RESTRICT
);

-- The engagement receives a copy, never a live reference: editing the library
-- afterwards cannot change what an engagement is being tested against.
CREATE TABLE engagement_checklists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    template_version_id UUID NOT NULL,
    -- Only a published version may be copied, and the foreign key below reads
    -- this constant as part of the referenced version's key.
    template_version_state TEXT NOT NULL DEFAULT 'published' CHECK (template_version_state = 'published'),
    version_number INTEGER NOT NULL CHECK (version_number >= 1),
    name TEXT NOT NULL CHECK (btrim(name) <> ''),
    source_name TEXT NOT NULL,
    source_version TEXT NOT NULL,
    attribution TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT engagement_checklists_organization_id_id_key UNIQUE (organization_id, id),
    CONSTRAINT engagement_checklists_organization_id_engagement_id_key UNIQUE (organization_id, engagement_id),
    CONSTRAINT engagement_checklists_organization_id_engagement_id_fkey
        FOREIGN KEY (organization_id, engagement_id)
        REFERENCES engagements (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT engagement_checklists_organization_id_template_version_id_fkey
        FOREIGN KEY (organization_id, template_version_id, template_version_state)
        REFERENCES methodology_template_versions (organization_id, id, state) ON DELETE RESTRICT
);

CREATE TABLE engagement_checklist_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    checklist_id UUID NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 1),
    title TEXT NOT NULL CHECK (btrim(title) <> ''),
    objective TEXT NOT NULL CHECK (btrim(objective) <> ''),
    preconditions TEXT NOT NULL DEFAULT '',
    procedure TEXT NOT NULL CHECK (btrim(procedure) <> ''),
    expected_evidence TEXT NOT NULL DEFAULT '',
    reference TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    CONSTRAINT engagement_checklist_items_checklist_id_position_key UNIQUE (checklist_id, position),
    CONSTRAINT engagement_checklist_items_organization_id_checklist_id_fkey
        FOREIGN KEY (organization_id, checklist_id)
        REFERENCES engagement_checklists (organization_id, id) ON DELETE RESTRICT
);

-- Publishing a version is the one change it accepts, and it never rewrites the
-- identity of the version being published. A correction to published content is
-- a new version, never a rewrite of the one an engagement may already have
-- copied.
-- +goose StatementBegin
CREATE FUNCTION prevent_published_methodology_rewrite() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.state = 'published' THEN
        RAISE EXCEPTION 'published methodology versions are immutable';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    IF (NEW.id, NEW.organization_id, NEW.template_id, NEW.version_number, NEW.created_by, NEW.created_at)
       IS DISTINCT FROM
       (OLD.id, OLD.organization_id, OLD.template_id, OLD.version_number, OLD.created_by, OLD.created_at) THEN
        RAISE EXCEPTION 'methodology version identity is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER methodology_template_versions_reject_rewrite
    BEFORE UPDATE OR DELETE ON methodology_template_versions
    FOR EACH ROW EXECUTE FUNCTION prevent_published_methodology_rewrite();

-- The content of a published version is immutable too, so its items can neither
-- be edited nor gain or lose an entry.
-- +goose StatementBegin
CREATE FUNCTION prevent_published_methodology_item_rewrite() RETURNS TRIGGER AS $$
DECLARE
    current_state TEXT;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        SELECT state INTO current_state FROM methodology_template_versions WHERE id = OLD.version_id;
        IF current_state = 'published' THEN
            RAISE EXCEPTION 'items of a published methodology version are immutable';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    SELECT state INTO current_state FROM methodology_template_versions WHERE id = NEW.version_id;
    IF current_state = 'published' THEN
        RAISE EXCEPTION 'items of a published methodology version are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER methodology_template_items_reject_rewrite
    BEFORE INSERT OR UPDATE OR DELETE ON methodology_template_items
    FOR EACH ROW EXECUTE FUNCTION prevent_published_methodology_item_rewrite();

-- +goose StatementBegin
CREATE FUNCTION prevent_engagement_checklist_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'engagement checklists are immutable copies';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER engagement_checklists_reject_mutation
    BEFORE UPDATE OR DELETE ON engagement_checklists
    FOR EACH ROW EXECUTE FUNCTION prevent_engagement_checklist_mutation();

CREATE TRIGGER engagement_checklist_items_reject_mutation
    BEFORE UPDATE OR DELETE ON engagement_checklist_items
    FOR EACH ROW EXECUTE FUNCTION prevent_engagement_checklist_mutation();

-- +goose Down
DROP TRIGGER engagement_checklist_items_reject_mutation ON engagement_checklist_items;
DROP TRIGGER engagement_checklists_reject_mutation ON engagement_checklists;
DROP FUNCTION prevent_engagement_checklist_mutation();
DROP TRIGGER methodology_template_items_reject_rewrite ON methodology_template_items;
DROP FUNCTION prevent_published_methodology_item_rewrite();
DROP TRIGGER methodology_template_versions_reject_rewrite ON methodology_template_versions;
DROP FUNCTION prevent_published_methodology_rewrite();
DROP TABLE engagement_checklist_items;
DROP TABLE engagement_checklists;
DROP TABLE methodology_template_items;
DROP TABLE methodology_template_versions;
DROP TABLE methodology_templates;
