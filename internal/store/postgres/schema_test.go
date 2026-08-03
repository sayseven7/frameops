package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUsersRejectCaseInsensitiveDuplicateEmail(t *testing.T) {

	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for schema integration tests")
	}

	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to schema test database: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(ctx); err != nil {
			t.Errorf("close schema test database connection: %v", err)
		}
	})

	var organizationID string
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Synthetic Organization') RETURNING id`).Scan(&organizationID); err != nil {
		t.Fatalf("create synthetic organization: %v", err)
	}

	if _, err := connection.Exec(ctx, `INSERT INTO users (display_name, email) VALUES ('Synthetic Member', 'member@example.test')`); err != nil {
		t.Fatalf("create synthetic user: %v", err)
	}

	_, err = connection.Exec(ctx, `INSERT INTO users (display_name, email) VALUES ('Duplicate Member', 'MEMBER@example.test')`)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
		t.Fatalf("duplicate email error = %v, want PostgreSQL unique-violation 23505", err)
	}
}

func TestOrganizationMembershipsRejectUnsupportedRole(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for schema integration tests")
	}

	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to schema test database: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(ctx); err != nil {
			t.Errorf("close schema test database connection: %v", err)
		}
	})

	var organizationID, userID string
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Membership Organization') RETURNING id`).Scan(&organizationID); err != nil {
		t.Fatalf("create synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO users (display_name, email) VALUES ('Membership User', 'membership@example.test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create synthetic user: %v", err)
	}

	_, err = connection.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'observer')`, organizationID, userID)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
		t.Fatalf("unsupported membership role error = %v, want PostgreSQL check-violation 23514", err)
	}
}

func TestEngagementsRejectClientFromAnotherOrganization(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for schema integration tests")
	}

	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to schema test database: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(ctx); err != nil {
			t.Errorf("close schema test database connection: %v", err)
		}
	})

	var firstOrganizationID, secondOrganizationID, clientID string
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('First Organization') RETURNING id`).Scan(&firstOrganizationID); err != nil {
		t.Fatalf("create first synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Second Organization') RETURNING id`).Scan(&secondOrganizationID); err != nil {
		t.Fatalf("create second synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO clients (organization_id, name) VALUES ($1, 'Synthetic Client') RETURNING id`, firstOrganizationID).Scan(&clientID); err != nil {
		t.Fatalf("create synthetic client: %v", err)
	}

	_, err = connection.Exec(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'Cross-organization Engagement')`, secondOrganizationID, clientID)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("cross-organization engagement error = %v, want PostgreSQL foreign-key violation 23503", err)
	}
}

func TestAssetsRequireAnEngagementFromTheirOrganization(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for schema integration tests")
	}

	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to schema test database: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(ctx); err != nil {
			t.Errorf("close schema test database connection: %v", err)
		}
	})

	var firstOrganizationID, secondOrganizationID, clientID, engagementID string
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Asset Organization One') RETURNING id`).Scan(&firstOrganizationID); err != nil {
		t.Fatalf("create first synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Asset Organization Two') RETURNING id`).Scan(&secondOrganizationID); err != nil {
		t.Fatalf("create second synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO clients (organization_id, name) VALUES ($1, 'Asset Client') RETURNING id`, firstOrganizationID).Scan(&clientID); err != nil {
		t.Fatalf("create synthetic client: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'Asset Engagement') RETURNING id`, firstOrganizationID, clientID).Scan(&engagementID); err != nil {
		t.Fatalf("create synthetic engagement: %v", err)
	}
	if _, err := connection.Exec(ctx, `INSERT INTO assets (organization_id, engagement_id, name) VALUES ($1, $2, 'Valid Asset')`, firstOrganizationID, engagementID); err != nil {
		t.Fatalf("create same-organization asset: %v", err)
	}
	_, err = connection.Exec(ctx, `DELETE FROM engagements WHERE id = $1`, engagementID)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23001" {
		t.Fatalf("delete referenced engagement error = %v, want PostgreSQL restrict-violation 23001", err)
	}

	_, err = connection.Exec(ctx, `INSERT INTO assets (organization_id, engagement_id, name) VALUES ($1, $2, 'Cross-organization Asset')`, secondOrganizationID, engagementID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("cross-organization asset error = %v, want PostgreSQL foreign-key violation 23503", err)
	}
}

func TestFindingAssetsRejectAnAssetFromAnotherEngagement(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for schema integration tests")
	}

	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to schema test database: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(ctx); err != nil {
			t.Errorf("close schema test database connection: %v", err)
		}
	})

	var organizationID, userID, clientID, engagementID, siblingID, findingID, assetID, siblingAssetID string
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Link Organization') RETURNING id`).Scan(&organizationID); err != nil {
		t.Fatalf("create synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO users (display_name, email) VALUES ('Link Member', 'link-member@example.test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create synthetic user: %v", err)
	}
	if _, err := connection.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')`, organizationID, userID); err != nil {
		t.Fatalf("create synthetic membership: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO clients (organization_id, name) VALUES ($1, 'Link Client') RETURNING id`, organizationID).Scan(&clientID); err != nil {
		t.Fatalf("create synthetic client: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'Link Engagement') RETURNING id`, organizationID, clientID).Scan(&engagementID); err != nil {
		t.Fatalf("create synthetic engagement: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'Sibling Engagement') RETURNING id`, organizationID, clientID).Scan(&siblingID); err != nil {
		t.Fatalf("create sibling engagement: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO findings (organization_id, engagement_id, title, cvss_vector, cvss_score, created_by) VALUES ($1, $2, 'Link Finding', 'CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H', 9.8, $3) RETURNING id`, organizationID, engagementID, userID).Scan(&findingID); err != nil {
		t.Fatalf("create synthetic finding: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO assets (organization_id, engagement_id, name) VALUES ($1, $2, 'Link Asset') RETURNING id`, organizationID, engagementID).Scan(&assetID); err != nil {
		t.Fatalf("create synthetic asset: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO assets (organization_id, engagement_id, name) VALUES ($1, $2, 'Sibling Asset') RETURNING id`, organizationID, siblingID).Scan(&siblingAssetID); err != nil {
		t.Fatalf("create sibling asset: %v", err)
	}
	if _, err := connection.Exec(ctx, `INSERT INTO finding_assets (organization_id, engagement_id, finding_id, asset_id) VALUES ($1, $2, $3, $4)`, organizationID, engagementID, findingID, assetID); err != nil {
		t.Fatalf("link same-engagement asset: %v", err)
	}

	var databaseError *pgconn.PgError
	_, err = connection.Exec(ctx, `INSERT INTO finding_assets (organization_id, engagement_id, finding_id, asset_id) VALUES ($1, $2, $3, $4)`, organizationID, engagementID, findingID, siblingAssetID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("sibling-engagement link error = %v, want PostgreSQL foreign-key violation 23503", err)
	}

	_, err = connection.Exec(ctx, `INSERT INTO finding_assets (organization_id, engagement_id, finding_id, asset_id) VALUES ($1, $2, $3, $4)`, organizationID, siblingID, findingID, siblingAssetID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("relabelled-engagement link error = %v, want PostgreSQL foreign-key violation 23503", err)
	}
}

func TestFindingRetestsAreImmutableSequentialRoundsOwnedByOneOrganization(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for schema integration tests")
	}

	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to schema test database: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(ctx); err != nil {
			t.Errorf("close schema test database connection: %v", err)
		}
	})

	var organizationID, otherOrganizationID, userID, clientID, engagementID, siblingID, findingID, roundID string
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Retest Organization') RETURNING id`).Scan(&organizationID); err != nil {
		t.Fatalf("create synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Retest Outside Organization') RETURNING id`).Scan(&otherOrganizationID); err != nil {
		t.Fatalf("create outside organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO users (display_name, email) VALUES ('Retest Member', 'retest-member@example.test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create synthetic user: %v", err)
	}
	if _, err := connection.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')`, organizationID, userID); err != nil {
		t.Fatalf("create synthetic membership: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO clients (organization_id, name) VALUES ($1, 'Retest Client') RETURNING id`, organizationID).Scan(&clientID); err != nil {
		t.Fatalf("create synthetic client: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'Retest Engagement') RETURNING id`, organizationID, clientID).Scan(&engagementID); err != nil {
		t.Fatalf("create synthetic engagement: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'Retest Sibling Engagement') RETURNING id`, organizationID, clientID).Scan(&siblingID); err != nil {
		t.Fatalf("create sibling engagement: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO findings (organization_id, engagement_id, title, cvss_vector, cvss_score, validation_state, remediation_state, created_by) VALUES ($1, $2, 'Retest Finding', 'CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H', 9.8, 'confirmed', 'open', $3) RETURNING id`, organizationID, engagementID, userID).Scan(&findingID); err != nil {
		t.Fatalf("create synthetic finding: %v", err)
	}

	const insertRound = `INSERT INTO finding_retests (organization_id, engagement_id, finding_id, round_number, previous_state, result_state, executed_procedure, observed_result, justification, performed_by) VALUES ($1, $2, $3, $4, $5, $6, 'Replayed the login request', 'Payload still returned the database error', 'Fix not deployed yet', $7) RETURNING id`
	if err := connection.QueryRow(ctx, insertRound, organizationID, engagementID, findingID, 1, "open", "open", userID).Scan(&roundID); err != nil {
		t.Fatalf("record first retest round: %v", err)
	}

	var databaseError *pgconn.PgError
	_, err = connection.Exec(ctx, insertRound, organizationID, engagementID, findingID, 1, "open", "fixed", userID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
		t.Fatalf("replayed round error = %v, want PostgreSQL unique-violation 23505", err)
	}

	_, err = connection.Exec(ctx, insertRound, organizationID, engagementID, findingID, 3, "open", "fixed", userID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("skipped round error = %v, want PostgreSQL foreign-key violation 23503", err)
	}

	for _, transition := range [][2]string{{"fixed", "open"}, {"open", "risk_accepted"}, {"not_reproduced", "fixed"}} {
		_, err = connection.Exec(ctx, insertRound, organizationID, engagementID, findingID, 2, transition[0], transition[1], userID)
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatalf("unsupported %s to %s transition error = %v, want PostgreSQL check-violation 23514", transition[0], transition[1], err)
		}
	}

	_, err = connection.Exec(ctx, insertRound, organizationID, siblingID, findingID, 2, "open", "fixed", userID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("relabelled-engagement round error = %v, want PostgreSQL foreign-key violation 23503", err)
	}

	_, err = connection.Exec(ctx, insertRound, otherOrganizationID, engagementID, findingID, 2, "open", "fixed", userID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("cross-organization round error = %v, want PostgreSQL foreign-key violation 23503", err)
	}

	for _, statement := range []string{
		`UPDATE finding_retests SET result_state = 'fixed' WHERE id = $1`,
		`DELETE FROM finding_retests WHERE id = $1`,
	} {
		_, err = connection.Exec(ctx, statement, roundID)
		if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
			t.Fatalf("immutable retest mutation error = %v, want PostgreSQL raise-exception P0001", err)
		}
	}

	if _, err := connection.Exec(ctx, insertRound, organizationID, engagementID, findingID, 2, "open", "fixed", userID); err != nil {
		t.Fatalf("record second retest round: %v", err)
	}
}

func TestEvidenceKeepsImmutableCustodyOwnedByOneOrganization(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for schema integration tests")
	}

	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to schema test database: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(ctx); err != nil {
			t.Errorf("close schema test database connection: %v", err)
		}
	})

	var organizationID, otherOrganizationID, userID, clientID, engagementID, siblingID, findingID, evidenceID, storageKey string
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Evidence Schema Organization') RETURNING id`).Scan(&organizationID); err != nil {
		t.Fatalf("create synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Evidence Schema Outside Organization') RETURNING id`).Scan(&otherOrganizationID); err != nil {
		t.Fatalf("create outside organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO users (display_name, email) VALUES ('Evidence Member', 'evidence-member@example.test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create synthetic user: %v", err)
	}
	if _, err := connection.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')`, organizationID, userID); err != nil {
		t.Fatalf("create synthetic membership: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO clients (organization_id, name) VALUES ($1, 'Evidence Client') RETURNING id`, organizationID).Scan(&clientID); err != nil {
		t.Fatalf("create synthetic client: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'Evidence Engagement') RETURNING id`, organizationID, clientID).Scan(&engagementID); err != nil {
		t.Fatalf("create synthetic engagement: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'Evidence Sibling Engagement') RETURNING id`, organizationID, clientID).Scan(&siblingID); err != nil {
		t.Fatalf("create sibling engagement: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO findings (organization_id, engagement_id, title, cvss_vector, cvss_score, created_by) VALUES ($1, $2, 'Evidence Finding', 'CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H', 9.8, $3) RETURNING id`, organizationID, engagementID, userID).Scan(&findingID); err != nil {
		t.Fatalf("create synthetic finding: %v", err)
	}

	const captureEvidence = `INSERT INTO evidence (organization_id, engagement_id, finding_id, filename, declared_media_type, detected_media_type, sha256, byte_size, captured_by)
		VALUES ($1, $2, $3, 'capture.png', 'text/plain', 'image/png', decode($4, 'hex'), $5, $6) RETURNING id, storage_key`
	const digest = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	const otherDigest = "60303ae22b998861bce3b28f33eec1be758a213c86c93c076dbe9f558c11c752"
	if err := connection.QueryRow(ctx, captureEvidence, organizationID, engagementID, findingID, digest, 23, userID).Scan(&evidenceID, &storageKey); err != nil {
		t.Fatalf("reserve synthetic evidence: %v", err)
	}
	if want := "organizations/" + organizationID + "/engagements/" + engagementID + "/evidence/" + evidenceID; storageKey != want {
		t.Fatalf("derived storage key = %q, want %q", storageKey, want)
	}

	var databaseError *pgconn.PgError
	for name, statement := range map[string]string{
		"truncated digest": `INSERT INTO evidence (organization_id, engagement_id, finding_id, filename, declared_media_type, detected_media_type, sha256, byte_size, captured_by) VALUES ($1, $2, $3, 'short.png', '', 'image/png', decode('9f86d0', 'hex'), 23, $4)`,
		"empty capture":    `INSERT INTO evidence (organization_id, engagement_id, finding_id, filename, declared_media_type, detected_media_type, sha256, byte_size, captured_by) VALUES ($1, $2, $3, 'empty.png', '', 'image/png', decode('` + digest + `', 'hex'), 0, $4)`,
		"undetected type":  `INSERT INTO evidence (organization_id, engagement_id, finding_id, filename, declared_media_type, detected_media_type, sha256, byte_size, captured_by) VALUES ($1, $2, $3, 'unknown.png', '', '  ', decode('` + digest + `', 'hex'), 23, $4)`,
		"stored without instant": `INSERT INTO evidence (organization_id, engagement_id, finding_id, filename, declared_media_type, detected_media_type, sha256, byte_size, state, captured_by)
			VALUES ($1, $2, $3, 'unconfirmed.png', '', 'image/png', decode('` + digest + `', 'hex'), 23, 'stored', $4)`,
	} {
		_, err = connection.Exec(ctx, statement, organizationID, engagementID, findingID, userID)
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatalf("%s error = %v, want PostgreSQL check-violation 23514", name, err)
		}
	}

	_, err = connection.Exec(ctx, captureEvidence, organizationID, siblingID, findingID, digest, 23, userID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("relabelled-engagement capture error = %v, want PostgreSQL foreign-key violation 23503", err)
	}
	_, err = connection.Exec(ctx, captureEvidence, otherOrganizationID, engagementID, findingID, digest, 23, userID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("cross-organization capture error = %v, want PostgreSQL foreign-key violation 23503", err)
	}

	// Evidence never cascades with its finding.
	_, err = connection.Exec(ctx, `DELETE FROM findings WHERE id = $1`, findingID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23001" {
		t.Fatalf("delete finding with evidence error = %v, want PostgreSQL restrict-violation 23001", err)
	}

	for name, statement := range map[string]string{
		"rewritten digest":  `UPDATE evidence SET sha256 = decode('` + otherDigest + `', 'hex'), state = 'stored', stored_at = now() WHERE id = $1`,
		"rewritten name":    `UPDATE evidence SET filename = 'renamed.png', state = 'stored', stored_at = now() WHERE id = $1`,
		"rewritten size":    `UPDATE evidence SET byte_size = 1, state = 'stored', stored_at = now() WHERE id = $1`,
		"reserved again":    `UPDATE evidence SET state = 'pending' WHERE id = $1`,
		"discarded capture": `DELETE FROM evidence WHERE id = $1`,
	} {
		_, err = connection.Exec(ctx, statement, evidenceID)
		if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
			t.Fatalf("%s error = %v, want PostgreSQL raise-exception P0001", name, err)
		}
	}

	// Confirming the upload is the one state change the schema accepts.
	if _, err := connection.Exec(ctx, `UPDATE evidence SET state = 'stored', stored_at = now() WHERE id = $1`, evidenceID); err != nil {
		t.Fatalf("confirm stored evidence: %v", err)
	}
	_, err = connection.Exec(ctx, `UPDATE evidence SET state = 'stored', stored_at = now() WHERE id = $1`, evidenceID)
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
		t.Fatalf("reconfirmed evidence error = %v, want PostgreSQL raise-exception P0001", err)
	}
}

