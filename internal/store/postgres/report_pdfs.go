package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ReportPDF is one delivered PDF and the provenance of its conversion:
// SourceSHA256 is the digest of the approved DOCX revision it was converted
// from, and Converter identifies the converter that read those exact bytes.
type ReportPDF struct {
	ID           string     `json:"id"`
	EngagementID string     `json:"engagementId"`
	RevisionID   string     `json:"revisionId"`
	State        string     `json:"state"`
	StorageKey   string     `json:"storageKey"`
	SourceSHA256 string     `json:"sourceSha256"`
	Converter    string     `json:"converter"`
	SHA256       string     `json:"sha256"`
	ByteSize     int64      `json:"byteSize"`
	DerivedAt    time.Time  `json:"derivedAt"`
	StoredAt     *time.Time `json:"storedAt"`
}

const reportPDFColumns = `id, engagement_id, revision_id, state, storage_key, encode(source_sha256, 'hex'), converter, encode(sha256, 'hex'), byte_size, derived_at, stored_at`

// ApprovedReportRevision reads the one revision a PDF may be derived from. A
// revision owned by another organization is indistinguishable from a missing
// one, and a revision that exists but was never approved is reported as an
// invalid state rather than converted.
func ApprovedReportRevision(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, session Session, revisionID string) (ReportRevision, error) {
	var revision ReportRevision
	err := pool.QueryRow(ctx, `SELECT `+reportRevisionColumns+` FROM report_revisions WHERE organization_id = $1 AND id = $2`,
		session.OrganizationID, revisionID).Scan(scanReportRevision(&revision)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReportRevision{}, ErrNotFound
	}
	if err != nil {
		return ReportRevision{}, fmt.Errorf("find approved report revision: %w", err)
	}
	if revision.State != "stored" || revision.ApprovedAt == nil {
		return ReportRevision{}, ErrInvalidState
	}
	return revision, nil
}

// ReserveReportPDF records the provenance of one finished conversion before its
// bytes reach the object store. The insert takes the source digest from the
// revision row itself, so the recorded provenance cannot describe any bytes
// other than the approved ones, and it only accepts a revision that is still
// approved and has no delivered PDF yet.
func ReserveReportPDF(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, revisionID, converter, digest string, byteSize int64) (ReportPDF, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ReportPDF{}, fmt.Errorf("begin report pdf reservation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var pdf ReportPDF
	err = tx.QueryRow(ctx, `INSERT INTO report_pdfs (organization_id, engagement_id, revision_id, source_sha256, converter, sha256, byte_size, derived_by)
		SELECT $1, revision.engagement_id, revision.id, revision.sha256, $3, decode($4, 'hex'), $5, $6
		FROM report_revisions revision
		WHERE revision.organization_id = $1 AND revision.id = $2 AND revision.approved_at IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM report_pdfs delivered WHERE delivered.organization_id = revision.organization_id AND delivered.revision_id = revision.id AND delivered.state = 'stored')
		RETURNING `+reportPDFColumns, session.OrganizationID, revisionID, converter, digest, byteSize, session.UserID).Scan(scanReportPDF(&pdf)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReportPDF{}, ErrInvalidState
	}
	if err != nil {
		return ReportPDF{}, fmt.Errorf("reserve report pdf: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'report.pdf.derived', 'report_pdf', $3, 'success', gen_random_uuid(), jsonb_build_object('revisionId', $4::uuid, 'sourceSha256', $5::text, 'converter', $6::text, 'sha256', $7::text, 'byteSize', $8::bigint))`,
		session.OrganizationID, session.UserID, pdf.ID, pdf.RevisionID, pdf.SourceSHA256, pdf.Converter, pdf.SHA256, pdf.ByteSize); err != nil {
		return ReportPDF{}, fmt.Errorf("audit report pdf reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ReportPDF{}, fmt.Errorf("commit report pdf reservation: %w", err)
	}
	return pdf, nil
}

// ConfirmReportPDF advances one reserved conversion to 'stored' after the object
// store accepted exactly the bytes its digest describes. Two conversions of the
// same revision that raced past the reservation predicate meet the unique index
// here, and the loser is reported as an invalid state instead of a second
// delivered PDF.
func ConfirmReportPDF(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, pdfID string) (ReportPDF, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ReportPDF{}, fmt.Errorf("begin report pdf confirmation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var pdf ReportPDF
	err = tx.QueryRow(ctx, `UPDATE report_pdfs SET state = 'stored', stored_at = now() WHERE organization_id = $1 AND id = $2 AND state = 'pending' RETURNING `+reportPDFColumns,
		session.OrganizationID, pdfID).Scan(scanReportPDF(&pdf)...)
	if errors.Is(err, pgx.ErrNoRows) || isReportPDFUniqueViolation(err) {
		return ReportPDF{}, ErrInvalidState
	}
	if err != nil {
		return ReportPDF{}, fmt.Errorf("confirm report pdf: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'report.pdf.stored', 'report_pdf', $3, 'success', gen_random_uuid(), jsonb_build_object('revisionId', $4::uuid, 'sourceSha256', $5::text, 'converter', $6::text, 'sha256', $7::text, 'byteSize', $8::bigint, 'storageKey', $9::text))`,
		session.OrganizationID, session.UserID, pdf.ID, pdf.RevisionID, pdf.SourceSHA256, pdf.Converter, pdf.SHA256, pdf.ByteSize, pdf.StorageKey); err != nil {
		return ReportPDF{}, fmt.Errorf("audit report pdf confirmation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ReportPDF{}, fmt.Errorf("commit report pdf confirmation: %w", err)
	}
	return pdf, nil
}

func isReportPDFUniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505" && databaseError.ConstraintName == "report_pdfs_one_stored_per_revision_idx"
}

func scanReportPDF(pdf *ReportPDF) []any {
	return []any{&pdf.ID, &pdf.EngagementID, &pdf.RevisionID, &pdf.State, &pdf.StorageKey, &pdf.SourceSHA256, &pdf.Converter, &pdf.SHA256, &pdf.ByteSize, &pdf.DerivedAt, &pdf.StoredAt}
}
