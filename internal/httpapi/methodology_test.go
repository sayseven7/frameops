package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/store/objectstore"
)

// methodologyVersionBody is the version one methodology request returns.
type methodologyVersionBody struct {
	ID            string  `json:"id"`
	TemplateID    string  `json:"templateId"`
	VersionNumber int     `json:"versionNumber"`
	State         string  `json:"state"`
	Name          string  `json:"name"`
	SourceName    string  `json:"sourceName"`
	SourceVersion string  `json:"sourceVersion"`
	Attribution   string  `json:"attribution"`
	CreatedBy     string  `json:"createdBy"`
	PublishedBy   *string `json:"publishedBy"`
	PublishedAt   *string `json:"publishedAt"`
	Items         []struct {
		Position  int    `json:"position"`
		Title     string `json:"title"`
		Objective string `json:"objective"`
		Procedure string `json:"procedure"`
		Reference string `json:"reference"`
	} `json:"items"`
}

func TestMethodologyDraftsPublishAndSnapshotEngagementChecklists(t *testing.T) {
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

	// The methodology library never reaches object storage.
	server := New(pool, objectstore.Bucket{})
	organizationID := createOrganization(t, ctx, pool, "Methodology Organization")
	adminCookie, adminCSRF := signIn(t, ctx, server, pool, organizationID, "admin", "methodology-admin@example.test")
	authorCookie, authorCSRF := signIn(t, ctx, server, pool, organizationID, "member", "methodology-author@example.test")
	otherCookie, otherCSRF := signIn(t, ctx, server, pool, organizationID, "member", "methodology-other@example.test")

	const draft = `{"name":"Web grey box","sourceName":"OWASP WSTG","sourceVersion":"4.2",` +
		`"attribution":"Original checklist written for FrameOPS, structured after OWASP WSTG 4.2.",` +
		`"items":[{"title":"Session cookie attributes","objective":"Confirm the session cookie cannot be read by scripts",` +
		`"preconditions":"One authenticated session","procedure":"Sign in and read Set-Cookie for HttpOnly, Secure and SameSite",` +
		`"expectedEvidence":"The response headers of the sign-in request","reference":"WSTG-SESS-02",` +
		`"notes":"Never record the session value itself"}]}`
	const minimalItem = `{"title":"t","objective":"o","procedure":"p"}`

	if unauthenticated := request(t, server, http.MethodPost, "/v1/methodology-templates", draft, nil, ""); unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated draft status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}
	if uncsrfed := request(t, server, http.MethodPost, "/v1/methodology-templates", draft, authorCookie, ""); uncsrfed.Code != http.StatusForbidden {
		t.Fatalf("draft without CSRF status = %d, want %d", uncsrfed.Code, http.StatusForbidden)
	}
	for name, invalid := range map[string]string{
		"unnamed":            `{"name":"  ","sourceName":"OWASP WSTG","sourceVersion":"4.2","attribution":"Structured after WSTG.","items":[` + minimalItem + `]}`,
		"unattributed":       `{"name":"Web","sourceName":"OWASP WSTG","sourceVersion":"4.2","attribution":"","items":[` + minimalItem + `]}`,
		"unsourced":          `{"name":"Web","sourceName":"","sourceVersion":"4.2","attribution":"Structured after WSTG.","items":[` + minimalItem + `]}`,
		"unversioned source": `{"name":"Web","sourceName":"OWASP WSTG","sourceVersion":"","attribution":"Structured after WSTG.","items":[` + minimalItem + `]}`,
		"item without a way to verify": `{"name":"Web","sourceName":"OWASP WSTG","sourceVersion":"4.2","attribution":"Structured after WSTG.",` +
			`"items":[{"title":"Test XSS","objective":"","procedure":""}]}`,
		"oversized title": `{"name":"Web","sourceName":"OWASP WSTG","sourceVersion":"4.2","attribution":"Structured after WSTG.",` +
			`"items":[{"title":"` + strings.Repeat("t", maxMethodologyLabelBytes+1) + `","objective":"o","procedure":"p"}]}`,
		"too many items": `{"name":"Web","sourceName":"OWASP WSTG","sourceVersion":"4.2","attribution":"Structured after WSTG.",` +
			`"items":[` + strings.Repeat(minimalItem+",", maxMethodologyItems) + minimalItem + `]}`,
		"client-numbered item": `{"name":"Web","sourceName":"OWASP WSTG","sourceVersion":"4.2","attribution":"Structured after WSTG.",` +
			`"items":[{"position":7,"title":"t","objective":"o","procedure":"p"}]}`,
	} {
		rejected := request(t, server, http.MethodPost, "/v1/methodology-templates", invalid, authorCookie, authorCSRF)
		if rejected.Code != http.StatusBadRequest {
			t.Fatalf("%s draft status = %d, want %d: %s", name, rejected.Code, http.StatusBadRequest, rejected.Body.String())
		}
	}

	drafted := methodologyVersion(t, request(t, server, http.MethodPost, "/v1/methodology-templates", draft, authorCookie, authorCSRF), http.StatusCreated)
	switch {
	case drafted.State != "draft" || drafted.VersionNumber != 1 || drafted.PublishedAt != nil:
		t.Fatalf("drafted version = %#v, want an unpublished version 1", drafted)
	case len(drafted.Items) != 1 || drafted.Items[0].Position != 1 || drafted.Items[0].Reference != "WSTG-SESS-02":
		t.Fatalf("drafted items = %#v, want one item numbered by the server", drafted.Items)
	}
	draftPath := "/v1/methodology-templates/" + url.PathEscape(drafted.TemplateID) + "/draft"
	publishPath := "/v1/methodology-templates/" + url.PathEscape(drafted.TemplateID) + "/publish"

	// A draft belongs to its author: it is not part of the shared library, and
	// only the administrator who may publish it also sees it.
	if listed := methodologyLibrary(t, server, otherCookie); strings.Contains(listed, drafted.ID) {
		t.Fatalf("another member's library = %s, want the draft hidden", listed)
	}
	for name, visible := range map[string]string{"author": methodologyLibrary(t, server, authorCookie), "admin": methodologyLibrary(t, server, adminCookie)} {
		if !strings.Contains(visible, drafted.ID) {
			t.Fatalf("%s library = %s, want the draft listed", name, visible)
		}
	}

	const edited = `{"name":"Web grey box","sourceName":"OWASP WSTG","sourceVersion":"4.2",` +
		`"attribution":"Original checklist written for FrameOPS, structured after OWASP WSTG 4.2.",` +
		`"items":[{"title":"Session cookie attributes","objective":"Confirm the session cookie cannot be read by scripts",` +
		`"procedure":"Sign in and read Set-Cookie for HttpOnly, Secure and SameSite","reference":"WSTG-SESS-02"},` +
		`{"title":"Authorization between accounts","objective":"Confirm one account cannot read another account's records",` +
		`"procedure":"Replay a record request with the session of a second account","reference":"WSTG-ATHZ-02"}]}`
	for name, forbidden := range map[string]*http.Cookie{"another member": otherCookie, "an administrator": adminCookie} {
		csrf := otherCSRF
		if name == "an administrator" {
			csrf = adminCSRF
		}
		if refused := request(t, server, http.MethodPut, draftPath, edited, forbidden, csrf); refused.Code != http.StatusNotFound {
			t.Fatalf("draft edited by %s status = %d, want %d: %s", name, refused.Code, http.StatusNotFound, refused.Body.String())
		}
	}
	if uncsrfed := request(t, server, http.MethodPut, draftPath, edited, authorCookie, ""); uncsrfed.Code != http.StatusForbidden {
		t.Fatalf("draft edit without CSRF status = %d, want %d", uncsrfed.Code, http.StatusForbidden)
	}
	updated := methodologyVersion(t, request(t, server, http.MethodPut, draftPath, edited, authorCookie, authorCSRF), http.StatusOK)
	if updated.ID != drafted.ID || updated.VersionNumber != 1 || updated.State != "draft" {
		t.Fatalf("edited version = %#v, want the same draft version 1", updated)
	}
	if len(updated.Items) != 2 || updated.Items[0].Position != 1 || updated.Items[1].Position != 2 || updated.Items[1].Reference != "WSTG-ATHZ-02" {
		t.Fatalf("edited items = %#v, want two contiguously numbered items", updated.Items)
	}

	if member := request(t, server, http.MethodPost, publishPath, "", authorCookie, authorCSRF); member.Code != http.StatusForbidden {
		t.Fatalf("publication by a member status = %d, want %d: %s", member.Code, http.StatusForbidden, member.Body.String())
	}
	if uncsrfed := request(t, server, http.MethodPost, publishPath, "", adminCookie, ""); uncsrfed.Code != http.StatusForbidden {
		t.Fatalf("publication without CSRF status = %d, want %d", uncsrfed.Code, http.StatusForbidden)
	}
	if unknown := request(t, server, http.MethodPost, "/v1/methodology-templates/00000000-0000-0000-0000-000000000000/publish", "", adminCookie, adminCSRF); unknown.Code != http.StatusNotFound {
		t.Fatalf("publication of an unknown template status = %d, want %d", unknown.Code, http.StatusNotFound)
	}
	if malformed := request(t, server, http.MethodPost, "/v1/methodology-templates/not-a-uuid/publish", "", adminCookie, adminCSRF); malformed.Code != http.StatusNotFound {
		t.Fatalf("publication of a malformed identifier status = %d, want %d", malformed.Code, http.StatusNotFound)
	}

	// An empty checklist is not a methodology, so it cannot be published.
	empty := methodologyVersion(t, request(t, server, http.MethodPost, "/v1/methodology-templates",
		`{"name":"Empty","sourceName":"PTES","sourceVersion":"1.0","attribution":"Structured after PTES 1.0.","items":[]}`, authorCookie, authorCSRF), http.StatusCreated)
	if refused := request(t, server, http.MethodPost, "/v1/methodology-templates/"+url.PathEscape(empty.TemplateID)+"/publish", "", adminCookie, adminCSRF); refused.Code != http.StatusConflict {
		t.Fatalf("publication of an empty draft status = %d, want %d: %s", refused.Code, http.StatusConflict, refused.Body.String())
	}

	published := methodologyVersion(t, request(t, server, http.MethodPost, publishPath, "", adminCookie, adminCSRF), http.StatusOK)
	switch {
	case published.State != "published" || published.PublishedAt == nil || published.PublishedBy == nil:
		t.Fatalf("published version = %#v, want a published version with its publisher recorded", published)
	case published.CreatedBy == *published.PublishedBy:
		t.Fatalf("published version author %q also published it, want the administrator recorded", published.CreatedBy)
	case len(published.Items) != 2:
		t.Fatalf("published items = %#v, want the two drafted items", published.Items)
	}
	if replayed := request(t, server, http.MethodPost, publishPath, "", adminCookie, adminCSRF); replayed.Code != http.StatusConflict {
		t.Fatalf("republished version status = %d, want %d: %s", replayed.Code, http.StatusConflict, replayed.Body.String())
	}
	if reedited := request(t, server, http.MethodPut, draftPath, edited, authorCookie, authorCSRF); reedited.Code != http.StatusConflict {
		t.Fatalf("edit of a published version status = %d, want %d: %s", reedited.Code, http.StatusConflict, reedited.Body.String())
	}
	if shared := methodologyLibrary(t, server, otherCookie); !strings.Contains(shared, published.ID) {
		t.Fatalf("shared library = %s, want the published version listed for every member", shared)
	}

	clientID := createdID(t, server, http.MethodPost, "/v1/clients", `{"name":"Methodology Client"}`, adminCookie, adminCSRF)
	engagementsPath := "/v1/clients/" + url.PathEscape(clientID) + "/engagements"
	if malformed := request(t, server, http.MethodPost, engagementsPath, `{"name":"Malformed","methodologyVersionId":"not-a-uuid"}`, adminCookie, adminCSRF); malformed.Code != http.StatusBadRequest {
		t.Fatalf("engagement with a malformed version status = %d, want %d", malformed.Code, http.StatusBadRequest)
	}
	if unpublished := request(t, server, http.MethodPost, engagementsPath, `{"name":"Unpublished","methodologyVersionId":"`+empty.ID+`"}`, adminCookie, adminCSRF); unpublished.Code != http.StatusNotFound {
		t.Fatalf("engagement selecting a draft status = %d, want %d: %s", unpublished.Code, http.StatusNotFound, unpublished.Body.String())
	}
	if listed := request(t, server, http.MethodGet, engagementsPath, "", adminCookie, ""); strings.Contains(listed.Body.String(), "Unpublished") {
		t.Fatalf("engagements = %s, want no engagement created for a refused methodology", listed.Body.String())
	}

	engagementID := createdID(t, server, http.MethodPost, engagementsPath, `{"name":"Q1","methodologyVersionId":"`+published.ID+`"}`, adminCookie, adminCSRF)
	checklistPath := "/v1/engagements/" + url.PathEscape(engagementID) + "/checklist"
	checklist := request(t, server, http.MethodGet, checklistPath, "", otherCookie, "")
	if checklist.Code != http.StatusOK {
		t.Fatalf("checklist status = %d, want %d: %s", checklist.Code, http.StatusOK, checklist.Body.String())
	}
	var snapshot struct {
		EngagementID      string `json:"engagementId"`
		TemplateVersionID string `json:"templateVersionId"`
		VersionNumber     int    `json:"versionNumber"`
		Name              string `json:"name"`
		SourceName        string `json:"sourceName"`
		SourceVersion     string `json:"sourceVersion"`
		Attribution       string `json:"attribution"`
		Items             []struct {
			Position  int    `json:"position"`
			Title     string `json:"title"`
			Procedure string `json:"procedure"`
			Reference string `json:"reference"`
		} `json:"items"`
	}
	if err := json.NewDecoder(checklist.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode checklist = %v", err)
	}
	switch {
	case snapshot.EngagementID != engagementID || snapshot.TemplateVersionID != published.ID || snapshot.VersionNumber != 1:
		t.Fatalf("checklist = %#v, want a copy of published version 1", snapshot)
	case snapshot.SourceName != "OWASP WSTG" || snapshot.SourceVersion != "4.2" || snapshot.Attribution == "":
		t.Fatalf("checklist attribution = %#v, want the source, its version and the attribution copied", snapshot)
	case len(snapshot.Items) != 2 || snapshot.Items[0].Position != 1 || snapshot.Items[1].Position != 2 || snapshot.Items[1].Reference != "WSTG-ATHZ-02":
		t.Fatalf("checklist items = %#v, want both items copied in order", snapshot.Items)
	case snapshot.Items[0].Procedure == "":
		t.Fatalf("checklist item = %#v, want the procedure copied, not only the control name", snapshot.Items[0])
	}

	withoutMethodology := createdID(t, server, http.MethodPost, engagementsPath, `{"name":"Q2"}`, adminCookie, adminCSRF)
	if absent := request(t, server, http.MethodGet, "/v1/engagements/"+url.PathEscape(withoutMethodology)+"/checklist", "", adminCookie, ""); absent.Code != http.StatusNotFound {
		t.Fatalf("checklist of an engagement without a methodology status = %d, want %d", absent.Code, http.StatusNotFound)
	}

	outsiderID := createOrganization(t, ctx, pool, "Methodology Outside Organization")
	outsiderCookie, outsiderCSRF := signIn(t, ctx, server, pool, outsiderID, "admin", "methodology-outsider@example.test")
	if leaked := methodologyLibrary(t, server, outsiderCookie); strings.Contains(leaked, published.ID) {
		t.Fatalf("outside library = %s, want another organization's library invisible", leaked)
	}
	for name, path := range map[string]string{"checklist": checklistPath, "draft": draftPath, "publication": publishPath} {
		method, body := http.MethodGet, ""
		switch name {
		case "draft":
			method, body = http.MethodPut, edited
		case "publication":
			method = http.MethodPost
		}
		if outsider := request(t, server, method, path, body, outsiderCookie, outsiderCSRF); outsider.Code != http.StatusNotFound {
			t.Fatalf("cross-organization %s status = %d, want %d: %s", name, outsider.Code, http.StatusNotFound, outsider.Body.String())
		}
	}
	outsiderClient := createdID(t, server, http.MethodPost, "/v1/clients", `{"name":"Outside Client"}`, outsiderCookie, outsiderCSRF)
	borrowed := request(t, server, http.MethodPost, "/v1/clients/"+url.PathEscape(outsiderClient)+"/engagements", `{"name":"Borrowed","methodologyVersionId":"`+published.ID+`"}`, outsiderCookie, outsiderCSRF)
	if borrowed.Code != http.StatusNotFound {
		t.Fatalf("engagement selecting another organization's version status = %d, want %d: %s", borrowed.Code, http.StatusNotFound, borrowed.Body.String())
	}

	for action, want := range map[string]int{
		"methodology.template.drafted":       2,
		"methodology.template.draft.updated": 1,
		"methodology.template.published":     1,
		"engagement.checklist.snapshotted":   1,
	} {
		if count := auditCount(t, ctx, pool, organizationID, action); count != want {
			t.Fatalf("%s audit events = %d, want %d", action, count, want)
		}
	}
	var auditedVersion string
	var auditedItems int
	if err := pool.QueryRow(ctx, `SELECT context->>'templateVersionId', (context->>'itemCount')::int FROM audit_events WHERE organization_id = $1 AND action = 'engagement.checklist.snapshotted'`, organizationID).Scan(&auditedVersion, &auditedItems); err != nil {
		t.Fatalf("read snapshot audit context: %v", err)
	}
	if auditedVersion != published.ID || auditedItems != 2 {
		t.Fatalf("snapshot audit context = version %q with %d items, want %q with 2", auditedVersion, auditedItems, published.ID)
	}
}

func methodologyVersion(t *testing.T, response *httptest.ResponseRecorder, want int) methodologyVersionBody {
	t.Helper()
	if response.Code != want {
		t.Fatalf("methodology status = %d, want %d: %s", response.Code, want, response.Body.String())
	}
	var version methodologyVersionBody
	if err := json.NewDecoder(response.Body).Decode(&version); err != nil || version.ID == "" || version.TemplateID == "" {
		t.Fatalf("decode methodology version = %v, body=%s", err, response.Body.String())
	}
	return version
}

func methodologyLibrary(t *testing.T, handler http.Handler, cookie *http.Cookie) string {
	t.Helper()
	response := request(t, handler, http.MethodGet, "/v1/methodology-templates", "", cookie, "")
	if response.Code != http.StatusOK {
		t.Fatalf("methodology library status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	return response.Body.String()
}
