package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/render"
)

// TestReportPDFIsDerivedOnlyFromTheApprovedRevision exercises the whole delivery
// path with the real dependencies: the revision is imported over HTTP, stored in
// the object store, approved, and converted by the isolated worker running a
// real converter. It then reads the delivered PDF back from the object store and
// checks that its provenance names the exact approved DOCX.
func TestReportPDFIsDerivedOnlyFromTheApprovedRevision(t *testing.T) {
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FRAMEOPS_DATABASE_URL is required for HTTP integration tests")
	}
	renderer := documentWorker(t)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	bucket := evidenceBucket(t)
	server := New(pool, bucket, renderer)
	organizationID := createOrganization(t, ctx, pool, "Report PDF Organization")
	cookie, csrf := signIn(t, ctx, server, pool, organizationID, "admin", "report-pdf@example.test")
	engagementID := reportEngagement(t, server, cookie, csrf, "Report PDF Engagement")
	revisionID := uploadReportRevision(t, server, engagementID, cookie, csrf)

	pdfPath := func(id string) string { return "/v1/report-revisions/" + url.PathEscape(id) + "/pdf" }
	for name, path := range map[string]string{
		"malformed": pdfPath("not-a-uuid"),
		"missing":   pdfPath("00000000-0000-0000-0000-000000000000"),
	} {
		if response := request(t, server, http.MethodPost, path, "", cookie, csrf); response.Code != http.StatusNotFound {
			t.Fatalf("%s conversion status = %d, want %d: %s", name, response.Code, http.StatusNotFound, response.Body.String())
		}
	}
	// A stored revision nobody approved is not a deliverable.
	if response := request(t, server, http.MethodPost, pdfPath(revisionID), "", cookie, csrf); response.Code != http.StatusConflict {
		t.Fatalf("unapproved conversion status = %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}

	if response := request(t, server, http.MethodPost, "/v1/report-revisions/"+url.PathEscape(revisionID)+"/approve", "", cookie, csrf); response.Code != http.StatusOK {
		t.Fatalf("approval status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	outsiderID := createOrganization(t, ctx, pool, "Report PDF Outside Organization")
	outsiderCookie, outsiderCSRF := signIn(t, ctx, server, pool, outsiderID, "admin", "report-pdf-outsider@example.test")
	if response := request(t, server, http.MethodPost, pdfPath(revisionID), "", outsiderCookie, outsiderCSRF); response.Code != http.StatusNotFound {
		t.Fatalf("cross-organization conversion status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
	}

	response := request(t, server, http.MethodPost, pdfPath(revisionID), "", cookie, csrf)
	if response.Code != http.StatusCreated {
		t.Fatalf("conversion status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	var pdf struct {
		RevisionID   string `json:"revisionId"`
		State        string `json:"state"`
		StorageKey   string `json:"storageKey"`
		SourceSHA256 string `json:"sourceSha256"`
		Converter    string `json:"converter"`
		SHA256       string `json:"sha256"`
		ByteSize     int64  `json:"byteSize"`
	}
	if err := json.NewDecoder(response.Body).Decode(&pdf); err != nil {
		t.Fatalf("decode delivered pdf: %v", err)
	}
	if pdf.RevisionID != revisionID || pdf.State != "stored" || pdf.ByteSize <= 0 || strings.TrimSpace(pdf.Converter) == "" {
		t.Fatalf("delivered pdf = %+v, want a stored conversion of revision %s with a converter", pdf, revisionID)
	}
	if approved := approvedRevisionDigest(t, server, engagementID, revisionID, cookie); pdf.SourceSHA256 != approved {
		t.Fatalf("pdf provenance digest = %q, want the approved revision digest %q", pdf.SourceSHA256, approved)
	}

	stored, err := bucket.Get(ctx, pdf.StorageKey)
	if err != nil {
		t.Fatalf("read delivered pdf: %v", err)
	}
	defer stored.Close() //nolint:errcheck
	bytes, err := io.ReadAll(io.LimitReader(stored, pdf.ByteSize+1))
	if err != nil {
		t.Fatalf("read delivered pdf bytes: %v", err)
	}
	digest := sha256.Sum256(bytes)
	if int64(len(bytes)) != pdf.ByteSize || hex.EncodeToString(digest[:]) != pdf.SHA256 {
		t.Fatalf("delivered pdf bytes = %d with digest %s, want %d with digest %s", len(bytes), hex.EncodeToString(digest[:]), pdf.ByteSize, pdf.SHA256)
	}
	if !strings.HasPrefix(string(bytes), "%PDF-") {
		t.Fatalf("delivered pdf does not start with the PDF magic bytes")
	}

	// One approved revision delivers one PDF; a replay is a conflict, never a
	// second deliverable for the same approval.
	if replay := request(t, server, http.MethodPost, pdfPath(revisionID), "", cookie, csrf); replay.Code != http.StatusConflict {
		t.Fatalf("replayed conversion status = %d, want %d: %s", replay.Code, http.StatusConflict, replay.Body.String())
	}
}

// TestDocumentWorkerRefusesToSeeCredentials proves the isolation the product
// requires from the outside: the worker binary stops before converting anything
// when its environment carries a database URL or an object-storage key.
func TestDocumentWorkerRefusesToSeeCredentials(t *testing.T) {
	worker := buildDocumentWorker(t)
	workspace := t.TempDir()
	command := exec.Command(worker, "--source", filepath.Join(workspace, "absent.docx"), "--destination", filepath.Join(workspace, "absent.pdf"))
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + workspace, "FRAMEOPS_DATABASE_URL=postgres://127.0.0.1:5432/frameops"}
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("the document worker accepted a database URL in its environment: %s", output)
	}
	if !strings.Contains(string(output), "FRAMEOPS_DATABASE_URL") {
		t.Fatalf("worker refusal = %q, want it to name the variable it refused", output)
	}
}

func documentWorker(t *testing.T) render.Worker {
	t.Helper()
	for _, program := range []string{"soffice", "unshare"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skipf("%s is required for the isolated document worker: %v", program, err)
		}
	}
	worker, err := render.New(buildDocumentWorker(t))
	if err != nil {
		t.Fatalf("configure document worker: %v", err)
	}
	return worker
}

func buildDocumentWorker(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "frameops-render")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/frameops-render")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build document worker: %v: %s", err, output)
	}
	return binary
}

// approvedRevisionDigest reads back the digest the API recorded for the approved
// revision, so the provenance assertion compares against the API's own record
// rather than a digest the test recomputed.
func approvedRevisionDigest(t *testing.T, handler http.Handler, engagementID, revisionID string, cookie *http.Cookie) string {
	t.Helper()
	response := request(t, handler, http.MethodGet, "/v1/engagements/"+url.PathEscape(engagementID)+"/reports", "", cookie, "")
	if response.Code != http.StatusOK {
		t.Fatalf("list revisions status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var listed struct {
		Items []struct {
			ID         string  `json:"id"`
			SHA256     string  `json:"sha256"`
			ApprovedAt *string `json:"approvedAt"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatalf("decode listed revisions: %v", err)
	}
	for _, item := range listed.Items {
		if item.ID == revisionID {
			if item.ApprovedAt == nil {
				t.Fatalf("revision %s is not approved", revisionID)
			}
			return item.SHA256
		}
	}
	t.Fatalf("revision %s is absent from the engagement report list", revisionID)
	return ""
}
