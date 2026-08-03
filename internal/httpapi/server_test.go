package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/store/postgres"
)

func TestAuthenticatedOrganizationPortfolio(t *testing.T) {
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

	tokenFile := writeBootstrapToken(t)
	admin, err := postgres.BootstrapFirstAdmin(ctx, pool, postgres.BootstrapInput{
		OrganizationName: "HTTP Test Organization",
		DisplayName:      "HTTP Test Admin",
		Email:            "admin@example.test",
		Password:         "correct horse battery staple",
		TokenFile:        tokenFile,
	})
	if err != nil {
		t.Fatalf("bootstrap first admin: %v", err)
	}
	if _, err := os.Stat(tokenFile); !os.IsNotExist(err) {
		t.Fatalf("bootstrap token file = %v, want removed", err)
	}
	server := New(pool)
	login := request(t, server, http.MethodPost, "/v1/session/login", `{"email":"admin@example.test","password":"correct horse battery staple"}`, nil, "")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login status = %d, want %d: %s", login.Code, http.StatusNoContent, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("session cookie = %#v, want __Host secure host-only cookie", cookie)
	}

	csrf := request(t, server, http.MethodGet, "/v1/csrf", "", cookie, "")
	if csrf.Code != http.StatusOK {
		t.Fatalf("csrf status = %d, want %d", csrf.Code, http.StatusOK)
	}
	var csrfBody struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(csrf.Body).Decode(&csrfBody); err != nil || csrfBody.Token == "" {
		t.Fatalf("decode csrf = %v, body=%s", err, csrf.Body.String())
	}

	missingCSRF := request(t, server, http.MethodPost, "/v1/clients", `{"name":"Blocked Client"}`, cookie, "")
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want %d", missingCSRF.Code, http.StatusForbidden)
	}

	client := request(t, server, http.MethodPost, "/v1/clients", `{"name":"Acme"}`, cookie, csrfBody.Token)
	if client.Code != http.StatusCreated {
		t.Fatalf("create client status = %d, want %d: %s", client.Code, http.StatusCreated, client.Body.String())
	}
	var clientBody struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(client.Body).Decode(&clientBody); err != nil || clientBody.ID == "" {
		t.Fatalf("decode client = %v, body=%s", err, client.Body.String())
	}

	engagement := request(t, server, http.MethodPost, "/v1/clients/"+url.PathEscape(clientBody.ID)+"/engagements", `{"name":"Q3"}`, cookie, csrfBody.Token)
	if engagement.Code != http.StatusCreated {
		t.Fatalf("create engagement status = %d, want %d: %s", engagement.Code, http.StatusCreated, engagement.Body.String())
	}
	var engagementBody struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(engagement.Body).Decode(&engagementBody); err != nil || engagementBody.ID == "" {
		t.Fatalf("decode engagement = %v, body=%s", err, engagement.Body.String())
	}

	invalidFinding := request(t, server, http.MethodPost, "/v1/engagements/"+url.PathEscape(engagementBody.ID)+"/findings", `{"title":"SQL injection","cvssVector":"CVSS:4.0/invalid"}`, cookie, csrfBody.Token)
	if invalidFinding.Code != http.StatusBadRequest {
		t.Fatalf("invalid finding status = %d, want %d", invalidFinding.Code, http.StatusBadRequest)
	}
	finding := request(t, server, http.MethodPost, "/v1/engagements/"+url.PathEscape(engagementBody.ID)+"/findings", `{"title":"SQL injection","description":"Injection in login","impact":"Account access","remediation":"Parameterize query","reproduction":"Submit crafted username","cvssVector":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}`, cookie, csrfBody.Token)
	if finding.Code != http.StatusCreated || !strings.Contains(finding.Body.String(), `"cvssScore":9.8`) {
		t.Fatalf("create finding status = %d, body=%s", finding.Code, finding.Body.String())
	}
	findings := request(t, server, http.MethodGet, "/v1/engagements/"+url.PathEscape(engagementBody.ID)+"/findings", "", cookie, "")
	if findings.Code != http.StatusOK || !strings.Contains(findings.Body.String(), "SQL injection") {
		t.Fatalf("list findings status = %d, body=%s", findings.Code, findings.Body.String())
	}
	missingEngagement := request(t, server, http.MethodGet, "/v1/engagements/00000000-0000-0000-0000-000000000000/findings", "", cookie, "")
	if missingEngagement.Code != http.StatusNotFound {
		t.Fatalf("missing engagement status = %d, want %d", missingEngagement.Code, http.StatusNotFound)
	}

	logout := request(t, server, http.MethodPost, "/v1/session/logout", "", cookie, csrfBody.Token)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", logout.Code, http.StatusNoContent)
	}
	if retry := request(t, server, http.MethodGet, "/v1/csrf", "", cookie, ""); retry.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d, want %d", retry.Code, http.StatusUnauthorized)
	}

	if count := auditCount(t, ctx, pool, admin.OrganizationID, "client.created"); count != 1 {
		t.Fatalf("client audit events = %d, want 1", count)
	}
	if count := auditCount(t, ctx, pool, admin.OrganizationID, "engagement.created"); count != 1 {
		t.Fatalf("engagement audit events = %d, want 1", count)
	}
	if count := auditCount(t, ctx, pool, admin.OrganizationID, "finding.created"); count != 1 {
		t.Fatalf("finding audit events = %d, want 1", count)
	}
}

func writeBootstrapToken(t *testing.T) string {
	t.Helper()
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		t.Fatalf("generate bootstrap token: %v", err)
	}
	path := filepath.Join(t.TempDir(), "bootstrap-token")
	if err := os.WriteFile(path, []byte(base64.RawURLEncoding.EncodeToString(token)), 0o600); err != nil {
		t.Fatalf("write bootstrap token: %v", err)
	}
	return path
}

func request(t *testing.T, handler http.Handler, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func auditCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, action string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE organization_id = $1 AND action = $2`, organizationID, action).Scan(&count); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	return count
}
