-- +goose Up
-- A retest round is an immutable event, and the finding's remediation_state is
-- the current state derived from this history rather than an independently
-- edited narrative. Every round therefore records the state it moved away from
-- and the state it produced, and only the transitions this stage supports are
-- accepted.
CREATE TABLE finding_retests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    engagement_id UUID NOT NULL,
    finding_id UUID NOT NULL,
    round_number INTEGER NOT NULL CHECK (round_number >= 1),
    -- Round 1 has no predecessor, and a NULL leaves the chain foreign key
    -- unchecked, so every later round must name the round immediately before it
    -- on the same finding and no round number can be skipped.
    previous_round_number INTEGER GENERATED ALWAYS AS (NULLIF(round_number - 1, 0)) STORED,
    previous_state TEXT NOT NULL,
    result_state TEXT NOT NULL,
    executed_procedure TEXT NOT NULL CHECK (btrim(executed_procedure) <> ''),
    observed_result TEXT NOT NULL CHECK (btrim(observed_result) <> ''),
    justification TEXT NOT NULL CHECK (btrim(justification) <> ''),
    performed_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT finding_retests_finding_id_round_number_key UNIQUE (finding_id, round_number),
    -- 'not_reproduced' stays distinct from 'fixed': an unreproduced finding is
    -- not proof that the correction was implemented. Accepting a risk is a
    -- separate decision and is not a retest result.
    CONSTRAINT finding_retests_supported_transition CHECK (
        previous_state = 'open' AND result_state IN ('open', 'fixed', 'not_reproduced')
    ),
    CONSTRAINT finding_retests_organization_id_engagement_id_finding_id_fkey
        FOREIGN KEY (organization_id, engagement_id, finding_id)
        REFERENCES findings (organization_id, engagement_id, id) ON DELETE RESTRICT,
    CONSTRAINT finding_retests_finding_id_previous_round_number_fkey
        FOREIGN KEY (finding_id, previous_round_number)
        REFERENCES finding_retests (finding_id, round_number) ON DELETE RESTRICT,
    CONSTRAINT finding_retests_organization_id_performed_by_fkey
        FOREIGN KEY (organization_id, performed_by)
        REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

-- A correction to a recorded round is a new round, never a rewrite of history.
-- +goose StatementBegin
CREATE FUNCTION prevent_finding_retest_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'finding retest rounds are immutable';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER finding_retests_reject_mutation
    BEFORE UPDATE OR DELETE ON finding_retests
    FOR EACH ROW EXECUTE FUNCTION prevent_finding_retest_mutation();

-- +goose Down
DROP TRIGGER finding_retests_reject_mutation ON finding_retests;
DROP FUNCTION prevent_finding_retest_mutation();
DROP TABLE finding_retests;
