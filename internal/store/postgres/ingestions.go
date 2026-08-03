package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Ingestion is the immutable record of one tool artifact imported into one
// engagement: what produced it, which bytes were read, and what the import did
// to the asset inventory.
type Ingestion struct {
	ID            string           `json:"id"`
	EngagementID  string           `json:"engagementId"`
	Tool          string           `json:"tool"`
	FormatVersion string           `json:"formatVersion"`
	Filename      string           `json:"filename"`
	SHA256        string           `json:"sha256"`
	ByteSize      int64            `json:"byteSize"`
	Summary       IngestionSummary `json:"summary"`
	ReceivedAt    time.Time        `json:"receivedAt"`
	ImportedBy    string           `json:"importedBy"`
}

// IngestionSummary reports what happened to every item the artifact carried.
// The database checks that the four outcomes account for exactly the items read.
type IngestionSummary struct {
	Read     int `json:"read"`
	Created  int `json:"created"`
	Reused   int `json:"reused"`
	Ignored  int `json:"ignored"`
	Rejected int `json:"rejected"`
}

const ingestionColumns = `id, engagement_id, tool, format_version, filename, encode(sha256, 'hex'), byte_size, items_read, items_created, items_reused, items_ignored, items_rejected, received_at, imported_by`

// RecordIngestion imports one already-parsed artifact in a single transaction:
// it inserts the assets the artifact identified, then records the ingestion with
// the summary those inserts produced. Assets are inserted first because the
// created and reused counts are only known afterwards, and the asset's ingestion
// reference is deferred to the commit for exactly that reason.
//
// An ingestion only ever creates: a name already present in the engagement is
// reused as it stands, never renamed or rewritten, so importing a scan cannot
// silently change what an operator recorded by hand. `names` must already be
// distinct.
func RecordIngestion(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, engagementID string, ingestion Ingestion, names []string) (Ingestion, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Ingestion{}, fmt.Errorf("begin ingestion transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var owned bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM engagements WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, engagementID).Scan(&owned); err != nil {
		return Ingestion{}, fmt.Errorf("find engagement: %w", err)
	}
	if !owned {
		return Ingestion{}, ErrNotFound
	}

	// The same artifact is imported at most once per engagement. The unique
	// constraint below is what actually decides it; this lookup exists so the
	// caller can name the earlier ingestion instead of only refusing the upload.
	var previous string
	err = tx.QueryRow(ctx, `SELECT id FROM tool_ingestions WHERE organization_id = $1 AND engagement_id = $2 AND sha256 = decode($3, 'hex')`, session.OrganizationID, engagementID, ingestion.SHA256).Scan(&previous)
	if err == nil {
		return Ingestion{ID: previous}, ErrDuplicate
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Ingestion{}, fmt.Errorf("find earlier ingestion: %w", err)
	}

	var ingestionID, correlationID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid(), gen_random_uuid()`).Scan(&ingestionID, &correlationID); err != nil {
		return Ingestion{}, fmt.Errorf("reserve ingestion identifier: %w", err)
	}

	rows, err := tx.Query(ctx, `INSERT INTO assets (organization_id, engagement_id, name, source, ingestion_id)
		SELECT $1, $2, name, 'ingest', $3 FROM unnest($4::text[]) AS name
		ON CONFLICT (organization_id, engagement_id, name) DO NOTHING
		RETURNING id`, session.OrganizationID, engagementID, ingestionID, names)
	if err != nil {
		return Ingestion{}, fmt.Errorf("insert ingested assets: %w", err)
	}
	createdIDs, err := scanIdentifiers(rows)
	if err != nil {
		return Ingestion{}, err
	}
	ingestion.Summary.Created = len(createdIDs)
	ingestion.Summary.Reused = len(names) - len(createdIDs)

	var recorded Ingestion
	err = tx.QueryRow(ctx, `INSERT INTO tool_ingestions (id, organization_id, engagement_id, tool, format_version, filename, sha256, byte_size, items_read, items_created, items_reused, items_ignored, items_rejected, imported_by)
		VALUES ($1, $2, $3, $4, $5, $6, decode($7, 'hex'), $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+ingestionColumns,
		ingestionID, session.OrganizationID, engagementID, ingestion.Tool, ingestion.FormatVersion, ingestion.Filename, ingestion.SHA256, ingestion.ByteSize,
		ingestion.Summary.Read, ingestion.Summary.Created, ingestion.Summary.Reused, ingestion.Summary.Ignored, ingestion.Summary.Rejected, session.UserID).Scan(scanIngestion(&recorded)...)
	if duplicateArtifact(err) {
		return Ingestion{}, ErrDuplicate
	}
	if err != nil {
		return Ingestion{}, fmt.Errorf("record ingestion: %w", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context)
		VALUES ($1, $2, 'ingestion.recorded', 'ingestion', $3, 'success', $4, jsonb_build_object('engagementId', $5::uuid, 'tool', $6::text, 'formatVersion', $7::text, 'sha256', $8::text, 'byteSize', $9::bigint, 'read', $10::int, 'created', $11::int, 'reused', $12::int, 'ignored', $13::int, 'rejected', $14::int))`,
		session.OrganizationID, session.UserID, recorded.ID, correlationID, recorded.EngagementID, recorded.Tool, recorded.FormatVersion, recorded.SHA256, recorded.ByteSize,
		recorded.Summary.Read, recorded.Summary.Created, recorded.Summary.Reused, recorded.Summary.Ignored, recorded.Summary.Rejected); err != nil {
		return Ingestion{}, fmt.Errorf("audit ingestion: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context)
		SELECT $1, $2, 'asset.created', 'asset', asset_id, 'success', $3, jsonb_build_object('source', 'ingest', 'ingestionId', $4::uuid)
		FROM unnest($5::uuid[]) AS asset_id`, session.OrganizationID, session.UserID, correlationID, recorded.ID, createdIDs); err != nil {
		return Ingestion{}, fmt.Errorf("audit ingested assets: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		if duplicateArtifact(err) {
			return Ingestion{}, ErrDuplicate
		}
		return Ingestion{}, fmt.Errorf("commit ingestion transaction: %w", err)
	}
	return recorded, nil
}

