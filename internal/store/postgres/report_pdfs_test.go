package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestReportPDFReservationUniqueViolation(t *testing.T) {
	for name, err := range map[string]error{
		"expected reservation index": &pgconn.PgError{Code: "23505", ConstraintName: "report_pdfs_one_effective_per_revision_key"},
		"stored-only index":          &pgconn.PgError{Code: "23505", ConstraintName: "report_pdfs_one_stored_per_revision_idx"},
		"other unique index":         &pgconn.PgError{Code: "23505", ConstraintName: "report_pdfs_storage_key_key"},
		"wrapped expected index":     fmt.Errorf("reserve: %w", &pgconn.PgError{Code: "23505", ConstraintName: "report_pdfs_one_effective_per_revision_key"}),
	} {
		t.Run(name, func(t *testing.T) {
			want := name == "expected reservation index" || name == "wrapped expected index"
			if got := isReportPDFUniqueViolation(err); got != want {
				t.Fatalf("isReportPDFUniqueViolation(%v) = %t, want %t", err, got, want)
			}
		})
	}
}

func TestReserveReportPDFAllowsOneConcurrentPendingReservation(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	setup, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to PostgreSQL integration database: %v", err)
	}
	t.Cleanup(func() { _ = setup.Close(ctx) })

	var organizationID, userID, clientID, engagementID, revisionID string
	if err := setup.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('PDF Reservation Organization') RETURNING id`).Scan(&organizationID); err != nil {
		t.Fatal(err)
	}
	email := fmt.Sprintf("pdf-reservation-%d@example.test", time.Now().UnixNano())
	if err := setup.QueryRow(ctx, `INSERT INTO users (display_name, email) VALUES ('PDF Reservation User', $1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')`, organizationID, userID); err != nil {
		t.Fatal(err)
	}
	if err := setup.QueryRow(ctx, `INSERT INTO clients (organization_id, name) VALUES ($1, 'PDF Reservation Client') RETURNING id`, organizationID).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	if err := setup.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'PDF Reservation Engagement') RETURNING id`, organizationID, clientID).Scan(&engagementID); err != nil {
		t.Fatal(err)
	}
	const digest = "1111111111111111111111111111111111111111111111111111111111111111"
	if err := setup.QueryRow(ctx, `INSERT INTO report_revisions (organization_id, engagement_id, state, filename, sha256, byte_size, stored_at, imported_by, approved_at, approved_by) VALUES ($1, $2, 'stored', 'approved.docx', decode($3, 'hex'), 1, now(), $4, now(), $4) RETURNING id`, organizationID, engagementID, digest, userID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	const reservationBarrierKey int64 = 2_043_314_290
	barrier := fmt.Sprintf(`CREATE FUNCTION test_report_pdf_reservation_barrier() RETURNS trigger AS $$ BEGIN PERFORM pg_advisory_xact_lock(%d); RETURN NEW; END; $$ LANGUAGE plpgsql; CREATE TRIGGER test_report_pdf_reservation_barrier BEFORE INSERT ON report_pdfs FOR EACH ROW EXECUTE FUNCTION test_report_pdf_reservation_barrier()`, reservationBarrierKey)
	if _, err := setup.Exec(ctx, barrier); err != nil {
		t.Fatalf("install reservation barrier: %v", err)
	}
	t.Cleanup(func() {
		_, _ = setup.Exec(context.Background(), `DROP TRIGGER IF EXISTS test_report_pdf_reservation_barrier ON report_pdfs; DROP FUNCTION IF EXISTS test_report_pdf_reservation_barrier()`)
	})
	if _, err := setup.Exec(ctx, `SELECT pg_advisory_lock($1)`, reservationBarrierKey); err != nil {
		t.Fatalf("lock reservation barrier: %v", err)
	}
	t.Cleanup(func() {
		_, _ = setup.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, reservationBarrierKey)
	})

	connections := make([]*pgx.Conn, 2)
	for index := range connections {
		connection, err := pgx.Connect(ctx, databaseURL)
		if err != nil {
			t.Fatalf("connect reservation worker %d: %v", index, err)
		}
		if err := connection.Ping(ctx); err != nil {
			_ = connection.Close(ctx)
			t.Fatalf("ping reservation worker %d: %v", index, err)
		}
		connections[index] = connection
	}
	t.Cleanup(func() {
		for _, connection := range connections {
			_ = connection.Close(ctx)
		}
	})

	ready := make(chan struct{}, len(connections))
	start := make(chan struct{})
	type reservationResult struct {
		pdf ReportPDF
		err error
	}
	results := make(chan reservationResult, len(connections))
	var wait sync.WaitGroup
	for _, connection := range connections {
		wait.Add(1)
		go func(connection *pgx.Conn) {
			defer wait.Done()
			ready <- struct{}{}
			<-start
			pdf, err := ReserveReportPDF(ctx, connection, Session{OrganizationID: organizationID, UserID: userID}, revisionID, "LibreOffice test", digest, 1)
			results <- reservationResult{pdf: pdf, err: err}
		}(connection)
	}
	for range connections {
		<-ready
	}
	close(start)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := setup.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE pid = ANY($1) AND wait_event_type = 'Lock' AND wait_event = 'advisory'`, []int32{int32(connections[0].PgConn().PID()), int32(connections[1].PgConn().PID())}).Scan(&waiting); err != nil {
			t.Fatalf("observe reservation workers: %v", err)
		}
		if waiting == len(connections) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reservation workers did not both reach the database barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := setup.Exec(ctx, `SELECT pg_advisory_unlock($1)`, reservationBarrierKey); err != nil {
		t.Fatalf("unlock reservation barrier: %v", err)
	}
	wait.Wait()
	close(results)

	successes := 0
	invalidStates := 0
	for result := range results {
		if result.err == nil {
			if result.pdf.State != "pending" {
				t.Fatalf("reserved report PDF state = %q, want pending", result.pdf.State)
			}
			successes++
			continue
		}
		if result.err != ErrInvalidState {
			t.Fatalf("competing reservation error = %v, want ErrInvalidState", result.err)
		}
		invalidStates++
	}
	if successes != 1 {
		t.Fatalf("successful reservations = %d, want 1", successes)
	}
	if invalidStates != 1 {
		t.Fatalf("invalid reservation states = %d, want 1", invalidStates)
	}

	var pendingReservations int
	if err := setup.QueryRow(ctx, `SELECT count(*) FROM report_pdfs WHERE organization_id = $1 AND revision_id = $2 AND state = 'pending'`, organizationID, revisionID).Scan(&pendingReservations); err != nil {
		t.Fatalf("count pending reservations: %v", err)
	}
	if pendingReservations != 1 {
		t.Fatalf("pending reservations = %d, want 1", pendingReservations)
	}
}

