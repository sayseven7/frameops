package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sayseven7/frameops/internal/domain"
)

type ProjectPlan struct {
	EngagementID      string              `json:"engagementId"`
	OwnerUserID       string              `json:"ownerUserId"`
	Status            string              `json:"status"`
	StartsOn          time.Time           `json:"startsOn"`
	EndsOn            time.Time           `json:"endsOn"`
	RulesOfEngagement string              `json:"rulesOfEngagement"`
	Scope             ProjectScope        `json:"scope"`
	Team              []ProjectTeamMember `json:"team"`
	Milestones        []ProjectMilestone  `json:"milestones"`
}

type ProjectScope struct {
	VersionNumber int       `json:"versionNumber"`
	Targets       []string  `json:"targets"`
	Exclusions    []string  `json:"exclusions"`
	CreatedAt     time.Time `json:"createdAt"`
}

type ProjectTeamMember struct {
	UserID string `json:"userId"`
	Role   string `json:"role"`
}

type ProjectMilestone struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	DueOn       time.Time  `json:"dueOn"`
	CompletedAt *time.Time `json:"completedAt"`
}

func CreateProjectPlan(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, plan ProjectPlan) (ProjectPlan, error) {
	if err := validProjectPlan(plan); err != nil {
		return ProjectPlan{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ProjectPlan{}, fmt.Errorf("begin project plan transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := tx.QueryRow(ctx, `INSERT INTO engagement_plans (organization_id, engagement_id, owner_user_id, starts_on, ends_on, rules_of_engagement)
		SELECT $1, id, $2, $3, $4, $5 FROM engagements WHERE organization_id = $1 AND id = $6
		RETURNING engagement_id, owner_user_id, status, starts_on, ends_on, rules_of_engagement`, session.OrganizationID, session.UserID, plan.StartsOn, plan.EndsOn, plan.RulesOfEngagement, plan.EngagementID).Scan(&plan.EngagementID, &plan.OwnerUserID, &plan.Status, &plan.StartsOn, &plan.EndsOn, &plan.RulesOfEngagement); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectPlan{}, ErrNotFound
		}
		return ProjectPlan{}, fmt.Errorf("create project plan: %w", err)
	}
	if err := writeProjectPlanDetails(ctx, tx, session, &plan); err != nil {
		return ProjectPlan{}, err
	}
	if err := auditProjectPlan(ctx, tx, session, plan.EngagementID, "project.plan.created", map[string]any{"scopeVersion": plan.Scope.VersionNumber}); err != nil {
		return ProjectPlan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectPlan{}, fmt.Errorf("commit project plan: %w", err)
	}
	return plan, nil
}

func UpdateProjectPlan(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, plan ProjectPlan) (ProjectPlan, error) {
	if err := validProjectPlan(plan); err != nil {
		return ProjectPlan{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ProjectPlan{}, fmt.Errorf("begin project plan update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := authorizeProjectPlan(ctx, tx, session, plan.EngagementID); err != nil {
		return ProjectPlan{}, err
	}
	if err := tx.QueryRow(ctx, `UPDATE engagement_plans SET starts_on = $3, ends_on = $4, rules_of_engagement = $5, updated_at = now()
		WHERE organization_id = $1 AND engagement_id = $2 AND status = 'draft'
		RETURNING engagement_id, owner_user_id, status, starts_on, ends_on, rules_of_engagement`, session.OrganizationID, plan.EngagementID, plan.StartsOn, plan.EndsOn, plan.RulesOfEngagement).Scan(&plan.EngagementID, &plan.OwnerUserID, &plan.Status, &plan.StartsOn, &plan.EndsOn, &plan.RulesOfEngagement); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectPlan{}, ErrInvalidState
		}
		return ProjectPlan{}, fmt.Errorf("update project plan: %w", err)
	}
	if err := writeProjectPlanDetails(ctx, tx, session, &plan); err != nil {
		return ProjectPlan{}, err
	}
	if err := auditProjectPlan(ctx, tx, session, plan.EngagementID, "project.plan.updated", map[string]any{"scopeVersion": plan.Scope.VersionNumber}); err != nil {
		return ProjectPlan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectPlan{}, fmt.Errorf("commit project plan update: %w", err)
	}
	return plan, nil
}

func TransitionProjectPlan(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, engagementID, status string) (ProjectPlan, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ProjectPlan{}, fmt.Errorf("begin project plan transition: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := authorizeProjectPlan(ctx, tx, session, engagementID); err != nil {
		return ProjectPlan{}, err
	}
	var from string
	if err := tx.QueryRow(ctx, `SELECT status FROM engagement_plans WHERE organization_id = $1 AND engagement_id = $2 FOR UPDATE`, session.OrganizationID, engagementID).Scan(&from); errors.Is(err, pgx.ErrNoRows) {
		return ProjectPlan{}, ErrNotFound
	} else if err != nil {
		return ProjectPlan{}, fmt.Errorf("lock project plan: %w", err)
	}
	if !domain.ValidProjectPlanTransition(from, status) {
		return ProjectPlan{}, ErrInvalidState
	}
	if status == "active" {
		var scopeCount, leadCount int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM engagement_scope_versions WHERE organization_id = $1 AND engagement_id = $2), (SELECT count(*) FROM engagement_team_members WHERE organization_id = $1 AND engagement_id = $2 AND role = 'lead')`, session.OrganizationID, engagementID).Scan(&scopeCount, &leadCount); err != nil {
			return ProjectPlan{}, fmt.Errorf("validate project plan activation: %w", err)
		}
		if scopeCount == 0 || leadCount == 0 {
			return ProjectPlan{}, ErrInvalidState
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE engagement_plans SET status = $3, updated_at = now() WHERE organization_id = $1 AND engagement_id = $2`, session.OrganizationID, engagementID, status); err != nil {
		return ProjectPlan{}, fmt.Errorf("transition project plan: %w", err)
	}
	if err := auditProjectPlan(ctx, tx, session, engagementID, "project.plan.transitioned", map[string]any{"from": from, "to": status}); err != nil {
		return ProjectPlan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectPlan{}, fmt.Errorf("commit project plan transition: %w", err)
	}
	return ProjectPlan{EngagementID: engagementID, Status: status}, nil
}

func ReadProjectPlan(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, engagementID string) (ProjectPlan, error) {
	var plan ProjectPlan
	if err := pool.QueryRow(ctx, `SELECT engagement_id, owner_user_id, status, starts_on, ends_on, rules_of_engagement FROM engagement_plans WHERE organization_id = $1 AND engagement_id = $2`, session.OrganizationID, engagementID).Scan(&plan.EngagementID, &plan.OwnerUserID, &plan.Status, &plan.StartsOn, &plan.EndsOn, &plan.RulesOfEngagement); errors.Is(err, pgx.ErrNoRows) {
		return ProjectPlan{}, ErrNotFound
	} else if err != nil {
		return ProjectPlan{}, fmt.Errorf("read project plan: %w", err)
	}
	if err := pool.QueryRow(ctx, `SELECT version_number, targets, exclusions, created_at FROM engagement_scope_versions WHERE organization_id = $1 AND engagement_id = $2 ORDER BY version_number DESC LIMIT 1`, session.OrganizationID, engagementID).Scan(&plan.Scope.VersionNumber, &plan.Scope.Targets, &plan.Scope.Exclusions, &plan.Scope.CreatedAt); err != nil {
		return ProjectPlan{}, fmt.Errorf("read project scope: %w", err)
	}
	team, err := pool.Query(ctx, `SELECT user_id, role FROM engagement_team_members WHERE organization_id = $1 AND engagement_id = $2 ORDER BY role, user_id`, session.OrganizationID, engagementID)
	if err != nil {
		return ProjectPlan{}, fmt.Errorf("read project team: %w", err)
	}
	defer team.Close()
	for team.Next() {
		var member ProjectTeamMember
		if err := team.Scan(&member.UserID, &member.Role); err != nil {
			return ProjectPlan{}, fmt.Errorf("scan project team: %w", err)
		}
		plan.Team = append(plan.Team, member)
	}
	if err := team.Err(); err != nil {
		return ProjectPlan{}, err
	}
	milestones, err := pool.Query(ctx, `SELECT id, title, due_on, completed_at FROM engagement_milestones WHERE organization_id = $1 AND engagement_id = $2 ORDER BY due_on, id`, session.OrganizationID, engagementID)
	if err != nil {
		return ProjectPlan{}, fmt.Errorf("read project milestones: %w", err)
	}
	defer milestones.Close()
	for milestones.Next() {
		var milestone ProjectMilestone
		if err := milestones.Scan(&milestone.ID, &milestone.Title, &milestone.DueOn, &milestone.CompletedAt); err != nil {
			return ProjectPlan{}, fmt.Errorf("scan project milestone: %w", err)
		}
		plan.Milestones = append(plan.Milestones, milestone)
	}
	return plan, milestones.Err()
}

func validProjectPlan(plan ProjectPlan) error {
	if plan.EngagementID == "" || plan.StartsOn.IsZero() || plan.EndsOn.IsZero() || plan.EndsOn.Before(plan.StartsOn) || len(plan.RulesOfEngagement) == 0 || len(plan.RulesOfEngagement) > 4000 || len(plan.Scope.Targets) == 0 || len(plan.Scope.Targets) > 128 || len(plan.Scope.Exclusions) > 128 || len(plan.Team) == 0 || len(plan.Team) > 64 || len(plan.Milestones) > 128 {
		return errors.New("invalid project plan")
	}
	for _, target := range append(append([]string{}, plan.Scope.Targets...), plan.Scope.Exclusions...) {
		if len(target) == 0 || len(target) > 500 {
			return errors.New("invalid project scope")
		}
	}
	for _, member := range plan.Team {
		if member.UserID == "" || (member.Role != "lead" && member.Role != "tester" && member.Role != "reviewer") {
			return errors.New("invalid project team")
		}
	}
	for _, milestone := range plan.Milestones {
		if milestone.Title == "" || len(milestone.Title) > 200 || milestone.DueOn.IsZero() || milestone.DueOn.Before(plan.StartsOn) || milestone.DueOn.After(plan.EndsOn) {
			return errors.New("invalid project milestone")
		}
	}
	return nil
}

func writeProjectPlanDetails(ctx context.Context, tx pgx.Tx, session Session, plan *ProjectPlan) error {
	targets, _ := json.Marshal(plan.Scope.Targets)
	exclusions, _ := json.Marshal(plan.Scope.Exclusions)
	if plan.Scope.Exclusions == nil {
		exclusions = []byte("[]")
	}
	if err := tx.QueryRow(ctx, `INSERT INTO engagement_scope_versions (organization_id, engagement_id, version_number, targets, exclusions, created_by) SELECT $1, $2, coalesce(max(version_number), 0) + 1, $3::jsonb, $4::jsonb, $5 FROM engagement_scope_versions WHERE organization_id = $1 AND engagement_id = $2 RETURNING version_number, created_at`, session.OrganizationID, plan.EngagementID, targets, exclusions, session.UserID).Scan(&plan.Scope.VersionNumber, &plan.Scope.CreatedAt); err != nil {
		return fmt.Errorf("snapshot project scope: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM engagement_team_members WHERE organization_id = $1 AND engagement_id = $2`, session.OrganizationID, plan.EngagementID); err != nil {
		return fmt.Errorf("clear project team: %w", err)
	}
	for _, member := range plan.Team {
		if _, err := tx.Exec(ctx, `INSERT INTO engagement_team_members (organization_id, engagement_id, user_id, role) VALUES ($1, $2, $3, $4)`, session.OrganizationID, plan.EngagementID, member.UserID, member.Role); err != nil {
			return fmt.Errorf("write project team: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM engagement_milestones WHERE organization_id = $1 AND engagement_id = $2`, session.OrganizationID, plan.EngagementID); err != nil {
		return fmt.Errorf("clear project milestones: %w", err)
	}
	for index := range plan.Milestones {
		milestone := &plan.Milestones[index]
		if err := tx.QueryRow(ctx, `INSERT INTO engagement_milestones (organization_id, engagement_id, title, due_on) VALUES ($1, $2, $3, $4) RETURNING id, completed_at`, session.OrganizationID, plan.EngagementID, milestone.Title, milestone.DueOn).Scan(&milestone.ID, &milestone.CompletedAt); err != nil {
			return fmt.Errorf("write project milestone: %w", err)
		}
	}
	return nil
}

func authorizeProjectPlan(ctx context.Context, tx pgx.Tx, session Session, engagementID string) error {
	var owner string
	if err := tx.QueryRow(ctx, `SELECT owner_user_id FROM engagement_plans WHERE organization_id = $1 AND engagement_id = $2`, session.OrganizationID, engagementID).Scan(&owner); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("read project plan owner: %w", err)
	}
	if owner != session.UserID && session.Role != "admin" {
		return ErrNotFound
	}
	return nil
}

func auditProjectPlan(ctx context.Context, tx pgx.Tx, session Session, engagementID, action string, details map[string]any) error {
	contextJSON, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, $3, 'engagement', $4, 'success', gen_random_uuid(), $5::jsonb)`, session.OrganizationID, session.UserID, action, engagementID, contextJSON); err != nil {
		return fmt.Errorf("audit project plan: %w", err)
	}
	return nil
}
