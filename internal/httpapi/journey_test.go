package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthenticatedJourneyWithRealDependencies(t *testing.T) {
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

	bucket := evidenceBucket(t)
	server := New(pool, bucket, documentWorker(t))
	organizationID := createOrganization(t, ctx, pool, "Journey Organization")
	cookie, csrf := signIn(t, ctx, server, pool, organizationID, "admin", "journey@example.test")

	methodology := methodologyVersion(t, request(t, server, http.MethodPost, "/v1/methodology-templates", `{"name":"Journey methodology","sourceName":"OWASP WSTG","sourceVersion":"4.2","attribution":"Structured after OWASP WSTG 4.2.","items":[{"title":"Authorization","objective":"Confirm tenant isolation","procedure":"Replay a request with another tenant","reference":"WSTG-ATHZ-02"}]}`, cookie, csrf), http.StatusCreated)
	methodology = methodologyVersion(t, request(t, server, http.MethodPost, "/v1/methodology-templates/"+url.PathEscape(methodology.TemplateID)+"/publish", "", cookie, csrf), http.StatusOK)
	if methodology.State != "published" {
		t.Fatalf("methodology state = %q, want published", methodology.State)
	}

	clientID := createdID(t, server, http.MethodPost, "/v1/clients", `{"name":"Journey Client"}`, cookie, csrf)
	engagementID := createdID(t, server, http.MethodPost, "/v1/clients/"+url.PathEscape(clientID)+"/engagements", `{"name":"Journey Engagement","methodologyVersionId":"`+methodology.ID+`"}`, cookie, csrf)
	checklistPath := "/v1/engagements/" + url.PathEscape(engagementID) + "/checklist"
	checklist := request(t, server, http.MethodGet, checklistPath, "", cookie, "")
	if checklist.Code != http.StatusOK || !strings.Contains(checklist.Body.String(), methodology.ID) || !strings.Contains(checklist.Body.String(), "Authorization") {
		t.Fatalf("checklist status = %d, body=%s", checklist.Code, checklist.Body.String())
	}

	findingID := createdID(t, server, http.MethodPost, "/v1/engagements/"+url.PathEscape(engagementID)+"/findings", `{"title":"Journey finding","description":"A reproducible issue","impact":"Tenant data exposure","remediation":"Authorize the request","reproduction":"Replay the request","cvssVector":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}`, cookie, csrf)
	evidencePath := "/v1/findings/" + url.PathEscape(findingID) + "/evidence"
	content := append([]byte("\x89PNG\r\n\x1a\n"), []byte("journey evidence")...)
	captured := captureRequest(t, server, evidencePath, cookie, csrf, "journey.png", "image/png", "", bytes.NewReader(content))
	if captured.Code != http.StatusCreated {
		t.Fatalf("capture evidence status = %d, body=%s", captured.Code, captured.Body.String())
	}
	var evidence struct {
		State      string `json:"state"`
		StorageKey string `json:"storageKey"`
		SHA256     string `json:"sha256"`
		ByteSize   int64  `json:"byteSize"`
	}
	if err := json.NewDecoder(captured.Body).Decode(&evidence); err != nil {
		t.Fatalf("decode captured evidence: %v", err)
	}
	digest := sha256.Sum256(content)
	if evidence.State != "stored" || evidence.SHA256 != hex.EncodeToString(digest[:]) || evidence.ByteSize != int64(len(content)) {
		t.Fatalf("captured evidence = %#v, want stored bytes with their digest", evidence)
	}
	storedEvidence, err := bucket.Get(ctx, evidence.StorageKey)
	if err != nil {
		t.Fatalf("read stored evidence: %v", err)
	}
	persisted, err := io.ReadAll(storedEvidence)
	if closeErr := storedEvidence.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil || !bytes.Equal(persisted, content) {
		t.Fatalf("stored evidence = %q, err=%v, want captured bytes", persisted, err)
	}

	triagePath := "/v1/findings/" + url.PathEscape(findingID) + "/triage"
	if triaged := request(t, server, http.MethodPut, triagePath, `{"validationState":"confirmed","remediationState":"open"}`, cookie, csrf); triaged.Code != http.StatusOK {
		t.Fatalf("confirm finding status = %d, body=%s", triaged.Code, triaged.Body.String())
	}
	retestsPath := "/v1/findings/" + url.PathEscape(findingID) + "/retests"
	if retest := request(t, server, http.MethodPost, retestsPath, `{"round":1,"resultState":"fixed","procedure":"Retested the patched request","observedResult":"The request is authorized","justification":"Verified against the fixed build"}`, cookie, csrf); retest.Code != http.StatusCreated || !strings.Contains(retest.Body.String(), `"resultState":"fixed"`) {
		t.Fatalf("closing retest status = %d, body=%s", retest.Code, retest.Body.String())
	}
	findings := request(t, server, http.MethodGet, "/v1/engagements/"+url.PathEscape(engagementID)+"/findings", "", cookie, "")
	if findings.Code != http.StatusOK || !strings.Contains(findings.Body.String(), `"remediationState":"fixed"`) {
		t.Fatalf("findings after retest status = %d, body=%s", findings.Code, findings.Body.String())
	}

	revisionID := uploadReportRevision(t, server, engagementID, cookie, csrf)
	revisions := request(t, server, http.MethodGet, "/v1/engagements/"+url.PathEscape(engagementID)+"/reports", "", cookie, "")
	if revisions.Code != http.StatusOK || !strings.Contains(revisions.Body.String(), revisionID) || !strings.Contains(revisions.Body.String(), `"state":"stored"`) {
		t.Fatalf("stored report revision status = %d, body=%s", revisions.Code, revisions.Body.String())
	}
	if approved := request(t, server, http.MethodPost, "/v1/report-revisions/"+url.PathEscape(revisionID)+"/approve", "", cookie, csrf); approved.Code != http.StatusOK {
		t.Fatalf("approve report revision status = %d, body=%s", approved.Code, approved.Body.String())
	}
	pdfPath := "/v1/report-revisions/" + url.PathEscape(revisionID) + "/pdf"
	pdfResponse := request(t, server, http.MethodPost, pdfPath, "", cookie, csrf)
	if pdfResponse.Code != http.StatusCreated {
		t.Fatalf("derive PDF status = %d, body=%s", pdfResponse.Code, pdfResponse.Body.String())
	}
	var pdf struct {
		State        string `json:"state"`
		StorageKey   string `json:"storageKey"`
		SourceSHA256 string `json:"sourceSha256"`
	}
	if err := json.NewDecoder(pdfResponse.Body).Decode(&pdf); err != nil {
		t.Fatalf("decode derived PDF: %v", err)
	}
	if pdf.State != "stored" || pdf.SourceSHA256 != approvedRevisionDigest(t, server, engagementID, revisionID, cookie) {
		t.Fatalf("derived PDF = %#v, want stored PDF from approved revision", pdf)
	}
	storedPDF, err := bucket.Get(ctx, pdf.StorageKey)
	if err != nil {
		t.Fatalf("read stored PDF: %v", err)
	}
	pdfBytes, err := io.ReadAll(storedPDF)
	if closeErr := storedPDF.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil || !bytes.HasPrefix(pdfBytes, []byte("%PDF-")) {
		t.Fatalf("stored PDF = %q, err=%v, want PDF bytes", pdfBytes, err)
	}

	outsiderID := createOrganization(t, ctx, pool, "Journey Outside Organization")
	outsiderCookie, outsiderCSRF := signIn(t, ctx, server, pool, outsiderID, "admin", "journey-outsider@example.test")
	for name, response := range map[string]*httptest.ResponseRecorder{
		"evidence read":    request(t, server, http.MethodGet, evidencePath, "", outsiderCookie, ""),
		"evidence capture": captureRequest(t, server, evidencePath, outsiderCookie, outsiderCSRF, "outside.png", "image/png", "", bytes.NewReader(content)),
		"checklist":        request(t, server, http.MethodGet, checklistPath, "", outsiderCookie, ""),
		"PDF derivation":   request(t, server, http.MethodPost, pdfPath, "", outsiderCookie, outsiderCSRF),
	} {
		if response.Code != http.StatusNotFound {
			t.Fatalf("cross-organization %s status = %d, want %d: %s", name, response.Code, http.StatusNotFound, response.Body.String())
		}
	}
}
