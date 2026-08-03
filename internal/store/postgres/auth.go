package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/argon2"
)

const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 1
	argonKeyLen  = 32
	sessionTTL   = 12 * time.Hour
)

var (
	ErrBootstrapUsed = errors.New("first admin bootstrap has already been consumed")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrNotFound      = errors.New("not found")
	ErrInvalidState  = errors.New("invalid state")
	ErrDuplicate     = errors.New("duplicate")
)

type Pool interface {
	Begin(context.Context) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type BootstrapInput struct {
	OrganizationName string
	DisplayName      string
	Email            string
	Password         string
	TokenFile        string
}

type BootstrapResult struct {
	OrganizationID string
	UserID         string
}

type Session struct {
	ID             string
	UserID         string
	OrganizationID string
	Role           string
	CSRFHash       []byte
}

type Client struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type Engagement struct {
	ID        string    `json:"id"`
	ClientID  string    `json:"clientId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// Asset carries where its name came from: an operator recorded it by hand, or
// exactly one tool ingestion created it. Imported inventory and human
// interpretation are never merged into one indistinguishable list.
type Asset struct {
	ID           string    `json:"id"`
	EngagementID string    `json:"engagementId"`
	Name         string    `json:"name"`
	Source       string    `json:"source"`
	IngestionID  *string   `json:"ingestionId"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Finding struct {
	ID               string    `json:"id"`
	EngagementID     string    `json:"engagementId"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	Impact           string    `json:"impact"`
	Remediation      string    `json:"remediation"`
	Reproduction     string    `json:"reproduction"`
	CVSSVector       string    `json:"cvssVector"`
	CVSSScore        float64   `json:"cvssScore"`
	ValidationState  string    `json:"validationState"`
	RemediationState *string   `json:"remediationState"`
	CreatedAt        time.Time `json:"createdAt"`
}

type Retest struct {
	ID             string    `json:"id"`
	FindingID      string    `json:"findingId"`
	Round          int       `json:"round"`
	PreviousState  string    `json:"previousState"`
	ResultState    string    `json:"resultState"`
	Procedure      string    `json:"procedure"`
	ObservedResult string    `json:"observedResult"`
	Justification  string    `json:"justification"`
	PerformedBy    string    `json:"performedBy"`
	CreatedAt      time.Time `json:"createdAt"`
}

// BootstrapFirstAdmin consumes the local one-time credential only after its database transaction commits.
func BootstrapFirstAdmin(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, input BootstrapInput) (BootstrapResult, error) {
	if strings.TrimSpace(input.OrganizationName) == "" || strings.TrimSpace(input.DisplayName) == "" || strings.TrimSpace(input.Email) == "" || input.Password == "" {
		return BootstrapResult{}, errors.New("organization, display name, email, and password are required")
	}
	if err := validateBootstrapTokenFile(input.TokenFile); err != nil {
		return BootstrapResult{}, err
	}
	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return BootstrapResult{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8675309)`); err != nil {
		return BootstrapResult{}, fmt.Errorf("lock bootstrap: %w", err)
	}
	var consumed bool
	err = tx.QueryRow(ctx, `SELECT TRUE FROM bootstrap_consumptions WHERE id = TRUE`).Scan(&consumed)
	if err == nil {
		return BootstrapResult{}, ErrBootstrapUsed
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return BootstrapResult{}, fmt.Errorf("check bootstrap consumption: %w", err)
	}

	var result BootstrapResult
	if err := tx.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ($1) RETURNING id`, strings.TrimSpace(input.OrganizationName)).Scan(&result.OrganizationID); err != nil {
		return BootstrapResult{}, fmt.Errorf("create organization: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO users (display_name, email, password_hash) VALUES ($1, $2, $3) RETURNING id`, strings.TrimSpace(input.DisplayName), strings.TrimSpace(input.Email), passwordHash).Scan(&result.UserID); err != nil {
		return BootstrapResult{}, fmt.Errorf("create admin user: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'admin')`, result.OrganizationID, result.UserID); err != nil {
		return BootstrapResult{}, fmt.Errorf("create admin membership: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO bootstrap_consumptions (id) VALUES (TRUE)`); err != nil {
		return BootstrapResult{}, fmt.Errorf("record bootstrap consumption: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'auth.bootstrap.succeeded', 'user', $2, 'success', gen_random_uuid(), '{}'::jsonb)`, result.OrganizationID, result.UserID); err != nil {
		return BootstrapResult{}, fmt.Errorf("audit bootstrap: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return BootstrapResult{}, fmt.Errorf("commit bootstrap: %w", err)
	}
	if err := os.Remove(input.TokenFile); err != nil {
		return BootstrapResult{}, fmt.Errorf("remove consumed bootstrap token file: %w", err)
	}
	return result, nil
}

func HashPassword(password string) (string, error) {
	salt, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, base64.RawURLEncoding.EncodeToString(salt), base64.RawURLEncoding.EncodeToString(hash)), nil
}

func Authenticate(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, email, password string) (string, error) {
	var userID, passwordHash string
	var active bool
	err := pool.QueryRow(ctx, `SELECT id, COALESCE(password_hash, ''), is_active FROM users WHERE email = $1`, strings.TrimSpace(email)).Scan(&userID, &passwordHash, &active)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("load login user: %w", err)
	}
	credentialHash := passwordHash
	if err != nil || !active || !validPasswordHash(passwordHash) {
		credentialHash = dummyPasswordHash()
	}
	valid := verifyPassword(password, credentialHash)
	if err != nil || !active || !valid {
		return "", ErrUnauthorized
	}

	var organizationID, role string
	if err := pool.QueryRow(ctx, `SELECT organization_id, role FROM organization_memberships WHERE user_id = $1 ORDER BY created_at, organization_id LIMIT 1`, userID).Scan(&organizationID, &role); err != nil {
		return "", ErrUnauthorized
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	csrf, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	csrfHash := sha256.Sum256(csrf)
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (user_id, organization_id, token_hash, csrf_hash, expires_at) VALUES ($1, $2, $3, $4, now() + interval '12 hours')`, userID, organizationID, tokenHash(token), csrfHash[:]); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

func SessionForToken(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, token string) (Session, error) {
	var session Session
	if err := pool.QueryRow(ctx, `SELECT sessions.id, sessions.user_id, sessions.organization_id, organization_memberships.role, sessions.csrf_hash FROM sessions JOIN organization_memberships ON organization_memberships.organization_id = sessions.organization_id AND organization_memberships.user_id = sessions.user_id WHERE sessions.token_hash = $1 AND sessions.revoked_at IS NULL AND sessions.expires_at > now()`, tokenHash(token)).Scan(&session.ID, &session.UserID, &session.OrganizationID, &session.Role, &session.CSRFHash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrUnauthorized
		}
		return Session{}, fmt.Errorf("load session: %w", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE id = $1`, session.ID); err != nil {
		return Session{}, fmt.Errorf("touch session: %w", err)
	}
	return session, nil
}

func ValidCSRF(session Session, token string) bool {
	hash := sha256.Sum256([]byte(token))
	return token != "" && subtle.ConstantTimeCompare(hash[:], session.CSRFHash) == 1
}

func IssueCSRF(ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, session Session) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET csrf_hash = $1 WHERE id = $2 AND revoked_at IS NULL AND expires_at > now()`, tokenHash(token), session.ID); err != nil {
		return "", fmt.Errorf("rotate csrf token: %w", err)
	}
	return token, nil
}

func RevokeSession(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin logout transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, session.ID); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'auth.session.revoked', 'session', $3, 'success', gen_random_uuid(), '{}'::jsonb)`, session.OrganizationID, session.UserID, session.ID); err != nil {
		return fmt.Errorf("audit session revocation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit logout: %w", err)
	}
	return nil
}