func TestMethodologyVersionsPublishOnceAndChecklistsCopyThemImmutably(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for schema integration tests")
	}

	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to schema test database: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(ctx); err != nil {
			t.Errorf("close schema test database connection: %v", err)
		}
	})

	var organizationID, otherOrganizationID, userID, clientID, engagementID, siblingID string
	var templateID, draftID, publishedID, checklistID, itemID string
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Methodology Schema Organization') RETURNING id`).Scan(&organizationID); err != nil {
		t.Fatalf("create synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Methodology Schema Outside Organization') RETURNING id`).Scan(&otherOrganizationID); err != nil {
		t.Fatalf("create outside organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO users (display_name, email) VALUES ('Methodology Member', 'methodology-schema@example.test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create synthetic user: %v", err)
	}
	if _, err := connection.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'admin')`, organizationID, userID); err != nil {
		t.Fatalf("create synthetic membership: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO clients (organization_id, name) VALUES ($1, 'Methodology Client') RETURNING id`, organizationID).Scan(&clientID); err != nil {
		t.Fatalf("create synthetic client: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'Methodology Engagement') RETURNING id`, organizationID, clientID).Scan(&engagementID); err != nil {
		t.Fatalf("create synthetic engagement: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'Methodology Sibling Engagement') RETURNING id`, organizationID, clientID).Scan(&siblingID); err != nil {
		t.Fatalf("create sibling engagement: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO methodology_templates (organization_id, created_by) VALUES ($1, $2) RETURNING id`, organizationID, userID).Scan(&templateID); err != nil {
		t.Fatalf("create synthetic template: %v", err)
	}

	const insertVersion = `INSERT INTO methodology_template_versions (organization_id, template_id, version_number, name, source_name, source_version, attribution, created_by)
		VALUES ($1, $2, $3, 'Web grey box', 'OWASP WSTG', '4.2', 'Original checklist structured after OWASP WSTG 4.2', $4) RETURNING id`
	if err := connection.QueryRow(ctx, insertVersion, organizationID, templateID, 1, userID).Scan(&publishedID); err != nil {
		t.Fatalf("create synthetic version: %v", err)
	}
	if err := connection.QueryRow(ctx, insertVersion, organizationID, templateID, 2, userID).Scan(&draftID); err != nil {
		t.Fatalf("create second synthetic version: %v", err)
	}

	var databaseError *pgconn.PgError
	_, err = connection.Exec(ctx, insertVersion, otherOrganizationID, templateID, 3, userID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("cross-organization version error = %v, want PostgreSQL foreign-key violation 23503", err)
	}
	_, err = connection.Exec(ctx, insertVersion, organizationID, templateID, 1, userID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
		t.Fatalf("duplicate version number error = %v, want PostgreSQL unique-violation 23505", err)
	}
	_, err = connection.Exec(ctx, `UPDATE methodology_template_versions SET state = 'published' WHERE id = $1`, publishedID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
		t.Fatalf("publication without a publisher error = %v, want PostgreSQL check-violation 23514", err)
	}

	const insertItem = `INSERT INTO methodology_template_items (organization_id, version_id, position, title, objective, procedure, reference)
		VALUES ($1, $2, $3, 'Session cookie attributes', 'Confirm the cookie cannot be read by scripts', 'Sign in and read Set-Cookie', 'WSTG-SESS-02') RETURNING id`
	if err := connection.QueryRow(ctx, insertItem, organizationID, publishedID, 1).Scan(&itemID); err != nil {
		t.Fatalf("create synthetic item: %v", err)
	}
	_, err = connection.Exec(ctx, insertItem, otherOrganizationID, publishedID, 2)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("cross-organization item error = %v, want PostgreSQL foreign-key violation 23503", err)
	}

	// A checklist copies a published version, never a draft.
	const insertChecklist = `INSERT INTO engagement_checklists (organization_id, engagement_id, template_version_id, version_number, name, source_name, source_version, attribution)
		VALUES ($1, $2, $3, 1, 'Web grey box', 'OWASP WSTG', '4.2', 'Original checklist structured after OWASP WSTG 4.2') RETURNING id`
	_, err = connection.Exec(ctx, insertChecklist, organizationID, engagementID, publishedID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("checklist of an unpublished version error = %v, want PostgreSQL foreign-key violation 23503", err)
	}

	if _, err := connection.Exec(ctx, `UPDATE methodology_template_versions SET state = 'published', published_by = $2, published_at = now() WHERE id = $1`, publishedID, userID); err != nil {
		t.Fatalf("publish synthetic version: %v", err)
	}
	for name, statement := range map[string]string{
		"republished":  `UPDATE methodology_template_versions SET published_at = now() WHERE id = $1`,
		"unpublished":  `UPDATE methodology_template_versions SET state = 'draft', published_by = NULL, published_at = NULL WHERE id = $1`,
		"rewritten":    `UPDATE methodology_template_versions SET name = 'Rewritten' WHERE id = $1`,
		"discarded":    `DELETE FROM methodology_template_versions WHERE id = $1`,
		"renumbered":   `UPDATE methodology_template_versions SET version_number = 9 WHERE id = $1`,
		"reattributed": `UPDATE methodology_template_versions SET attribution = 'Uncredited' WHERE id = $1`,
	} {
		_, err = connection.Exec(ctx, statement, publishedID)
		if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
			t.Fatalf("%s published version error = %v, want PostgreSQL raise-exception P0001", name, err)
		}
	}
	for name, statement := range map[string]string{
		"edited item":  `UPDATE methodology_template_items SET title = 'Rewritten' WHERE id = $1`,
		"removed item": `DELETE FROM methodology_template_items WHERE id = $1`,
	} {
		_, err = connection.Exec(ctx, statement, itemID)
		if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
			t.Fatalf("%s of a published version error = %v, want PostgreSQL raise-exception P0001", name, err)
		}
	}
	_, err = connection.Exec(ctx, insertItem, organizationID, publishedID, 2)
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
		t.Fatalf("added item of a published version error = %v, want PostgreSQL raise-exception P0001", err)
	}
	// The draft is still editable, which is what makes publication meaningful.
	if _, err := connection.Exec(ctx, `UPDATE methodology_template_versions SET name = 'Still a draft' WHERE id = $1`, draftID); err != nil {
		t.Fatalf("edit unpublished version: %v", err)
	}

	if err := connection.QueryRow(ctx, insertChecklist, organizationID, engagementID, publishedID).Scan(&checklistID); err != nil {
		t.Fatalf("snapshot synthetic checklist: %v", err)
	}
	_, err = connection.Exec(ctx, insertChecklist, organizationID, engagementID, publishedID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23505" {
		t.Fatalf("second checklist of one engagement error = %v, want PostgreSQL unique-violation 23505", err)
	}
	_, err = connection.Exec(ctx, insertChecklist, otherOrganizationID, siblingID, publishedID)
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("cross-organization checklist error = %v, want PostgreSQL foreign-key violation 23503", err)
	}

	const copyItem = `INSERT INTO engagement_checklist_items (organization_id, checklist_id, position, title, objective, procedure)
		VALUES ($1, $2, 1, 'Session cookie attributes', 'Confirm the cookie cannot be read by scripts', 'Sign in and read Set-Cookie') RETURNING id`
	var copiedID string
	if err := connection.QueryRow(ctx, copyItem, organizationID, checklistID).Scan(&copiedID); err != nil {
		t.Fatalf("copy synthetic checklist item: %v", err)
	}
	for name, statement := range map[string]string{
		"edited checklist":  `UPDATE engagement_checklists SET name = 'Rewritten' WHERE id = $1`,
		"deleted checklist": `DELETE FROM engagement_checklists WHERE id = $1`,
	} {
		_, err = connection.Exec(ctx, statement, checklistID)
		if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
			t.Fatalf("%s error = %v, want PostgreSQL raise-exception P0001", name, err)
		}
	}
	for name, statement := range map[string]string{
		"edited copy":  `UPDATE engagement_checklist_items SET title = 'Rewritten' WHERE id = $1`,
		"deleted copy": `DELETE FROM engagement_checklist_items WHERE id = $1`,
	} {
		_, err = connection.Exec(ctx, statement, copiedID)
		if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
			t.Fatalf("%s error = %v, want PostgreSQL raise-exception P0001", name, err)
		}
	}
}