func TestFailReportPDFReleasesOnlyTheTenantsPendingReservation(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close(ctx) })

	var organizationID, otherOrganizationID, userID, clientID, engagementID, revisionID string
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('PDF Recovery Organization') RETURNING id`).Scan(&organizationID); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('PDF Recovery Outside Organization') RETURNING id`).Scan(&otherOrganizationID); err != nil {
		t.Fatal(err)
	}
	email := fmt.Sprintf("pdf-recovery-%d@example.test", time.Now().UnixNano())
	if err := connection.QueryRow(ctx, `INSERT INTO users (display_name, email) VALUES ('PDF Recovery User', $1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')`, organizationID, userID); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO clients (organization_id, name) VALUES ($1, 'PDF Recovery Client') RETURNING id`, organizationID).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'PDF Recovery Engagement') RETURNING id`, organizationID, clientID).Scan(&engagementID); err != nil {
		t.Fatal(err)
	}
	const sourceDigest = "2222222222222222222222222222222222222222222222222222222222222222"
	if err := connection.QueryRow(ctx, `INSERT INTO report_revisions (organization_id, engagement_id, state, filename, sha256, byte_size, stored_at, imported_by, approved_at, approved_by) VALUES ($1, $2, 'stored', 'approved.docx', decode($3, 'hex'), 1, now(), $4, now(), $4) RETURNING id`, organizationID, engagementID, sourceDigest, userID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	session := Session{OrganizationID: organizationID, UserID: userID}
	pdf, err := ReserveReportPDF(ctx, connection, session, revisionID, "LibreOffice test", sourceDigest, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FailReportPDF(ctx, connection, Session{OrganizationID: otherOrganizationID, UserID: userID}, pdf.ID); err != ErrInvalidState {
		t.Fatalf("cross-organization failure error = %v, want ErrInvalidState", err)
	}
	failed, err := FailReportPDF(ctx, connection, session, pdf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != "failed" || failed.FailedAt == nil {
		t.Fatalf("failed reservation = %+v, want failed state with timestamp", failed)
	}
	if _, err := ReserveReportPDF(ctx, connection, session, revisionID, "LibreOffice retry", sourceDigest, 1); err != nil {
		t.Fatalf("reserve after failure: %v", err)
	}

	var failedCount, effectiveCount int
	if err := connection.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state = 'failed'), count(*) FILTER (WHERE state IN ('pending', 'stored')) FROM report_pdfs WHERE organization_id = $1 AND revision_id = $2`, organizationID, revisionID).Scan(&failedCount, &effectiveCount); err != nil {
		t.Fatal(err)
	}
	if failedCount != 1 || effectiveCount != 1 {
		t.Fatalf("reservations = %d failed and %d effective, want 1 and 1", failedCount, effectiveCount)
	}
	if _, err := connection.Exec(ctx, `UPDATE report_pdfs SET converter = 'rewritten' WHERE organization_id = $1 AND id = $2`, organizationID, pdf.ID); err == nil {
		t.Fatal("failed reservation provenance was mutable")
	}

	const staleSourceDigest = "6666666666666666666666666666666666666666666666666666666666666666"
	var staleEngagementID, staleRevisionID, stalePDFID string
	if err := connection.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) VALUES ($1, $2, 'PDF Recovery Stale Engagement') RETURNING id`, organizationID, clientID).Scan(&staleEngagementID); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO report_revisions (organization_id, engagement_id, state, filename, sha256, byte_size, stored_at, imported_by, approved_at, approved_by) VALUES ($1, $2, 'stored', 'stale.docx', decode($3, 'hex'), 1, now(), $4, now(), $4) RETURNING id`, organizationID, staleEngagementID, staleSourceDigest, userID).Scan(&staleRevisionID); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRow(ctx, `INSERT INTO report_pdfs (organization_id, engagement_id, revision_id, source_sha256, converter, sha256, byte_size, derived_at, derived_by) VALUES ($1, $2, $3, decode($4, 'hex'), 'abandoned', decode($4, 'hex'), 1, now() - interval '6 minutes', $5) RETURNING id`, organizationID, staleEngagementID, staleRevisionID, staleSourceDigest, userID).Scan(&stalePDFID); err != nil {
		t.Fatal(err)
	}
	if _, err := ReserveReportPDF(ctx, connection, session, staleRevisionID, "LibreOffice recovery", staleSourceDigest, 1); err != nil {
		t.Fatalf("reserve after stale pending lease: %v", err)
	}
	var staleState string
	if err := connection.QueryRow(ctx, `SELECT state FROM report_pdfs WHERE organization_id = $1 AND id = $2`, organizationID, stalePDFID).Scan(&staleState); err != nil {
		t.Fatal(err)
	}
	if staleState != "failed" {
		t.Fatalf("stale reservation state = %q, want failed", staleState)
	}
}
