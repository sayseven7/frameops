package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReportRevisionApprovalIsScopedAndConflicts(t *testing.T) {
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

	server := New(pool, evidenceBucket(t))
	organizationID := createOrganization(t, ctx, pool, "Report Approval Organization")
	cookie, csrf := signIn(t, ctx, server, pool, organizationID, "admin", "report-approval@example.test")
	engagementID := reportEngagement(t, server, cookie, csrf, "Report Approval Engagement")
	pendingID := createReportRevision(t, ctx, pool, organizationID, engagementID, false)
	storedID := createReportRevision(t, ctx, pool, organizationID, engagementID, true)

	approvalPath := func(id string) string { return "/v1/report-revisions/" + url.PathEscape(id) + "/approve" }
	for name, path := range map[string]string{
		"malformed": approvalPath("not-a-uuid"),
		"missing":   approvalPath("00000000-0000-0000-0000-000000000000"),
	} {
		if response := request(t, server, http.MethodPost, path, "", cookie, csrf); response.Code != http.StatusNotFound {
			t.Fatalf("%s approval status = %d, want %d: %s", name, response.Code, http.StatusNotFound, response.Body.String())
		}
	}

	outsiderID := createOrganization(t, ctx, pool, "Report Approval Outside Organization")
	outsiderCookie, outsiderCSRF := signIn(t, ctx, server, pool, outsiderID, "admin", "report-approval-outsider@example.test")
	if response := request(t, server, http.MethodPost, approvalPath(storedID), "", outsiderCookie, outsiderCSRF); response.Code != http.StatusNotFound {
		t.Fatalf("cross-organization approval status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
	}

	if response := request(t, server, http.MethodPost, approvalPath(pendingID), "", cookie, csrf); response.Code != http.StatusConflict {
		t.Fatalf("pending approval status = %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}
	if response := request(t, server, http.MethodPost, approvalPath(storedID), "", cookie, csrf); response.Code != http.StatusOK {
		t.Fatalf("stored approval status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if response := request(t, server, http.MethodPost, approvalPath(storedID), "", cookie, csrf); response.Code != http.StatusConflict {
		t.Fatalf("replayed approval status = %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}

	raceEngagementID := reportEngagement(t, server, cookie, csrf, "Report Approval Race")
	firstID := createReportRevision(t, ctx, pool, organizationID, raceEngagementID, true)
	secondID := createReportRevision(t, ctx, pool, organizationID, raceEngagementID, true)
	responses := make(chan int, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	for _, id := range []string{firstID, secondID} {
		go func() {
			ready.Done()
			<-start
			responses <- request(t, server, http.MethodPost, approvalPath(id), "", cookie, csrf).Code
		}()
	}
	ready.Wait()
	close(start)
	statuses := []int{<-responses, <-responses}
	sort.Ints(statuses)
	if want := []int{http.StatusOK, http.StatusConflict}; statuses[0] != want[0] || statuses[1] != want[1] {
		t.Fatalf("concurrent approval statuses = %v, want %v", statuses, want)
	}
}

func reportEngagement(t *testing.T, handler http.Handler, cookie *http.Cookie, csrf, name string) string {
	t.Helper()
	clientID := createdID(t, handler, http.MethodPost, "/v1/clients", `{"name":"`+name+` Client"}`, cookie, csrf)
	return createdID(t, handler, http.MethodPost, "/v1/clients/"+url.PathEscape(clientID)+"/engagements", `{"name":"`+name+`"}`, cookie, csrf)
}

func createReportRevision(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, engagementID string, stored bool) string {
	t.Helper()
	state, storedAt := "pending", "NULL"
	if stored {
		state, storedAt = "stored", "now()"
	}
	var revisionID string
	query := `INSERT INTO report_revisions (organization_id, engagement_id, filename, sha256, byte_size, state, stored_at, imported_by)
		SELECT $1, $2, 'report.docx', decode(repeat('ab', 32), 'hex'), 1, $3, ` + storedAt + `, user_id
		FROM organization_memberships WHERE organization_id = $1 LIMIT 1 RETURNING id`
	if err := pool.QueryRow(ctx, query, organizationID, engagementID, state).Scan(&revisionID); err != nil {
		t.Fatalf("create %s report revision: %v", state, err)
	}
	return revisionID
}
