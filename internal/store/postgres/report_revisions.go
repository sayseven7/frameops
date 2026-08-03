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
