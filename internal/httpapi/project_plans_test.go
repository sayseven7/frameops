package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/render"
	"github.com/sayseven7/frameops/internal/store/objectstore"
)

func TestProjectPlansAuthorizeValidateVersionAndTransition(t *testing.T) {
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
	organizationID := createOrganization(t, ctx, pool, "Project Plan Organization")
	adminCookie, adminCSRF := signIn(t, ctx, server, pool, organizationID, "admin", "project-plan-admin@example.test")
	ownerCookie, ownerCSRF := signIn(t, ctx, server, pool, organizationID, "member", "project-plan-owner@example.test")
	memberCookie, memberCSRF := signIn(t, ctx, server, pool, organizationID, "member", "project-plan-member@example.test")
	clientID := createdID(t, server, http.MethodPost, "/v1/clients", `{"name":"Project Plan Client"}`, adminCookie, adminCSRF)
	engagementID := createdID(t, server, http.MethodPost, "/v1/clients/"+url.PathEscape(clientID)+"/engagements", `{"name":"Project Plan Engagement"}`, adminCookie, adminCSRF)
	planPath := "/v1/engagements/" + url.PathEscape(engagementID) + "/plan"
	transitionPath := planPath + "/transition"

	valid := `{"startsOn":"2026-01-01","endsOn":"2026-01-31","rulesOfEngagement":"Written authorization required.","targets":["api.example.test"],"milestones":[{"title":"Kickoff","dueOn":"2026-01-15"}]}`
	if response := request(t, server, http.MethodPost, planPath, valid, ownerCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("create plan without CSRF status = %d, want %d", response.Code, http.StatusForbidden)
	}
	for name, invalid := range map[string]string{
		"end before start":      `{"startsOn":"2026-01-31","endsOn":"2026-01-01","rulesOfEngagement":"Written authorization required.","targets":["api.example.test"]}`,
		"empty target":          `{"startsOn":"2026-01-01","endsOn":"2026-01-31","rulesOfEngagement":"Written authorization required.","targets":[" "]}`,
		"milestone before plan": `{"startsOn":"2026-01-01","endsOn":"2026-01-31","rulesOfEngagement":"Written authorization required.","targets":["api.example.test"],"milestones":[{"title":"Kickoff","dueOn":"2025-12-31"}]}`,
	} {
		if response := request(t, server, http.MethodPost, planPath, invalid, ownerCookie, ownerCSRF); response.Code != http.StatusBadRequest {
			t.Fatalf("%s plan status = %d, want %d: %s", name, response.Code, http.StatusBadRequest, response.Body.String())
		}
	}

	created := projectPlan(t, request(t, server, http.MethodPost, planPath, valid, ownerCookie, ownerCSRF), http.StatusCreated)
	if created.Status != "draft" || created.Scope.VersionNumber != 1 {
		t.Fatalf("created plan = %#v, want draft scope version 1", created)
	}
	if response := request(t, server, http.MethodPut, planPath, valid, memberCookie, memberCSRF); response.Code != http.StatusNotFound {
		t.Fatalf("member update status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
	}
	if response := request(t, server, http.MethodPost, transitionPath, `{"status":"active"}`, memberCookie, memberCSRF); response.Code != http.StatusNotFound {
		t.Fatalf("member transition status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
	}

	updated := projectPlan(t, request(t, server, http.MethodPut, planPath, `{"startsOn":"2026-01-01","endsOn":"2026-01-31","rulesOfEngagement":"Written authorization required.","targets":["admin.example.test"]}`, adminCookie, adminCSRF), http.StatusOK)
	if updated.Scope.VersionNumber != 2 || updated.Scope.Targets[0] != "admin.example.test" {
		t.Fatalf("updated scope = %#v, want immutable version 2", updated.Scope)
	}
	var databaseError *pgconn.PgError
	_, err = pool.Exec(ctx, `UPDATE engagement_scope_versions SET targets = '["mutated.example.test"]' WHERE organization_id = $1 AND engagement_id = $2 AND version_number = 1`, organizationID, engagementID)
	if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
		t.Fatalf("scope mutation error = %v, want PostgreSQL raise-exception P0001", err)
	}

	outsideOrganizationID := createOrganization(t, ctx, pool, "Project Plan Outside Organization")
	outsideCookie, outsideCSRF := signIn(t, ctx, server, pool, outsideOrganizationID, "admin", "project-plan-outside@example.test")
	if response := request(t, server, http.MethodGet, planPath, "", outsideCookie, ""); response.Code != http.StatusNotFound {
		t.Fatalf("cross-organization read status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
	}
	if response := request(t, server, http.MethodPut, planPath, valid, outsideCookie, outsideCSRF); response.Code != http.StatusNotFound {
		t.Fatalf("cross-organization update status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
	}

	active := projectPlan(t, request(t, server, http.MethodPost, transitionPath, `{"status":"active"}`, adminCookie, adminCSRF), http.StatusOK)
	if active.Status != "active" {
		t.Fatalf("active plan = %#v, want active", active)
	}
	closed := projectPlan(t, request(t, server, http.MethodPost, transitionPath, `{"status":"closed"}`, ownerCookie, ownerCSRF), http.StatusOK)
	if closed.Status != "closed" {
		t.Fatalf("closed plan = %#v, want closed", closed)
	}
	if response := request(t, server, http.MethodPut, planPath, valid, adminCookie, adminCSRF); response.Code != http.StatusConflict {
		t.Fatalf("closed plan update status = %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}
}

type projectPlanBody struct {
	Status string `json:"status"`
	Scope  struct {
		VersionNumber int      `json:"versionNumber"`
		Targets       []string `json:"targets"`
	} `json:"scope"`
}

func projectPlan(t *testing.T, response *httptest.ResponseRecorder, want int) projectPlanBody {
	t.Helper()
	if response.Code != want {
		t.Fatalf("project plan status = %d, want %d: %s", response.Code, want, response.Body.String())
	}
	var plan projectPlanBody
	if err := json.NewDecoder(response.Body).Decode(&plan); err != nil {
		t.Fatalf("decode project plan: %v", err)
	}
	return plan
}
