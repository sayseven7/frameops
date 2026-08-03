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
