package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/store/objectstore"
)

func TestEvidenceCaptureRecordsIntegrityAndCustody(t *testing.T) {
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
	server := New(pool, bucket)
	organizationID := createOrganization(t, ctx, pool, "Evidence Organization")
	cookie, csrf := signIn(t, ctx, server, pool, organizationID, "admin", "evidence-admin@example.test")
	findingID := createFinding(t, server, cookie, csrf)
	evidencePath := "/v1/findings/" + url.PathEscape(findingID) + "/evidence"

	// A PNG signature declared as plain text: the stored media type must come
	// from the bytes, never from what the client claimed.
	content := append([]byte("\x89PNG\r\n\x1a\n"), []byte("synthetic capture bytes")...)
	digest := sha256.Sum256(content)
	const capturedAt = "2026-08-03T14:04:05Z"

	if unauthenticated := captureRequest(t, server, evidencePath, nil, "", "screenshot.png", "text/plain", capturedAt, bytes.NewReader(content)); unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated capture status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}
	if uncsrfed := captureRequest(t, server, evidencePath, cookie, "", "screenshot.png", "text/plain", capturedAt, bytes.NewReader(content)); uncsrfed.Code != http.StatusForbidden {
		t.Fatalf("capture without CSRF status = %d, want %d", uncsrfed.Code, http.StatusForbidden)
	}
	if empty := captureRequest(t, server, evidencePath, cookie, csrf, "empty.txt", "text/plain", "", bytes.NewReader(nil)); empty.Code != http.StatusBadRequest {
		t.Fatalf("empty capture status = %d, want %d: %s", empty.Code, http.StatusBadRequest, empty.Body.String())
	}
	if malformed := captureRequest(t, server, evidencePath, cookie, csrf, "screenshot.png", "text/plain", "yesterday", bytes.NewReader(content)); malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed capture instant status = %d, want %d", malformed.Code, http.StatusBadRequest)
	}
	oversized := captureRequest(t, server, evidencePath, cookie, csrf, "capture.bin", "application/octet-stream", "", io.LimitReader(filler{}, maxEvidenceBytes+1))
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized capture status = %d, want %d: %s", oversized.Code, http.StatusRequestEntityTooLarge, oversized.Body.String())
	}

	captured := captureRequest(t, server, evidencePath, cookie, csrf, "screenshot.png", "text/plain", capturedAt, bytes.NewReader(content))
	if captured.Code != http.StatusCreated {
		t.Fatalf("capture status = %d, want %d: %s", captured.Code, http.StatusCreated, captured.Body.String())
	}
	var stored struct {
		ID                string  `json:"id"`
		FindingID         string  `json:"findingId"`
		State             string  `json:"state"`
		StorageKey        string  `json:"storageKey"`
		Filename          string  `json:"filename"`
		DeclaredMediaType string  `json:"declaredMediaType"`
		DetectedMediaType string  `json:"detectedMediaType"`
		SHA256            string  `json:"sha256"`
		ByteSize          int64   `json:"byteSize"`
		CapturedAt        *string `json:"capturedAt"`
		ReceivedAt        string  `json:"receivedAt"`
		StoredAt          *string `json:"storedAt"`
	}
	if err := json.NewDecoder(captured.Body).Decode(&stored); err != nil {
		t.Fatalf("decode capture = %v", err)
	}
	switch {
	case stored.State != "stored" || stored.StoredAt == nil:
		t.Fatalf("capture state = %q stored at %v, want stored with an instant", stored.State, stored.StoredAt)
	case stored.SHA256 != hex.EncodeToString(digest[:]):
		t.Fatalf("capture digest = %q, want %q", stored.SHA256, hex.EncodeToString(digest[:]))
	case stored.ByteSize != int64(len(content)):
		t.Fatalf("capture size = %d, want %d", stored.ByteSize, len(content))
	case stored.DetectedMediaType != "image/png":
		t.Fatalf("detected media type = %q, want the type of the bytes themselves", stored.DetectedMediaType)
	case stored.DeclaredMediaType != "text/plain":
		t.Fatalf("declared media type = %q, want the untrusted client claim preserved", stored.DeclaredMediaType)
	case stored.CapturedAt == nil || stored.ReceivedAt == "" || *stored.CapturedAt == stored.ReceivedAt:
		t.Fatalf("captured at %v and received at %q must both exist and stay distinct", stored.CapturedAt, stored.ReceivedAt)
	case stored.StorageKey != "organizations/"+organizationID+"/engagements/"+engagementOf(t, ctx, pool, findingID)+"/evidence/"+stored.ID:
		t.Fatalf("storage key = %q, want a key derived from the owning identifiers", stored.StorageKey)
	}

	// The object store holds exactly the bytes the digest describes: detection
	// read the buffered copy and never consumed the persisted stream.
	object, err := bucket.Get(ctx, stored.StorageKey)
	if err != nil {
		t.Fatalf("read stored evidence object: %v", err)
	}
	persisted, err := io.ReadAll(object)
	if err != nil {
		t.Fatalf("read stored evidence bytes: %v", err)
	}
	if err := object.Close(); err != nil {
		t.Fatalf("close stored evidence object: %v", err)
	}
	if !bytes.Equal(persisted, content) {
		t.Fatalf("stored evidence is %d bytes, want the %d captured bytes unchanged", len(persisted), len(content))
	}

	// PostgreSQL and the object store share no transaction. A capture whose
	// upload fails keeps its custody metadata in the explicit intermediate
	// state instead of vanishing or being reported as stored evidence.
	unreachable, err := objectstore.New(os.Getenv("FRAMEOPS_EVIDENCE_S3_ENDPOINT"), "frameops-absent-evidence-bucket", os.Getenv("FRAMEOPS_EVIDENCE_S3_REGION"), os.Getenv("FRAMEOPS_EVIDENCE_S3_ACCESS_KEY"), os.Getenv("FRAMEOPS_EVIDENCE_S3_SECRET_KEY"))
	if err != nil {
		t.Fatalf("build unreachable evidence bucket: %v", err)
	}
	interrupted := captureRequest(t, New(pool, unreachable), evidencePath, cookie, csrf, "interrupted.txt", "text/plain", "", strings.NewReader("interrupted capture"))
	if interrupted.Code != http.StatusInternalServerError {
		t.Fatalf("interrupted capture status = %d, want %d: %s", interrupted.Code, http.StatusInternalServerError, interrupted.Body.String())
	}

	listed := request(t, server, http.MethodGet, evidencePath, "", cookie, "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list evidence status = %d, want %d: %s", listed.Code, http.StatusOK, listed.Body.String())
	}
	var chain struct {
		Items []struct {
			Filename string  `json:"filename"`
			State    string  `json:"state"`
			SHA256   string  `json:"sha256"`
			StoredAt *string `json:"storedAt"`
		} `json:"items"`
	}
	if err := json.NewDecoder(listed.Body).Decode(&chain); err != nil {
		t.Fatalf("decode evidence chain = %v", err)
	}
	if len(chain.Items) != 2 {
		t.Fatalf("evidence chain = %#v, want the stored capture and the pending one", chain.Items)
	}
	if chain.Items[0].Filename != "screenshot.png" || chain.Items[0].State != "stored" || chain.Items[0].StoredAt == nil {
		t.Fatalf("first chain entry = %#v, want the stored screenshot", chain.Items[0])
	}
	if chain.Items[1].Filename != "interrupted.txt" || chain.Items[1].State != "pending" || chain.Items[1].StoredAt != nil {
		t.Fatalf("second chain entry = %#v, want an unconfirmed pending capture", chain.Items[1])
	}

	outsiderID := createOrganization(t, ctx, pool, "Evidence Outside Organization")
	outsiderCookie, outsiderCSRF := signIn(t, ctx, server, pool, outsiderID, "admin", "evidence-outsider@example.test")
	if outsider := captureRequest(t, server, evidencePath, outsiderCookie, outsiderCSRF, "screenshot.png", "image/png", "", bytes.NewReader(content)); outsider.Code != http.StatusNotFound {
		t.Fatalf("cross-organization capture status = %d, want %d: %s", outsider.Code, http.StatusNotFound, outsider.Body.String())
	}
	if outsider := request(t, server, http.MethodGet, evidencePath, "", outsiderCookie, ""); outsider.Code != http.StatusNotFound {
		t.Fatalf("cross-organization evidence chain status = %d, want %d", outsider.Code, http.StatusNotFound)
	}

	if count := auditCount(t, ctx, pool, organizationID, "evidence.capture.reserved"); count != 2 {
		t.Fatalf("reserved evidence audit events = %d, want 2", count)
	}
	if count := auditCount(t, ctx, pool, organizationID, "evidence.capture.stored"); count != 1 {
		t.Fatalf("stored evidence audit events = %d, want 1", count)
	}
	var auditedDigest string
	var auditedSize int64
	if err := pool.QueryRow(ctx, `SELECT context->>'sha256', (context->>'byteSize')::bigint FROM audit_events WHERE organization_id = $1 AND action = 'evidence.capture.stored'`, organizationID).Scan(&auditedDigest, &auditedSize); err != nil {
		t.Fatalf("read stored evidence audit context: %v", err)
	}
	if auditedDigest != stored.SHA256 || auditedSize != stored.ByteSize {
		t.Fatalf("audited custody = %q/%d, want %q/%d", auditedDigest, auditedSize, stored.SHA256, stored.ByteSize)
	}
	var nothingRecorded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM evidence WHERE organization_id = $1 AND filename IN ('empty.txt', 'capture.bin')`, organizationID).Scan(&nothingRecorded); err != nil {
		t.Fatalf("count refused captures: %v", err)
	}
	if nothingRecorded != 0 {
		t.Fatalf("refused captures recorded %d rows, want none persisted", nothingRecorded)
	}
}

// evidenceBucket builds the evidence bucket the API requires. The API has no
// degraded mode without object storage, so its integration tests need one too.
func evidenceBucket(t *testing.T) objectstore.Bucket {
	t.Helper()
	bucket, err := objectstore.FromEnv()
	if err != nil {
		t.Skipf("evidence object storage is required for HTTP integration tests: %v", err)
	}
	if err := bucket.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("ensure evidence bucket: %v", err)
	}
	return bucket
}

func createOrganization(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var organizationID string
	if err := pool.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ($1) RETURNING id`, name).Scan(&organizationID); err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return organizationID
}

