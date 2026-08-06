package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type OrganizationMember struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	IsActive    bool   `json:"isActive"`
}

type AuditEvent struct {
	ID         string          `json:"id"`
	Action     string          `json:"action"`
	TargetType string          `json:"targetType"`
	TargetID   string          `json:"targetId"`
	Outcome    string          `json:"outcome"`
	CreatedAt  time.Time       `json:"createdAt"`
	Context    json.RawMessage `json:"context"`
}

func ReadOrganization(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, session Session) (Organization, error) {
	if session.Role != "admin" {
		return Organization{}, ErrForbidden
	}
	var organization Organization
	if err := pool.QueryRow(ctx, `SELECT id, name FROM organizations WHERE id = $1`, session.OrganizationID).Scan(&organization.ID, &organization.Name); err != nil {
		return Organization{}, fmt.Errorf("read organization: %w", err)
	}
	return organization, nil
}

func UpdateOrganization(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, name string) (Organization, error) {
	if session.Role != "admin" {
		return Organization{}, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return Organization{}, errors.New("invalid organization name")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Organization{}, fmt.Errorf("begin organization update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var organization Organization
	if err := tx.QueryRow(ctx, `UPDATE organizations SET name = $2 WHERE id = $1 RETURNING id, name`, session.OrganizationID, name).Scan(&organization.ID, &organization.Name); err != nil {
		return Organization{}, fmt.Errorf("update organization: %w", err)
	}
	if err := auditOrganization(ctx, tx, session, "organization.updated", "organization", organization.ID); err != nil {
		return Organization{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Organization{}, fmt.Errorf("commit organization update: %w", err)
	}
	return organization, nil
}

func ListOrganizationMembers(ctx context.Context, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session) ([]OrganizationMember, error) {
	if session.Role != "admin" {
		return nil, ErrForbidden
	}
	rows, err := pool.Query(ctx, `SELECT users.id, users.display_name, users.email, organization_memberships.role, organization_memberships.is_active FROM organization_memberships JOIN users ON users.id = organization_memberships.user_id WHERE organization_memberships.organization_id = $1 ORDER BY users.display_name, users.id`, session.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization members: %w", err)
	}
	defer rows.Close()
	var members []OrganizationMember
	for rows.Next() {
		var member OrganizationMember
		if err := rows.Scan(&member.ID, &member.DisplayName, &member.Email, &member.Role, &member.IsActive); err != nil {
			return nil, fmt.Errorf("scan organization member: %w", err)
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func CreateOrganizationMember(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, member OrganizationMember, password string) (OrganizationMember, error) {
	if session.Role != "admin" {
		return OrganizationMember{}, ErrForbidden
	}
	member.DisplayName, member.Email = strings.TrimSpace(member.DisplayName), strings.TrimSpace(member.Email)
	if member.DisplayName == "" || member.Email == "" || (member.Role != "admin" && member.Role != "member") || len(password) < 12 {
		return OrganizationMember{}, errors.New("invalid member")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return OrganizationMember{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return OrganizationMember{}, fmt.Errorf("begin member creation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := tx.QueryRow(ctx, `INSERT INTO users (display_name, email, password_hash) VALUES ($1, $2, $3) RETURNING id`, member.DisplayName, member.Email, hash).Scan(&member.ID); err != nil {
		return OrganizationMember{}, fmt.Errorf("create member user: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, $3)`, session.OrganizationID, member.ID, member.Role); err != nil {
		return OrganizationMember{}, fmt.Errorf("create member membership: %w", err)
	}
	member.IsActive = true
	if err := auditOrganization(ctx, tx, session, "organization.member.created", "user", member.ID); err != nil {
		return OrganizationMember{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrganizationMember{}, fmt.Errorf("commit member creation: %w", err)
	}
	return member, nil
}

func UpdateOrganizationMember(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, userID, role string, active *bool) (OrganizationMember, error) {
	if session.Role != "admin" {
		return OrganizationMember{}, ErrForbidden
	}
	if (role != "" && role != "admin" && role != "member") || (role == "" && active == nil) || (role != "" && active != nil) {
		return OrganizationMember{}, errors.New("invalid member update")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return OrganizationMember{}, fmt.Errorf("begin member update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Every last-admin transition takes this shared organization lock before a
	// member lock, so concurrent targets cannot acquire membership rows in a
	// different order and deadlock.
	var organizationID string
	if err := tx.QueryRow(ctx, `SELECT id FROM organizations WHERE id = $1 FOR UPDATE`, session.OrganizationID).Scan(&organizationID); err != nil {
		return OrganizationMember{}, fmt.Errorf("lock organization: %w", err)
	}
	var currentRole string
	var currentActive bool
	if err := tx.QueryRow(ctx, `SELECT role, is_active FROM organization_memberships WHERE organization_id = $1 AND user_id = $2 FOR UPDATE`, session.OrganizationID, userID).Scan(&currentRole, &currentActive); errors.Is(err, pgx.ErrNoRows) {
		return OrganizationMember{}, ErrNotFound
	} else if err != nil {
		return OrganizationMember{}, fmt.Errorf("lock member: %w", err)
	}
	newRole, newActive := currentRole, currentActive
	if role != "" {
		newRole = role
	}
	if active != nil {
		newActive = *active
	}
	if currentRole == "admin" && currentActive && (newRole != "admin" || !newActive) {
		locked, err := tx.Query(ctx, `SELECT user_id FROM organization_memberships WHERE organization_id = $1 AND role = 'admin' AND is_active FOR UPDATE`, session.OrganizationID)
		if err != nil {
			return OrganizationMember{}, fmt.Errorf("lock active admins: %w", err)
		}
		for locked.Next() {
		}
		if err := locked.Err(); err != nil {
			locked.Close()
			return OrganizationMember{}, fmt.Errorf("read active admins: %w", err)
		}
		locked.Close()
		var admins int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM organization_memberships WHERE organization_id = $1 AND role = 'admin' AND is_active`, session.OrganizationID).Scan(&admins); err != nil {
			return OrganizationMember{}, fmt.Errorf("count active admins: %w", err)
		}
		if admins < 2 {
			return OrganizationMember{}, ErrInvalidState
		}
	}
	var member OrganizationMember
	if err := tx.QueryRow(ctx, `UPDATE organization_memberships SET role = $3, is_active = $4 WHERE organization_id = $1 AND user_id = $2 RETURNING user_id, role, is_active`, session.OrganizationID, userID, newRole, newActive).Scan(&member.ID, &member.Role, &member.IsActive); err != nil {
		return OrganizationMember{}, fmt.Errorf("update member: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT display_name, email FROM users WHERE id = $1`, member.ID).Scan(&member.DisplayName, &member.Email); err != nil {
		return OrganizationMember{}, fmt.Errorf("read member profile: %w", err)
	}
	if !newActive {
		if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE organization_id = $1 AND user_id = $2 AND revoked_at IS NULL`, session.OrganizationID, member.ID); err != nil {
			return OrganizationMember{}, fmt.Errorf("revoke member sessions: %w", err)
		}
	}
	action := "organization.member.role.updated"
	if active != nil {
		action = "organization.member.activated"
		if !newActive {
			action = "organization.member.deactivated"
		}
	}
	if err := auditOrganization(ctx, tx, session, action, "user", member.ID); err != nil {
		return OrganizationMember{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrganizationMember{}, fmt.Errorf("commit member update: %w", err)
	}
	return member, nil
}

func ListAuditEvents(ctx context.Context, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, action, cursor string, limit int) ([]AuditEvent, string, error) {
	if session.Role != "admin" {
		return nil, "", ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return nil, "", errors.New("invalid audit limit")
	}
	rows, err := pool.Query(ctx, `WITH cursor_event AS (SELECT created_at, id FROM audit_events WHERE organization_id = $1 AND id = NULLIF($2, '')::uuid) SELECT id, action, target_type, target_id, outcome, created_at, context FROM audit_events WHERE organization_id = $1 AND ($3 = '' OR action = $3) AND ($2 = '' OR (created_at, id) < (SELECT created_at, id FROM cursor_event)) ORDER BY created_at DESC, id DESC LIMIT $4`, session.OrganizationID, cursor, action, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(&event.ID, &event.Action, &event.TargetType, &event.TargetID, &event.Outcome, &event.CreatedAt, &event.Context); err != nil {
			return nil, "", fmt.Errorf("scan audit event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(events) > limit {
		events = events[:limit]
		next = events[len(events)-1].ID
	}
	return events, next, nil
}

func auditOrganization(ctx context.Context, tx pgx.Tx, session Session, action, targetType, targetID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, $3, $4, $5, 'success', gen_random_uuid(), '{}'::jsonb)`, session.OrganizationID, session.UserID, action, targetType, targetID); err != nil {
		return fmt.Errorf("audit organization administration: %w", err)
	}
	return nil
}