func CreateClient(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, name string) (Client, error) {
	if session.Role != "admin" {
		return Client{}, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Client{}, errors.New("client name is required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Client{}, fmt.Errorf("begin client transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var client Client
	if err := tx.QueryRow(ctx, `INSERT INTO clients (organization_id, name) VALUES ($1, $2) RETURNING id, name, created_at`, session.OrganizationID, name).Scan(&client.ID, &client.Name, &client.CreatedAt); err != nil {
		return Client{}, fmt.Errorf("insert client: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'client.created', 'client', $3, 'success', gen_random_uuid(), '{}'::jsonb)`, session.OrganizationID, session.UserID, client.ID); err != nil {
		return Client{}, fmt.Errorf("audit client creation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Client{}, fmt.Errorf("commit client transaction: %w", err)
	}
	return client, nil
}

func ListClients(ctx context.Context, pool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session) ([]Client, error) {
	rows, err := pool.Query(ctx, `SELECT id, name, created_at FROM clients WHERE organization_id = $1 ORDER BY created_at, id`, session.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}
	defer rows.Close()
	var clients []Client
	for rows.Next() {
		var client Client
		if err := rows.Scan(&client.ID, &client.Name, &client.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan client: %w", err)
		}
		clients = append(clients, client)
	}
	return clients, rows.Err()
}

// CreateEngagement opens one engagement and, when the caller selected a
// published methodology version, copies it into the engagement's own immutable
// checklist in the same transaction. An engagement is never created with a
// checklist the selected version could not produce.
func CreateEngagement(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, clientID, name, methodologyVersionID string) (Engagement, error) {
	if session.Role != "admin" {
		return Engagement{}, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Engagement{}, errors.New("engagement name is required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Engagement{}, fmt.Errorf("begin engagement transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var engagement Engagement
	err = tx.QueryRow(ctx, `INSERT INTO engagements (organization_id, client_id, name) SELECT $1, id, $3 FROM clients WHERE organization_id = $1 AND id = $2 RETURNING id, client_id, name, created_at`, session.OrganizationID, clientID, name).Scan(&engagement.ID, &engagement.ClientID, &engagement.Name, &engagement.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Engagement{}, ErrNotFound
	}
	if err != nil {
		return Engagement{}, fmt.Errorf("insert engagement: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'engagement.created', 'engagement', $3, 'success', gen_random_uuid(), '{}'::jsonb)`, session.OrganizationID, session.UserID, engagement.ID); err != nil {
		return Engagement{}, fmt.Errorf("audit engagement creation: %w", err)
	}
	if methodologyVersionID != "" {
		if err := snapshotEngagementChecklist(ctx, tx, session, engagement.ID, methodologyVersionID); err != nil {
			return Engagement{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Engagement{}, fmt.Errorf("commit engagement transaction: %w", err)
	}
	return engagement, nil
}

func ListEngagements(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, clientID string) ([]Engagement, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM clients WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, clientID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("find client: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := pool.Query(ctx, `SELECT id, client_id, name, created_at FROM engagements WHERE organization_id = $1 AND client_id = $2 ORDER BY created_at, id`, session.OrganizationID, clientID)
	if err != nil {
		return nil, fmt.Errorf("list engagements: %w", err)
	}
	defer rows.Close()
	var engagements []Engagement
	for rows.Next() {
		var engagement Engagement
		if err := rows.Scan(&engagement.ID, &engagement.ClientID, &engagement.Name, &engagement.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan engagement: %w", err)
		}
		engagements = append(engagements, engagement)
	}
	return engagements, rows.Err()
}

func validateBootstrapTokenFile(path string) error {
	if path == "" {
		return errors.New("bootstrap token file is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat bootstrap token file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || int(info.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() {
		return errors.New("bootstrap token file must be a regular owner-only file")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read bootstrap token file: %w", err)
	}
	token, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(contents)))
	if err != nil || len(token) != 32 {
		return errors.New("bootstrap token file must contain one 32-byte base64url token")
	}
	return nil
}

func verifyPassword(password, encoded string) bool {
	if !validPasswordHash(encoded) {
		return false
	}
	parts := strings.Split(encoded, "$")
	salt, _ := base64.RawURLEncoding.DecodeString(parts[3])
	expected, _ := base64.RawURLEncoding.DecodeString(parts[4])
	actual := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func validPasswordHash(encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" || parts[2] != fmt.Sprintf("m=%d,t=%d,p=%d", argonMemory, argonTime, argonThreads) {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(salt) != 16 {
		return false
	}
	hash, err := base64.RawURLEncoding.DecodeString(parts[4])
	return err == nil && len(hash) == argonKeyLen
}

func dummyPasswordHash() string {
	return "argon2id$v=19$m=65536,t=3,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
}

func randomToken() (string, error) {
	bytes, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func randomBytes(length int) ([]byte, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}
	return bytes, nil
}

func tokenHash(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

func CreateFinding(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, engagementID string, finding Finding) (Finding, error) {
	finding.Title = strings.TrimSpace(finding.Title)
	if finding.Title == "" {
		return Finding{}, errors.New("finding title is required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Finding{}, fmt.Errorf("begin finding transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	err = tx.QueryRow(ctx, `INSERT INTO findings (organization_id, engagement_id, title, description, impact, remediation, reproduction, cvss_vector, cvss_score, created_by) SELECT $1, id, $3, $4, $5, $6, $7, $8, $9, $10 FROM engagements WHERE organization_id = $1 AND id = $2 RETURNING id, engagement_id, title, description, impact, remediation, reproduction, cvss_vector, cvss_score, validation_state, remediation_state, created_at`, session.OrganizationID, engagementID, finding.Title, finding.Description, finding.Impact, finding.Remediation, finding.Reproduction, finding.CVSSVector, finding.CVSSScore, session.UserID).Scan(&finding.ID, &finding.EngagementID, &finding.Title, &finding.Description, &finding.Impact, &finding.Remediation, &finding.Reproduction, &finding.CVSSVector, &finding.CVSSScore, &finding.ValidationState, &finding.RemediationState, &finding.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Finding{}, ErrNotFound
	}
	if err != nil {
		return Finding{}, fmt.Errorf("insert finding: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'finding.created', 'finding', $3, 'success', gen_random_uuid(), '{}'::jsonb)`, session.OrganizationID, session.UserID, finding.ID); err != nil {
		return Finding{}, fmt.Errorf("audit finding creation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Finding{}, fmt.Errorf("commit finding transaction: %w", err)
	}
	return finding, nil
}

// TriageFinding applies the only supported triage edge: a finding still in
// 'new' with no remediation state becomes 'confirmed' and 'open'. Ownership and
// the current state are one predicate on the update itself, so a replay or a
// concurrent caller either performs the whole edge or changes nothing, and a
// finding owned by another organization is indistinguishable from a missing one.
func TriageFinding(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, findingID string) (Finding, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Finding{}, fmt.Errorf("begin finding triage transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var finding Finding
	err = tx.QueryRow(ctx, `UPDATE findings SET validation_state = 'confirmed', remediation_state = 'open' WHERE organization_id = $1 AND id = $2 AND validation_state = 'new' AND remediation_state IS NULL RETURNING id, engagement_id, title, description, impact, remediation, reproduction, cvss_vector, cvss_score, validation_state, remediation_state, created_at`, session.OrganizationID, findingID).Scan(&finding.ID, &finding.EngagementID, &finding.Title, &finding.Description, &finding.Impact, &finding.Remediation, &finding.Reproduction, &finding.CVSSVector, &finding.CVSSScore, &finding.ValidationState, &finding.RemediationState, &finding.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM findings WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, findingID).Scan(&exists); err != nil {
			return Finding{}, fmt.Errorf("find finding: %w", err)
		}
		if !exists {
			return Finding{}, ErrNotFound
		}
		return Finding{}, ErrInvalidState
	}
	if err != nil {
		return Finding{}, fmt.Errorf("triage finding: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'finding.triage.confirmed', 'finding', $3, 'success', gen_random_uuid(), '{}'::jsonb)`, session.OrganizationID, session.UserID, finding.ID); err != nil {
		return Finding{}, fmt.Errorf("audit finding triage: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Finding{}, fmt.Errorf("commit finding triage transaction: %w", err)
	}
	return finding, nil
}

// RecordRetest appends one immutable retest round and advances the finding's
// current remediation state in the same transaction, so the state a reader sees
// is always the one the recorded history produced. Ownership and the required
// current state are one predicate on the update itself: a finding owned by
// another organization is indistinguishable from a missing one, and a concurrent
// caller waits for the row and then finds a state it may no longer retest. The
// caller names the round it believes is next, so a replayed request is refused
// instead of appending a second round for the same work.
func RecordRetest(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, findingID string, retest Retest) (Retest, error) {
	retest.Procedure = strings.TrimSpace(retest.Procedure)
	retest.ObservedResult = strings.TrimSpace(retest.ObservedResult)
	retest.Justification = strings.TrimSpace(retest.Justification)
	if retest.Procedure == "" || retest.ObservedResult == "" || retest.Justification == "" {
		return Retest{}, errors.New("retest procedure, observed result, and justification are required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Retest{}, fmt.Errorf("begin retest transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var engagementID string
	err = tx.QueryRow(ctx, `UPDATE findings SET remediation_state = $3 WHERE organization_id = $1 AND id = $2 AND validation_state = 'confirmed' AND remediation_state = 'open' RETURNING engagement_id`, session.OrganizationID, findingID, retest.ResultState).Scan(&engagementID)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM findings WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, findingID).Scan(&exists); err != nil {
			return Retest{}, fmt.Errorf("find finding: %w", err)
		}
		if !exists {
			return Retest{}, ErrNotFound
		}
		return Retest{}, ErrInvalidState
	}
	if err != nil {
		return Retest{}, fmt.Errorf("advance finding remediation state: %w", err)
	}
	var nextRound int
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(round_number), 0) + 1 FROM finding_retests WHERE organization_id = $1 AND finding_id = $2`, session.OrganizationID, findingID).Scan(&nextRound); err != nil {
		return Retest{}, fmt.Errorf("read retest history: %w", err)
	}
	if retest.Round != nextRound {
		return Retest{}, ErrInvalidState
	}
	if err := tx.QueryRow(ctx, `INSERT INTO finding_retests (organization_id, engagement_id, finding_id, round_number, previous_state, result_state, executed_procedure, observed_result, justification, performed_by) VALUES ($1, $2, $3, $4, 'open', $5, $6, $7, $8, $9) RETURNING id, finding_id, round_number, previous_state, result_state, executed_procedure, observed_result, justification, performed_by, created_at`, session.OrganizationID, engagementID, findingID, retest.Round, retest.ResultState, retest.Procedure, retest.ObservedResult, retest.Justification, session.UserID).Scan(&retest.ID, &retest.FindingID, &retest.Round, &retest.PreviousState, &retest.ResultState, &retest.Procedure, &retest.ObservedResult, &retest.Justification, &retest.PerformedBy, &retest.CreatedAt); err != nil {
		return Retest{}, fmt.Errorf("insert retest round: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'finding.retest.recorded', 'finding', $3, 'success', gen_random_uuid(), jsonb_build_object('round', $4::int, 'previousState', $5::text, 'resultState', $6::text))`, session.OrganizationID, session.UserID, findingID, retest.Round, retest.PreviousState, retest.ResultState); err != nil {
		return Retest{}, fmt.Errorf("audit retest round: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Retest{}, fmt.Errorf("commit retest transaction: %w", err)
	}
	return retest, nil
}

func ListRetests(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, findingID string) ([]Retest, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM findings WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, findingID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("find finding: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := pool.Query(ctx, `SELECT id, finding_id, round_number, previous_state, result_state, executed_procedure, observed_result, justification, performed_by, created_at FROM finding_retests WHERE organization_id = $1 AND finding_id = $2 ORDER BY round_number`, session.OrganizationID, findingID)
	if err != nil {
		return nil, fmt.Errorf("list retests: %w", err)
	}
	defer rows.Close()
	var retests []Retest
	for rows.Next() {
		var retest Retest
		if err := rows.Scan(&retest.ID, &retest.FindingID, &retest.Round, &retest.PreviousState, &retest.ResultState, &retest.Procedure, &retest.ObservedResult, &retest.Justification, &retest.PerformedBy, &retest.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan retest: %w", err)
		}
		retests = append(retests, retest)
	}
	return retests, rows.Err()
}

func CreateAsset(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, engagementID, name string) (Asset, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Asset{}, errors.New("asset name is required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Asset{}, fmt.Errorf("begin asset transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var asset Asset
	err = tx.QueryRow(ctx, `INSERT INTO assets (organization_id, engagement_id, name) SELECT $1, id, $3 FROM engagements WHERE organization_id = $1 AND id = $2 RETURNING `+assetColumns, session.OrganizationID, engagementID, name).Scan(scanAsset(&asset)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	if duplicateAssetName(err) {
		return Asset{}, ErrDuplicate
	}
	if err != nil {
		return Asset{}, fmt.Errorf("insert asset: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'asset.created', 'asset', $3, 'success', gen_random_uuid(), '{}'::jsonb)`, session.OrganizationID, session.UserID, asset.ID); err != nil {
		return Asset{}, fmt.Errorf("audit asset creation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Asset{}, fmt.Errorf("commit asset transaction: %w", err)
	}
	return asset, nil
}

func ListAssets(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, engagementID string) ([]Asset, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM engagements WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, engagementID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("find engagement: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := pool.Query(ctx, `SELECT `+assetColumns+` FROM assets WHERE organization_id = $1 AND engagement_id = $2 ORDER BY created_at, id`, session.OrganizationID, engagementID)
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	return scanAssets(rows)
}

func ListFindingAssets(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, findingID string) ([]Asset, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM findings WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, findingID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("find finding: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	return linkedAssets(ctx, pool, session, findingID)
}

// ReplaceFindingAssets swaps a finding's whole asset set in one transaction. Any
// requested asset outside the finding's own organization and engagement, including
// one owned by a sibling engagement, leaves the finding untouched and reports
// ErrNotFound so callers cannot probe for identifiers they do not own.
func ReplaceFindingAssets(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, session Session, findingID string, assetIDs []string) ([]Asset, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin finding asset transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var engagementID string
	err = tx.QueryRow(ctx, `SELECT engagement_id FROM findings WHERE organization_id = $1 AND id = $2 FOR UPDATE`, session.OrganizationID, findingID).Scan(&engagementID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock finding: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM finding_assets WHERE organization_id = $1 AND finding_id = $2`, session.OrganizationID, findingID); err != nil {
		return nil, fmt.Errorf("clear finding assets: %w", err)
	}
	var owned bool
	if err := tx.QueryRow(ctx, `WITH requested AS (
		SELECT DISTINCT unnest($4::uuid[]) AS id
	), linked AS (
		INSERT INTO finding_assets (organization_id, engagement_id, finding_id, asset_id)
		SELECT $1, $2, $3, requested.id FROM requested
		JOIN assets ON assets.organization_id = $1 AND assets.engagement_id = $2 AND assets.id = requested.id
		RETURNING asset_id
	)
	SELECT (SELECT count(*) FROM requested) = (SELECT count(*) FROM linked)`, session.OrganizationID, engagementID, findingID, assetIDs).Scan(&owned); err != nil {
		return nil, fmt.Errorf("link finding assets: %w", err)
	}
	if !owned {
		return nil, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (organization_id, actor_user_id, action, target_type, target_id, outcome, correlation_id, context) VALUES ($1, $2, 'finding.assets.replaced', 'finding', $3, 'success', gen_random_uuid(), '{}'::jsonb)`, session.OrganizationID, session.UserID, findingID); err != nil {
		return nil, fmt.Errorf("audit finding asset replacement: %w", err)
	}
	assets, err := linkedAssets(ctx, tx, session, findingID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit finding asset transaction: %w", err)
	}
	return assets, nil
}

func linkedAssets(ctx context.Context, queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, findingID string) ([]Asset, error) {
	rows, err := queryer.Query(ctx, `SELECT `+qualifiedAssetColumns+` FROM finding_assets JOIN assets ON assets.organization_id = finding_assets.organization_id AND assets.id = finding_assets.asset_id WHERE finding_assets.organization_id = $1 AND finding_assets.finding_id = $2 ORDER BY assets.created_at, assets.id`, session.OrganizationID, findingID)
	if err != nil {
		return nil, fmt.Errorf("list finding assets: %w", err)
	}
	return scanAssets(rows)
}

const (
	assetColumns          = `id, engagement_id, name, source, ingestion_id, created_at`
	qualifiedAssetColumns = `assets.id, assets.engagement_id, assets.name, assets.source, assets.ingestion_id, assets.created_at`
)

func scanAssets(rows pgx.Rows) ([]Asset, error) {
	defer rows.Close()
	var assets []Asset
	for rows.Next() {
		var asset Asset
		if err := rows.Scan(scanAsset(&asset)...); err != nil {
			return nil, fmt.Errorf("scan asset: %w", err)
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func scanAsset(asset *Asset) []any {
	return []any{&asset.ID, &asset.EngagementID, &asset.Name, &asset.Source, &asset.IngestionID, &asset.CreatedAt}
}

// duplicateAssetName recognizes only the per-engagement asset name index, so an
// unrelated unique violation is never reported as a name an engagement already
// carries.
func duplicateAssetName(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505" && databaseError.ConstraintName == "assets_organization_id_engagement_id_name_key"
}

func ListFindings(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, session Session, engagementID string) ([]Finding, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM engagements WHERE organization_id = $1 AND id = $2)`, session.OrganizationID, engagementID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("find engagement: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := pool.Query(ctx, `SELECT id, engagement_id, title, description, impact, remediation, reproduction, cvss_vector, cvss_score, validation_state, remediation_state, created_at FROM findings WHERE organization_id = $1 AND engagement_id = $2 ORDER BY created_at, id`, session.OrganizationID, engagementID)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()
	var findings []Finding
	for rows.Next() {
		var finding Finding
		if err := rows.Scan(&finding.ID, &finding.EngagementID, &finding.Title, &finding.Description, &finding.Impact, &finding.Remediation, &finding.Reproduction, &finding.CVSSVector, &finding.CVSSScore, &finding.ValidationState, &finding.RemediationState, &finding.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}