// createFinding walks the API from client to finding so the evidence under test
// hangs from records the same authenticated session owns.
func createFinding(t *testing.T, handler http.Handler, cookie *http.Cookie, csrf string) string {
	t.Helper()
	client := createdID(t, handler, http.MethodPost, "/v1/clients", `{"name":"Evidence Client"}`, cookie, csrf)
	engagement := createdID(t, handler, http.MethodPost, "/v1/clients/"+url.PathEscape(client)+"/engagements", `{"name":"Evidence Engagement"}`, cookie, csrf)
	return createdID(t, handler, http.MethodPost, "/v1/engagements/"+url.PathEscape(engagement)+"/findings",
		`{"title":"Reflected XSS","cvssVector":"CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N"}`, cookie, csrf)
}

func createdID(t *testing.T, handler http.Handler, method, path, body string, cookie *http.Cookie, csrf string) string {
	t.Helper()
	response := request(t, handler, method, path, body, cookie, csrf)
	if response.Code != http.StatusCreated {
		t.Fatalf("%s %s status = %d, body=%s", method, path, response.Code, response.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil || created.ID == "" {
		t.Fatalf("decode %s = %v, body=%s", path, err, response.Body.String())
	}
	return created.ID
}

func engagementOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, findingID string) string {
	t.Helper()
	var engagementID string
	if err := pool.QueryRow(ctx, `SELECT engagement_id FROM findings WHERE id = $1`, findingID).Scan(&engagementID); err != nil {
		t.Fatalf("read finding engagement: %v", err)
	}
	return engagementID
}

// captureRequest streams one multipart capture so an upload larger than the
// accepted size is refused at the stream rather than buffered by the test.
func captureRequest(t *testing.T, handler http.Handler, path string, cookie *http.Cookie, csrf, filename, declared, capturedAt string, content io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	go func() {
		_ = pipeWriter.CloseWithError(func() error {
			if capturedAt != "" {
				if err := writer.WriteField("capturedAt", capturedAt); err != nil {
					return err
				}
			}
			header := textproto.MIMEHeader{}
			header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
			header.Set("Content-Type", declared)
			part, err := writer.CreatePart(header)
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, content); err != nil {
				return err
			}
			return writer.Close()
		}())
	}()
	defer func() { _ = pipeReader.Close() }()

	req := httptest.NewRequest(http.MethodPost, path, pipeReader)
	req.Header.Set("Content-Type", writer.FormDataContentType())
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

// filler produces bytes without allocating a whole oversized capture.
type filler struct{}

func (filler) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 'e'
	}
	return len(buffer), nil
}
