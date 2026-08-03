package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ReportRevision struct {
	ID           string     `json:"id"`
	EngagementID string     `json:"engagementId"`
	State        string     `json:"state"`
	StorageKey   string     `json:"storageKey"`
	Filename     string     `json:"filename"`
	SHA256       string     `json:"sha256"`
	ByteSize     int64      `json:"byteSize"`
	ReceivedAt   time.Time  `json:"receivedAt"`
	StoredAt     *time.Time `json:"storedAt"`
	ApprovedAt   *time.Time `json:"approvedAt"`
}

const reportRevisionColumns = `id, engagement_id, state, storage_key, filename, encode(sha256, 'hex'), byte_size, received_at, stored_at, approved_at`

type reportRevisionPool interface {
	Begin(context.Context) (pgx.Tx, error)
}

func ReserveReportRevision(ctx context.Context, pool reportRevisionPool, session Session, engagementID, filename, digest string, byteSize int64) (ReportRevision, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ReportRevision{}, fmt.Errorf("begin report reservation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var revision ReportRevision
	err = tx.QueryRow(ctx, `INSERT INTO report_revisions (organization_id, engagement_id, filename, sha256, byte_size, imported_by)
		SELECT $1, id, $3, decode($4, 'hex'), $5, $6 FROM engagements WHERE organization_id = $1 AND id = $2
		RETURNING `+reportRevisionColumns, session.OrganizationID, engagementID, filename, digest, byteSize, session.UserID).Scan(scanReportRevision(&revision)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReportRevision{}, ErrNotFound
	}
	if err != nil {
		return ReportRevision{}, fmt.Errorf("reserve report revision: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'report.revision.reserved', 'report_revision', $3, 'success', gen_random_uuid(), jsonb_build_object('engagementId', $4::uuid, 'sha256', $5::text, 'byteSize', $6::bigint))`, session.OrganizationID, session.UserID, revision.ID, revision.EngagementID, revision.SHA256, revision.ByteSize); err != nil {
		return ReportRevision{}, fmt.Errorf("audit report reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ReportRevision{}, fmt.Errorf("commit report reservation: %w", err)
	}
	return revision, nil
}

func ConfirmReportRevision(ctx context.Context, pool reportRevisionPool, session Session, revisionID string) (ReportRevision, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ReportRevision{}, fmt.Errorf("begin report confirmation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var revision ReportRevision
	err = tx.QueryRow(ctx, `UPDATE report_revisions SET state = 'stored', stored_at = now() WHERE organization_id = $1 AND id = $2 AND state = 'pending' RETURNING `+reportRevisionColumns, session.OrganizationID, revisionID).Scan(scanReportRevision(&revision)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReportRevision{}, ErrInvalidState
	}
	if err != nil {
		return ReportRevision{}, fmt.Errorf("confirm report revision: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'report.revision.stored', 'report_revision', $3, 'success', gen_random_uuid(), jsonb_build_object('engagementId', $4::uuid, 'sha256', $5::text, 'byteSize', $6::bigint))`, session.OrganizationID, session.UserID, revision.ID, revision.EngagementID, revision.SHA256, revision.ByteSize); err != nil {
		return ReportRevision{}, fmt.Errorf("audit report confirmation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ReportRevision{}, fmt.Errorf("commit report confirmation: %w", err)
	}
	return revision, nil
}

func ListReportRevisions(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, engagementID string) ([]ReportRevision, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM engagements WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, engagementID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("find report engagement: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := pool.Query(ctx, `SELECT `+reportRevisionColumns+` FROM report_revisions WHERE organization_id = $1 AND engagement_id = $2 ORDER BY received_at, id`, session.OrganizationID, engagementID)
	if err != nil {
		return nil, fmt.Errorf("list report revisions: %w", err)
	}
	defer rows.Close()
	var revisions []ReportRevision
	for rows.Next() {
		var revision ReportRevision
		if err := rows.Scan(scanReportRevision(&revision)...); err != nil {
			return nil, fmt.Errorf("scan report revision: %w", err)
		}
		revisions = append(revisions, revision)
	}
	return revisions, rows.Err()
}

func ApproveReportRevision(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, revisionID string) (ReportRevision, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ReportRevision{}, fmt.Errorf("begin report approval: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var revision ReportRevision
	err = tx.QueryRow(ctx, `UPDATE report_revisions SET approved_at = now(), approved_by = $3 WHERE organization_id = $1 AND id = $2 AND state = 'stored' AND approved_at IS NULL RETURNING `+reportRevisionColumns, session.OrganizationID, revisionID, session.UserID).Scan(scanReportRevision(&revision)...)
	if errors.Is(err, pgx.ErrNoRows) {
		var found bool
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM report_revisions WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, revisionID).Scan(&found)
		if err != nil {
			return ReportRevision{}, fmt.Errorf("find report revision: %w", err)
		}
		if !found {
			return ReportRevision{}, ErrNotFound
		}
		return ReportRevision{}, ErrInvalidState
	}
	if isReportApprovalUniqueViolation(err) {
		return ReportRevision{}, ErrInvalidState
	}
	if err != nil {
		return ReportRevision{}, fmt.Errorf("approve report revision: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'report.revision.approved', 'report_revision', $3, 'success', gen_random_uuid(), jsonb_build_object('engagementId', $4::uuid, 'sha256', $5::text, 'byteSize', $6::bigint))`, session.OrganizationID, session.UserID, revision.ID, revision.EngagementID, revision.SHA256, revision.ByteSize); err != nil {
		return ReportRevision{}, fmt.Errorf("audit report approval: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ReportRevision{}, fmt.Errorf("commit report approval: %w", err)
	}
	return revision, nil
}

func isReportApprovalUniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505" && databaseError.ConstraintName == "report_revisions_one_approved_per_engagement_idx"
}

func scanReportRevision(revision *ReportRevision) []any {
	return []any{&revision.ID, &revision.EngagementID, &revision.State, &revision.StorageKey, &revision.Filename, &revision.SHA256, &revision.ByteSize, &revision.ReceivedAt, &revision.StoredAt, &revision.ApprovedAt}
}
