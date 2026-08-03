package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MethodologyItem is one executable checklist entry. Its position is assigned
// by the database from the order the author submitted, so a caller can neither
// number nor reorder items by hand.
type MethodologyItem struct {
	Position         int    `json:"position"`
	Title            string `json:"title"`
	Objective        string `json:"objective"`
	Preconditions    string `json:"preconditions"`
	Procedure        string `json:"procedure"`
	ExpectedEvidence string `json:"expectedEvidence"`
	Reference        string `json:"reference"`
	Notes            string `json:"notes"`
}

// MethodologyVersion is one version of a template: original structured content
// that names the source it was derived from, that source's own version and its
// attribution.
type MethodologyVersion struct {
	ID            string            `json:"id"`
	TemplateID    string            `json:"templateId"`
	VersionNumber int               `json:"versionNumber"`
	State         string            `json:"state"`
	Name          string            `json:"name"`
	SourceName    string            `json:"sourceName"`
	SourceVersion string            `json:"sourceVersion"`
	Attribution   string            `json:"attribution"`
	CreatedBy     string            `json:"createdBy"`
	CreatedAt     time.Time         `json:"createdAt"`
	PublishedBy   *string           `json:"publishedBy"`
	PublishedAt   *time.Time        `json:"publishedAt"`
	Items         []MethodologyItem `json:"items,omitempty"`
}

// EngagementChecklist is the copy one engagement received when it was created.
// It records the exact published version it was copied from and never follows
// later edits of the library.
type EngagementChecklist struct {
	ID                string            `json:"id"`
	EngagementID      string            `json:"engagementId"`
	TemplateVersionID string            `json:"templateVersionId"`
	VersionNumber     int               `json:"versionNumber"`
	Name              string            `json:"name"`
	SourceName        string            `json:"sourceName"`
	SourceVersion     string            `json:"sourceVersion"`
	Attribution       string            `json:"attribution"`
	CreatedAt         time.Time         `json:"createdAt"`
	Items             []MethodologyItem `json:"items"`
}

const (
	methodologyVersionColumns   = `id, template_id, version_number, state, name, source_name, source_version, attribution, created_by, created_at, published_by, published_at`
	methodologyItemColumns      = `position, title, objective, preconditions, procedure, expected_evidence, reference, notes`
	engagementChecklistColumns  = `id, engagement_id, template_version_id, version_number, name, source_name, source_version, attribution, created_at`
	methodologyItemDestinations = `organization_id, version_id, position, title, objective, preconditions, procedure, expected_evidence, reference, notes`
)