func TestAuditEventsAreAppendOnlyAndActorsBelongToTheirOrganization(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for schema integration tests")
	}

	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to schema test database: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(ctx); err != nil {
			t.Errorf("close schema test database connection: %v", err)
		}
	})

	var firstOrganizationID, secondOrganizationID, memberID, eventID string
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Audit Organization One') RETURNING id`).Scan(&firstOrganizationID); err != nil {
		t.Fatalf("create first synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Audit Organization Two') RETURNING id`).Scan(&secondOrganizationID); err != nil {
		t.Fatalf("create second synthetic organization: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO users (display_name, email) VALUES ('Audit Member', 'audit-member@example.test') RETURNING id`).Scan(&memberID); err != nil {
		t.Fatalf("create synthetic member: %v", err)
	}
	if _, err := connection.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')`, firstOrganizationID, memberID); err != nil {
		t.Fatalf("create synthetic membership: %v", err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'asset.created', 'asset', gen_random_uuid(), 'success', gen_random_uuid(), '{"source":"schema-test"}') RETURNING id`, firstOrganizationID, memberID).Scan(&eventID); err != nil {
		t.Fatalf("insert valid audit event: %v", err)
	}

	_, err = connection.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'asset.created', 'asset', gen_random_uuid(), 'success', gen_random_uuid(), '{}')`, secondOrganizationID, memberID)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatalf("cross-organization audit actor error = %v, want PostgreSQL foreign-key violation 23503", err)
	}

	for _, statement := range []string{
		`UPDATE audit_events SET outcome = 'failed' WHERE id = $1`,
		`DELETE FROM audit_events WHERE id = $1`,
	} {
		_, err = connection.Exec(ctx, statement, eventID)
		if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
			t.Fatalf("append-only audit mutation error = %v, want PostgreSQL raise-exception P0001", err)
		}
	}

	if _, err := connection.Exec(ctx, `INSERT INTO audit_events (organization_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, 'asset.read', 'asset', gen_random_uuid(), 'success', gen_random_uuid(), '{}')`, firstOrganizationID); err != nil {
		t.Fatalf("insert audit event after rejected mutations: %v", err)
	}
}
