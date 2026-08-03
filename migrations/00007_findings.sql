-- +goose Up
CREATE TABLE findings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    title TEXT NOT NULL CHECK (btrim(title) <> ''),
    description TEXT NOT NULL DEFAULT '',
    impact TEXT NOT NULL DEFAULT '',
    remediation TEXT NOT NULL DEFAULT '',
    reproduction TEXT NOT NULL DEFAULT '',
    cvss_version TEXT NOT NULL DEFAULT '3.1' CHECK (cvss_version = '3.1'),
    cvss_vector TEXT NOT NULL CHECK (btrim(cvss_vector) <> ''),
    cvss_score NUMERIC(3,1) NOT NULL CHECK (cvss_score >= 0 AND cvss_score <= 10),
    validation_state TEXT NOT NULL DEFAULT 'new' CHECK (validation_state IN ('new', 'needs_review', 'confirmed', 'false_positive')),
    remediation_state TEXT CHECK (remediation_state IN ('open', 'fixed', 'risk_accepted', 'not_reproduced')),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT findings_organization_id_id_key UNIQUE (organization_id, id),
    CONSTRAINT findings_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE RESTRICT,
    CONSTRAINT findings_organization_id_engagement_id_fkey FOREIGN KEY (organization_id, engagement_id) REFERENCES engagements (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT findings_organization_id_created_by_fkey FOREIGN KEY (organization_id, created_by) REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT,
    CONSTRAINT findings_state_consistency CHECK (
        (validation_state IN ('new', 'needs_review', 'false_positive') AND remediation_state IS NULL)
        OR (validation_state = 'confirmed' AND remediation_state IS NOT NULL)
    )
);

-- +goose Down
DROP TABLE findings;
