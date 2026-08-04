package objectstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEnsureBucketRejectsMissingRetention(t *testing.T) {
	bucket, err := New("http://127.0.0.1:9000", "frameops-evidence", "us-east-1", "test-access", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := bucket.EnsureBucket(context.Background()); err == nil {
		t.Fatal("EnsureBucket accepted an unspecified retention period")
	}
}

func TestEnsureBucketConfiguresAndVerifiesComplianceRetention(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		switch len(requests) {
		case 1:
			if request.Method != http.MethodPut || request.Header.Get("X-Amz-Bucket-Object-Lock-Enabled") != "true" {
				t.Fatalf("bucket creation = %s with object lock %q", request.Method, request.Header.Get("X-Amz-Bucket-Object-Lock-Enabled"))
			}
			response.WriteHeader(http.StatusConflict)
		case 2:
			if request.Method != http.MethodPut || request.URL.Query().Get("object-lock") != "" || !strings.Contains(request.Header.Get("Content-Type"), "application/xml") {
				t.Fatalf("retention configuration request = %s %s", request.Method, request.URL.RequestURI())
			}
			response.WriteHeader(http.StatusOK)
		case 3:
			response.Header().Set("Content-Type", "application/xml")
			_, _ = response.Write([]byte(`<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>COMPLIANCE</Mode><Days>30</Days></DefaultRetention></Rule></ObjectLockConfiguration>`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.RequestURI())
		}
	}))
	defer server.Close()

	bucket, err := New(server.URL, "frameops-evidence", "us-east-1", "test-access", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	bucket.retentionDays = 30
	if err := bucket.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket = %v", err)
	}
	if got, want := strings.Join(requests, ", "), "PUT /frameops-evidence, PUT /frameops-evidence?object-lock=, GET /frameops-evidence?object-lock="; got != want {
		t.Fatalf("requests = %q, want %q", got, want)
	}
}

func TestPutUsesComplianceRetention(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("If-None-Match") != "*" || request.Header.Get("X-Amz-Object-Lock-Mode") != "COMPLIANCE" || request.Header.Get("X-Amz-Object-Lock-Retain-Until-Date") == "" {
			t.Fatalf("immutable put headers missing: if-none-match=%q mode=%q until=%q", request.Header.Get("If-None-Match"), request.Header.Get("X-Amz-Object-Lock-Mode"), request.Header.Get("X-Amz-Object-Lock-Retain-Until-Date"))
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	bucket, err := New(server.URL, "frameops-evidence", "us-east-1", "test-access", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	bucket.retentionDays = 30
	if err := bucket.Put(context.Background(), "organizations/a/engagements/b/reports/c", strings.NewReader("docx"), 4, "139d544b821b13ebea14f1b0fe18577222e415c2966e3a351d3c3f73c40d1f5e", "application/test"); err != nil {
		t.Fatalf("Put = %v", err)
	}
}

// TestMinIOObjectLockProof executes only when explicitly requested against the
// development MinIO identity. It leaves its protected test object in place: a
// successful cleanup would contradict the property being demonstrated.
func TestMinIOObjectLockProof(t *testing.T) {
	if os.Getenv("FRAMEOPS_OBJECT_LOCK_PROOF") != "1" {
		t.Skip("set FRAMEOPS_OBJECT_LOCK_PROOF=1 to prove MinIO Object Lock")
	}
	retentionDays, err := strconv.Atoi(os.Getenv("FRAMEOPS_OBJECT_RETENTION_DAYS"))
	if err != nil || retentionDays < 1 {
		t.Fatal("FRAMEOPS_OBJECT_RETENTION_DAYS must be a positive whole number")
	}
	bucket, err := New(os.Getenv("FRAMEOPS_EVIDENCE_S3_ENDPOINT"), "frameops-object-lock-proof-"+strconv.FormatInt(time.Now().UnixNano(), 36), os.Getenv("FRAMEOPS_EVIDENCE_S3_REGION"), os.Getenv("FRAMEOPS_EVIDENCE_S3_ACCESS_KEY"), os.Getenv("FRAMEOPS_EVIDENCE_S3_SECRET_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	bucket.retentionDays = retentionDays
	if err := bucket.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("ensure locked bucket: %v", err)
	}
	const key = "organizations/proof/engagements/proof/reports/proof"
	const payload = "proof"
	const digest = "c1cda26362828b69266512052b97cb3729e3b052e4ade47c0a1e3383defe73c7"
	if err := bucket.Put(context.Background(), key, strings.NewReader(payload), int64(len(payload)), digest, "application/test"); err != nil {
		t.Fatalf("put protected object: %v", err)
	}
	response, err := bucket.send(context.Background(), http.MethodGet, key, "", nil, 0, emptyPayloadHash, nil)
	if err != nil {
		t.Fatalf("get protected object version: %v", err)
	}
	versionID := response.Header.Get("X-Amz-Version-Id")
	if response.StatusCode != http.StatusOK || versionID == "" {
		drain(response)
		t.Fatalf("get protected object = %s with version ID %q", response.Status, versionID)
	}
	drain(response)
	if err := bucket.Put(context.Background(), key, strings.NewReader(payload), int64(len(payload)), digest, "application/test"); err == nil {
		t.Fatal("overwrite was accepted")
	}
	response, err = bucket.send(context.Background(), http.MethodDelete, key, "versionId="+url.QueryEscape(versionID), nil, 0, emptyPayloadHash, nil)
	if err != nil {
		t.Fatalf("delete protected object version request: %v", err)
	}
	if response.StatusCode < http.StatusBadRequest {
		drain(response)
		t.Fatalf("delete protected object version status = %s, want refusal", response.Status)
	}
	drain(response)
	response, err = bucket.send(context.Background(), http.MethodDelete, key, "", nil, 0, emptyPayloadHash, nil)
	if err != nil {
		t.Fatalf("delete protected object request: %v", err)
	}
	if response.StatusCode != http.StatusNoContent || response.Header.Get("X-Amz-Delete-Marker") != "true" {
		drain(response)
		t.Fatalf("delete protected object = %s with delete marker %q, want 204 with delete marker", response.Status, response.Header.Get("X-Amz-Delete-Marker"))
	}
	drain(response)
	response, err = bucket.send(context.Background(), http.MethodGet, key, "versionId="+url.QueryEscape(versionID), nil, 0, emptyPayloadHash, nil)
	if err != nil {
		t.Fatalf("get protected object version after delete marker: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		drain(response)
		t.Fatalf("get protected object version after delete marker = %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(len(payload))+1))
	drain(response)
	if err != nil {
		t.Fatalf("read protected object version after delete marker: %v", err)
	}
	if string(body) != payload {
		t.Fatalf("protected object version body = %q, want %q", body, payload)
	}
}
