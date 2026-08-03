package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Evidence struct {
	ID                string     `json:"id"`
	FindingID         string     `json:"findingId"`
	State             string     `json:"state"`
	StorageKey        string     `json:"storageKey"`
	Filename          string     `json:"filename"`
	DeclaredMediaType string     `json:"declaredMediaType"`
	DetectedMediaType string     `json:"detectedMediaType"`
	SHA256            string     `json:"sha256"`
	ByteSize          int64      `json:"byteSize"`
	CapturedAt        *time.Time `json:"capturedAt"`
	ReceivedAt        time.Time  `json:"receivedAt"`
	StoredAt          *time.Time `json:"storedAt"`
	CapturedBy        string     `json:"capturedBy"`
}

const evidenceColumns = `id, finding_id, state, storage_key, filename, declared_media_type, detected_media_type, encode(sha256, 'hex'), byte_size, captured_at, received_at, stored_at, captured_by`

// ReserveEvidence records the custody metadata of one capture before its bytes
// reach the object store, and returns the object key the database derived for
// it. The row is committed in state 'pending': PostgreSQL and the object store
// are separate systems and share no transaction, so a capture whose upload never
// completes stays visible as an unconfirmed record for reconciliation instead of
// disappearing or being reported as stored evidence. Ownership is the predicate
// of the insert itself, so a finding owned by another organization is
// indistinguishable from a missing one.
func ReserveEvidence(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, findingID string, evidence Evidence) (Evidence, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Evidence{}, fmt.Errorf("begin evidence transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	err = tx.QueryRow(ctx, `INSERT INTO evidence (organization_id, engagement_id, finding_id, filename, declared_media_type, detected_media_type, sha256, byte_size, captured_at, captured_by)
		SELECT $1, engagement_id, id, $3, $4, $5, decode($6, 'hex'), $7, $8, $9 FROM findings WHERE organization_id = $1 AND id = $2
		RETURNING `+evidenceColumns,
		session.OrganizationID, findingID, evidence.Filename, evidence.DeclaredMediaType, evidence.DetectedMediaType, evidence.SHA256, evidence.ByteSize, evidence.CapturedAt, session.UserID).Scan(scanEvidence(&evidence)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Evidence{}, ErrNotFound
	}
	if err != nil {
		return Evidence{}, fmt.Errorf("reserve evidence: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'evidence.capture.reserved', 'evidence', $3, 'success', gen_random_uuid(), jsonb_build_object('findingId', $4::uuid, 'sha256', $5::text, 'byteSize', $6::bigint, 'detectedMediaType', $7::text))`,
		session.OrganizationID, session.UserID, evidence.ID, evidence.FindingID, evidence.SHA256, evidence.ByteSize, evidence.DetectedMediaType); err != nil {
		return Evidence{}, fmt.Errorf("audit evidence reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Evidence{}, fmt.Errorf("commit evidence transaction: %w", err)
	}
	return evidence, nil
}

// ConfirmEvidence advances one reserved capture to 'stored' after the object
// store has accepted the exact bytes the digest describes. It is the only state
// change the schema allows, and it never rewrites the custody metadata; a replay
// or a row already confirmed changes nothing and reports ErrInvalidState.
func ConfirmEvidence(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, evidenceID string) (Evidence, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Evidence{}, fmt.Errorf("begin evidence confirmation transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var evidence Evidence
	err = tx.QueryRow(ctx, `UPDATE evidence SET state = 'stored', stored_at = now() WHERE organization_id = $1 AND id = $2 AND state = 'pending' RETURNING `+evidenceColumns,
		session.OrganizationID, evidenceID).Scan(scanEvidence(&evidence)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Evidence{}, ErrInvalidState
	}
	if err != nil {
		return Evidence{}, fmt.Errorf("confirm evidence: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'evidence.capture.stored', 'evidence', $3, 'success', gen_random_uuid(), jsonb_build_object('findingId', $4::uuid, 'sha256', $5::text, 'byteSize', $6::bigint, 'storageKey', $7::text))`,
		session.OrganizationID, session.UserID, evidence.ID, evidence.FindingID, evidence.SHA256, evidence.ByteSize, evidence.StorageKey); err != nil {
		return Evidence{}, fmt.Errorf("audit evidence confirmation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Evidence{}, fmt.Errorf("commit evidence confirmation transaction: %w", err)
	}
	return evidence, nil
}

// ListEvidence returns the whole chain of custody of one finding, including
// captures still pending, so an unconfirmed upload is never silently read as
// stored evidence.
func ListEvidence(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, findingID string) ([]Evidence, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM findings WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, findingID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("find finding: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := pool.Query(ctx, `SELECT `+evidenceColumns+` FROM evidence WHERE organization_id = $1 AND finding_id = $2 ORDER BY received_at, id`, session.OrganizationID, findingID)
	if err != nil {
		return nil, fmt.Errorf("list evidence: %w", err)
	}
	defer rows.Close()
	var items []Evidence
	for rows.Next() {
		var evidence Evidence
		if err := rows.Scan(scanEvidence(&evidence)...); err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		items = append(items, evidence)
	}
	return items, rows.Err()
}

func scanEvidence(evidence *Evidence) []any {
	return []any{&evidence.ID, &evidence.FindingID, &evidence.State, &evidence.StorageKey, &evidence.Filename, &evidence.DeclaredMediaType, &evidence.DetectedMediaType, &evidence.SHA256, &evidence.ByteSize, &evidence.CapturedAt, &evidence.ReceivedAt, &evidence.StoredAt, &evidence.CapturedBy}
}
