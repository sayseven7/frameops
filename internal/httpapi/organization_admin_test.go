package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/render"
	"github.com/sayseven7/frameops/internal/store/objectstore"
	"github.com/sayseven7/frameops/internal/store/postgres"
)

func TestOrganizationAdminRBACIsolationCSRFAuditAndPagination(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for HTTP integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	server := New(pool, objectstore.Bucket{}, render.Worker{})
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	organizationID := createOrganization(t, ctx, pool, "Organization Admin "+suffix)
	adminCookie, adminCSRF := signIn(t, ctx, server, pool, organizationID, "admin", "organization-admin-"+suffix+"@example.test")
	memberCookie, memberCSRF := signIn(t, ctx, server, pool, organizationID, "member", "organization-member-"+suffix+"@example.test")

	if response := request(t, server, http.MethodGet, "/v1/organization", "", memberCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("member reads organization administration status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
	}
	if response := request(t, server, http.MethodPut, "/v1/organization", `{"name":"Renamed Organization"}`, memberCookie, memberCSRF); response.Code != http.StatusForbidden {
		t.Fatalf("member updates organization status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
	}
	if response := request(t, server, http.MethodPut, "/v1/organization", `{"name":"Renamed Organization"}`, adminCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("organization update without CSRF status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if response := request(t, server, http.MethodPut, "/v1/organization", `{"name":"Renamed Organization"}`, adminCookie, adminCSRF); response.Code != http.StatusOK {
		t.Fatalf("admin updates organization status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	created := request(t, server, http.MethodPost, "/v1/organization/members", `{"displayName":"New Member","email":"new-member-`+suffix+`@example.test","password":"correct horse battery staple","role":"member"}`, adminCookie, adminCSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create member status = %d, want %d: %s", created.Code, http.StatusCreated, created.Body.String())
	}
	var member struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&member); err != nil || member.ID == "" {
		t.Fatalf("decode created member = %v", err)
	}
	if response := request(t, server, http.MethodPatch, "/v1/organization/members/"+member.ID, `{"role":"admin"}`, memberCookie, memberCSRF); response.Code != http.StatusForbidden {
		t.Fatalf("member changes role status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if response := request(t, server, http.MethodPatch, "/v1/organization/members/"+member.ID, `{"role":"admin"}`, adminCookie, adminCSRF); response.Code != http.StatusOK {
		t.Fatalf("admin changes role status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if response := request(t, server, http.MethodPatch, "/v1/organization/members/"+member.ID, `{"role":"member","isActive":true}`, adminCookie, adminCSRF); response.Code != http.StatusBadRequest {
		t.Fatalf("combined member update status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if response := request(t, server, http.MethodPatch, "/v1/organization/members/"+member.ID, `{"isActive":false}`, adminCookie, adminCSRF); response.Code != http.StatusOK {
		t.Fatalf("admin deactivates member status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	if response := request(t, server, http.MethodGet, "/v1/organization/audit-events?limit=1", "", memberCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("member lists audit events status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if response := request(t, server, http.MethodGet, "/v1/organization/audit-events?limit=1&action=organization.member.deactivated", "", adminCookie, ""); response.Code != http.StatusOK || !json.Valid(response.Body.Bytes()) {
		t.Fatalf("admin lists paginated audit events status = %d, body=%s", response.Code, response.Body.String())
	}

	outsideOrganizationID := createOrganization(t, ctx, pool, "Outside Organization Admin "+suffix)
	outsideCookie, outsideCSRF := signIn(t, ctx, server, pool, outsideOrganizationID, "admin", "outside-organization-admin-"+suffix+"@example.test")
	if response := request(t, server, http.MethodPatch, "/v1/organization/members/"+member.ID, `{"role":"member"}`, outsideCookie, outsideCSRF); response.Code != http.StatusNotFound {
		t.Fatalf("cross-organization role change status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestOrganizationAdminConcurrentLastAdminTransition(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for HTTP integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	server := New(pool, objectstore.Bucket{}, render.Worker{})
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	organizationID := createOrganization(t, ctx, pool, "Concurrent Organization "+suffix)
	adminEmail := "concurrent-admin-" + suffix + "@example.test"
	adminCookie, adminCSRF := signIn(t, ctx, server, pool, organizationID, "admin", adminEmail)
	created := request(t, server, http.MethodPost, "/v1/organization/members", `{"displayName":"Second Admin","email":"second-admin-`+suffix+`@example.test","password":"correct horse battery staple","role":"admin"}`, adminCookie, adminCSRF)
	if created.Code != http.StatusCreated {
		t.Fatalf("create second admin status = %d, want %d: %s", created.Code, http.StatusCreated, created.Body.String())
	}
	var secondAdmin struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&secondAdmin); err != nil || secondAdmin.ID == "" {
		t.Fatalf("decode second admin = %v", err)
	}
	var firstAdminID string
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, adminEmail).Scan(&firstAdminID); err != nil {
		t.Fatalf("find first admin: %v", err)
	}

	session := postgres.Session{OrganizationID: organizationID, UserID: firstAdminID, Role: "admin"}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, userID := range []string{firstAdminID, secondAdmin.ID} {
		wait.Add(1)
		go func(userID string) {
			defer wait.Done()
			<-start
			_, err := postgres.UpdateOrganizationMember(ctx, pool, session, userID, "member", nil)
			results <- err
		}(userID)
	}
	close(start)
	wait.Wait()
	close(results)
	var succeeded, conflicts int
	for err := range results {
		switch err {
		case nil:
			succeeded++
		case postgres.ErrInvalidState:
			conflicts++
		default:
			t.Fatalf("concurrent transition error = %v", err)
		}
	}
	if succeeded != 1 || conflicts != 1 {
		t.Fatalf("concurrent transitions succeeded=%d conflicts=%d, want one of each", succeeded, conflicts)
	}
	var activeAdmins int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM organization_memberships WHERE organization_id = $1 AND role = 'admin' AND is_active`, organizationID).Scan(&activeAdmins); err != nil {
		t.Fatalf("count active admins: %v", err)
	}
	if activeAdmins != 1 {
		t.Fatalf("active admins = %d, want 1", activeAdmins)
	}
}