// ListIngestions returns the import history of one engagement, so what a scan
// contributed to the inventory stays readable after the fact.
func ListIngestions(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, engagementID string) ([]Ingestion, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM engagements WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, engagementID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("find engagement: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := pool.Query(ctx, `SELECT `+ingestionColumns+` FROM tool_ingestions WHERE organization_id = $1 AND engagement_id = $2 ORDER BY received_at, id`, session.OrganizationID, engagementID)
	if err != nil {
		return nil, fmt.Errorf("list ingestions: %w", err)
	}
	defer rows.Close()
	var items []Ingestion
	for rows.Next() {
		var ingestion Ingestion
		if err := rows.Scan(scanIngestion(&ingestion)...); err != nil {
			return nil, fmt.Errorf("scan ingestion: %w", err)
		}
		items = append(items, ingestion)
	}
	return items, rows.Err()
}

func scanIngestion(ingestion *Ingestion) []any {
	return []any{&ingestion.ID, &ingestion.EngagementID, &ingestion.Tool, &ingestion.FormatVersion, &ingestion.Filename, &ingestion.SHA256, &ingestion.ByteSize,
		&ingestion.Summary.Read, &ingestion.Summary.Created, &ingestion.Summary.Reused, &ingestion.Summary.Ignored, &ingestion.Summary.Rejected, &ingestion.ReceivedAt, &ingestion.ImportedBy}
}

func scanIdentifiers(rows pgx.Rows) ([]string, error) {
	defer rows.Close()
	identifiers := []string{}
	for rows.Next() {
		var identifier string
		if err := rows.Scan(&identifier); err != nil {
			return nil, fmt.Errorf("scan identifier: %w", err)
		}
		identifiers = append(identifiers, identifier)
	}
	return identifiers, rows.Err()
}

// duplicateArtifact recognizes only the artifact uniqueness constraint, so an
// unrelated unique violation is never reported to a caller as a replayed import.
func duplicateArtifact(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505" && databaseError.ConstraintName == "tool_ingestions_artifact_key"
}