// DraftMethodologyTemplate creates one template and its first draft version.
// Every member may draft; the draft belongs to its author until an
// administrator publishes it.
func DraftMethodologyTemplate(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, version MethodologyVersion) (MethodologyVersion, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return MethodologyVersion{}, fmt.Errorf("begin methodology draft transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var templateID string
	if err := tx.QueryRow(ctx, `INSERT INTO methodology_templates (organization_id, created_by) VALUES ($1, $2) RETURNING id`, session.OrganizationID, session.UserID).Scan(&templateID); err != nil {
		return MethodologyVersion{}, fmt.Errorf("insert methodology template: %w", err)
	}
	items := version.Items
	if err := tx.QueryRow(ctx, `INSERT INTO methodology_template_versions (organization_id, template_id, version_number, name, source_name, source_version, attribution, created_by)
		VALUES ($1, $2, 1, $3, $4, $5, $6, $7) RETURNING `+methodologyVersionColumns,
		session.OrganizationID, templateID, version.Name, version.SourceName, version.SourceVersion, version.Attribution, session.UserID).Scan(scanMethodologyVersion(&version)...); err != nil {
		return MethodologyVersion{}, fmt.Errorf("insert methodology version: %w", err)
	}
	if err := replaceMethodologyItems(ctx, tx, session, version.ID, items); err != nil {
		return MethodologyVersion{}, err
	}
	if version.Items, err = readMethodologyItems(ctx, tx, session, version.ID); err != nil {
		return MethodologyVersion{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'methodology.template.drafted', 'methodology_template', $3, 'success', gen_random_uuid(), jsonb_build_object('versionId', $4::uuid, 'itemCount', $5::int))`,
		session.OrganizationID, session.UserID, templateID, version.ID, len(version.Items)); err != nil {
		return MethodologyVersion{}, fmt.Errorf("audit methodology draft: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MethodologyVersion{}, fmt.Errorf("commit methodology draft transaction: %w", err)
	}
	return version, nil
}

// UpdateMethodologyDraft replaces the whole content of a template's draft.
// Only the author of the draft may edit it, and a draft another member owns is
// reported as missing rather than as forbidden, so drafts stay invisible
// outside their author. A published version is never rewritten.
func UpdateMethodologyDraft(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, templateID string, version MethodologyVersion) (MethodologyVersion, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return MethodologyVersion{}, fmt.Errorf("begin methodology draft update transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var versionID, state, author string
	err = tx.QueryRow(ctx, `SELECT id, state, created_by FROM methodology_template_versions WHERE organization_id = $1 AND template_id = $2 ORDER BY version_number DESC LIMIT 1 FOR UPDATE`, session.OrganizationID, templateID).Scan(&versionID, &state, &author)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return MethodologyVersion{}, ErrNotFound
	case err != nil:
		return MethodologyVersion{}, fmt.Errorf("lock methodology draft: %w", err)
	case state != "draft":
		return MethodologyVersion{}, ErrInvalidState
	case author != session.UserID:
		return MethodologyVersion{}, ErrNotFound
	}
	items := version.Items
	if err := tx.QueryRow(ctx, `UPDATE methodology_template_versions SET name = $3, source_name = $4, source_version = $5, attribution = $6 WHERE organization_id = $1 AND id = $2 RETURNING `+methodologyVersionColumns,
		session.OrganizationID, versionID, version.Name, version.SourceName, version.SourceVersion, version.Attribution).Scan(scanMethodologyVersion(&version)...); err != nil {
		return MethodologyVersion{}, fmt.Errorf("update methodology draft: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM methodology_template_items WHERE organization_id = $1 AND version_id = $2`, session.OrganizationID, versionID); err != nil {
		return MethodologyVersion{}, fmt.Errorf("clear methodology draft items: %w", err)
	}
	if err := replaceMethodologyItems(ctx, tx, session, versionID, items); err != nil {
		return MethodologyVersion{}, err
	}
	if version.Items, err = readMethodologyItems(ctx, tx, session, versionID); err != nil {
		return MethodologyVersion{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'methodology.template.draft.updated', 'methodology_template', $3, 'success', gen_random_uuid(), jsonb_build_object('versionId', $4::uuid, 'itemCount', $5::int))`,
		session.OrganizationID, session.UserID, templateID, versionID, len(version.Items)); err != nil {
		return MethodologyVersion{}, fmt.Errorf("audit methodology draft update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MethodologyVersion{}, fmt.Errorf("commit methodology draft update transaction: %w", err)
	}
	return version, nil
}

// PublishMethodologyVersion turns a template's draft into the immutable version
// the organization shares. Only an administrator may publish, and a draft
// without a single item is refused because an empty checklist is not a
// methodology. Ownership and the required state are one predicate on the update
// itself, so a replay changes nothing and a template owned by another
// organization is indistinguishable from a missing one.
func PublishMethodologyVersion(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, templateID string) (MethodologyVersion, error) {
	if session.Role != "admin" {
		return MethodologyVersion{}, ErrForbidden
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return MethodologyVersion{}, fmt.Errorf("begin methodology publication transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var version MethodologyVersion
	err = tx.QueryRow(ctx, `UPDATE methodology_template_versions SET state = 'published', published_by = $3, published_at = now()
		WHERE organization_id = $1 AND template_id = $2 AND state = 'draft'
		AND EXISTS (SELECT 1 FROM methodology_template_items WHERE organization_id = $1 AND version_id = methodology_template_versions.id)
		RETURNING `+methodologyVersionColumns, session.OrganizationID, templateID, session.UserID).Scan(scanMethodologyVersion(&version)...)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM methodology_templates WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, templateID).Scan(&exists); err != nil {
			return MethodologyVersion{}, fmt.Errorf("find methodology template: %w", err)
		}
		if !exists {
			return MethodologyVersion{}, ErrNotFound
		}
		return MethodologyVersion{}, ErrInvalidState
	}
	if err != nil {
		return MethodologyVersion{}, fmt.Errorf("publish methodology version: %w", err)
	}
	version.Items, err = readMethodologyItems(ctx, tx, session, version.ID)
	if err != nil {
		return MethodologyVersion{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'methodology.template.published', 'methodology_template', $3, 'success', gen_random_uuid(), jsonb_build_object('versionId', $4::uuid, 'versionNumber', $5::int, 'itemCount', $6::int))`,
		session.OrganizationID, session.UserID, templateID, version.ID, version.VersionNumber, len(version.Items)); err != nil {
		return MethodologyVersion{}, fmt.Errorf("audit methodology publication: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MethodologyVersion{}, fmt.Errorf("commit methodology publication transaction: %w", err)
	}
	return version, nil
}

// ListMethodologyVersions returns the organization's shared library together
// with the drafts the caller may act on: their own, and every draft in the
// organization for an administrator, who is the only role that can publish one.
func ListMethodologyVersions(ctx context.Context, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session) ([]MethodologyVersion, error) {
	rows, err := pool.Query(ctx, `SELECT `+methodologyVersionColumns+` FROM methodology_template_versions
		WHERE organization_id = $1 AND (state = 'published' OR created_by = $2 OR $3)
		ORDER BY created_at, id`, session.OrganizationID, session.UserID, session.Role == "admin")
	if err != nil {
		return nil, fmt.Errorf("list methodology versions: %w", err)
	}
	defer rows.Close()
	var versions []MethodologyVersion
	for rows.Next() {
		var version MethodologyVersion
		if err := rows.Scan(scanMethodologyVersion(&version)...); err != nil {
			return nil, fmt.Errorf("scan methodology version: %w", err)
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

// ReadEngagementChecklist returns the copy one engagement received. An
// engagement created without a methodology, one owned by another organization
// and one that does not exist are all reported as missing.
func ReadEngagementChecklist(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, engagementID string) (EngagementChecklist, error) {
	var checklist EngagementChecklist
	err := pool.QueryRow(ctx, `SELECT `+engagementChecklistColumns+` FROM engagement_checklists WHERE organization_id = $1 AND engagement_id = $2`, session.OrganizationID, engagementID).Scan(
		&checklist.ID, &checklist.EngagementID, &checklist.TemplateVersionID, &checklist.VersionNumber, &checklist.Name, &checklist.SourceName, &checklist.SourceVersion, &checklist.Attribution, &checklist.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return EngagementChecklist{}, ErrNotFound
	}
	if err != nil {
		return EngagementChecklist{}, fmt.Errorf("read engagement checklist: %w", err)
	}
	rows, err := pool.Query(ctx, `SELECT `+methodologyItemColumns+` FROM engagement_checklist_items WHERE organization_id = $1 AND checklist_id = $2 ORDER BY position`, session.OrganizationID, checklist.ID)
	if err != nil {
		return EngagementChecklist{}, fmt.Errorf("list engagement checklist items: %w", err)
	}
	checklist.Items, err = scanMethodologyItems(rows)
	if err != nil {
		return EngagementChecklist{}, err
	}
	return checklist, nil
}

// snapshotEngagementChecklist copies one published version into a new
// engagement, inside the transaction that creates the engagement. A version
// that is not published, or that belongs to another organization, is reported
// as missing and the engagement is never created.
func snapshotEngagementChecklist(ctx context.Context, tx pgx.Tx, session Session, engagementID, versionID string) error {
	var checklistID string
	var versionNumber int
	err := tx.QueryRow(ctx, `INSERT INTO engagement_checklists (organization_id, engagement_id, template_version_id, version_number, name, source_name, source_version, attribution)
		SELECT $1, $2, id, version_number, name, source_name, source_version, attribution
		FROM methodology_template_versions WHERE organization_id = $1 AND id = $3 AND state = 'published'
		RETURNING id, version_number`, session.OrganizationID, engagementID, versionID).Scan(&checklistID, &versionNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("snapshot engagement checklist: %w", err)
	}
	copied, err := tx.Exec(ctx, `INSERT INTO engagement_checklist_items (organization_id, checklist_id, `+methodologyItemColumns+`)
		SELECT $1, $2, `+methodologyItemColumns+` FROM methodology_template_items WHERE organization_id = $1 AND version_id = $3`,
		session.OrganizationID, checklistID, versionID)
	if err != nil {
		return fmt.Errorf("copy engagement checklist items: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'engagement.checklist.snapshotted', 'engagement', $3, 'success', gen_random_uuid(), jsonb_build_object('checklistId', $4::uuid, 'templateVersionId', $5::uuid, 'versionNumber', $6::int, 'itemCount', $7::bigint))`,
		session.OrganizationID, session.UserID, engagementID, checklistID, versionID, versionNumber, copied.RowsAffected()); err != nil {
		return fmt.Errorf("audit engagement checklist snapshot: %w", err)
	}
	return nil
}

// replaceMethodologyItems writes the submitted items in the order they were
// submitted. The database numbers them, so positions are always contiguous and
// never client-supplied.
func replaceMethodologyItems(ctx context.Context, tx pgx.Tx, session Session, versionID string, items []MethodologyItem) error {
	titles := make([]string, len(items))
	objectives := make([]string, len(items))
	preconditions := make([]string, len(items))
	procedures := make([]string, len(items))
	expectedEvidence := make([]string, len(items))
	references := make([]string, len(items))
	notes := make([]string, len(items))
	for index, item := range items {
		titles[index] = item.Title
		objectives[index] = item.Objective
		preconditions[index] = item.Preconditions
		procedures[index] = item.Procedure
		expectedEvidence[index] = item.ExpectedEvidence
		references[index] = item.Reference
		notes[index] = item.Notes
	}
	if _, err := tx.Exec(ctx, `INSERT INTO methodology_template_items (`+methodologyItemDestinations+`)
		SELECT $1, $2, item.ordinal, item.title, item.objective, item.preconditions, item.procedure, item.expected_evidence, item.reference, item.notes
		FROM unnest($3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[])
		WITH ORDINALITY AS item(title, objective, preconditions, procedure, expected_evidence, reference, notes, ordinal)`,
		session.OrganizationID, versionID, titles, objectives, preconditions, procedures, expectedEvidence, references, notes); err != nil {
		return fmt.Errorf("insert methodology items: %w", err)
	}
	return nil
}

func readMethodologyItems(ctx context.Context, queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, versionID string) ([]MethodologyItem, error) {
	rows, err := queryer.Query(ctx, `SELECT `+methodologyItemColumns+` FROM methodology_template_items WHERE organization_id = $1 AND version_id = $2 ORDER BY position`, session.OrganizationID, versionID)
	if err != nil {
		return nil, fmt.Errorf("list methodology items: %w", err)
	}
	return scanMethodologyItems(rows)
}

func scanMethodologyItems(rows pgx.Rows) ([]MethodologyItem, error) {
	defer rows.Close()
	var items []MethodologyItem
	for rows.Next() {
		var item MethodologyItem
		if err := rows.Scan(&item.Position, &item.Title, &item.Objective, &item.Preconditions, &item.Procedure, &item.ExpectedEvidence, &item.Reference, &item.Notes); err != nil {
			return nil, fmt.Errorf("scan methodology item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanMethodologyVersion(version *MethodologyVersion) []any {
	return []any{&version.ID, &version.TemplateID, &version.VersionNumber, &version.State, &version.Name, &version.SourceName, &version.SourceVersion, &version.Attribution, &version.CreatedBy, &version.CreatedAt, &version.PublishedBy, &version.PublishedAt}
}
