package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sayseven7/frameops/internal/store/postgres"
)

func TestMultipartUploadsRejectBodiesOverGlobalLimit(t *testing.T) {
	tests := []struct {
		name       string
		limit      int64
		read       func(http.ResponseWriter, *http.Request) error
		handle     func(http.ResponseWriter, *http.Request)
		wantStatus int
		wantCode   string
	}{
		{
			name:       "evidence",
			limit:      maxEvidenceBytes,
			read:       func(w http.ResponseWriter, r *http.Request) error { _, err := readEvidenceUpload(w, r); return err },
			handle:     func(w http.ResponseWriter, r *http.Request) { (Server{}).captureEvidence(w, r, postgres.Session{}, "") },
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   "evidence_too_large",
		},
		{
			name:       "ingestion",
			limit:      maxIngestionBytes,
			read:       func(w http.ResponseWriter, r *http.Request) error { _, err := readIngestionUpload(w, r); return err },
			handle:     func(w http.ResponseWriter, r *http.Request) { (Server{}).recordIngestion(w, r, postgres.Session{}, "") },
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   "artifact_too_large",
		},
		{
			name:  "report",
			limit: maxReportBytes,
			read:  func(w http.ResponseWriter, r *http.Request) error { _, err := readReportUpload(w, r); return err },
			handle: func(w http.ResponseWriter, r *http.Request) {
				(Server{}).importReportRevision(w, r, postgres.Session{}, "")
			},
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   "report_too_large",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, body := range []struct {
				name          string
				contentLength int64
			}{
				{"declared", test.limit + multipartOverheadBytes + 1},
				{"extra hostile part", -1},
				{"truncated over limit", -1},
			} {
				t.Run(body.name, func(t *testing.T) {
					request := oversizedMultipartRequest(t, test.limit, body.contentLength, body.name)
					if err := test.read(httptest.NewRecorder(), request); !errors.Is(err, errMultipartBodyTooLarge) {
						t.Fatalf("read error = %v, want %v", err, errMultipartBodyTooLarge)
					}

					response := httptest.NewRecorder()
					test.handle(response, oversizedMultipartRequest(t, test.limit, body.contentLength, body.name))
					if response.Code != test.wantStatus || !bytes.Contains(response.Body.Bytes(), []byte(test.wantCode)) {
						t.Fatalf("response = %d %s, want %d %q", response.Code, response.Body.String(), test.wantStatus, test.wantCode)
					}
				})
			}
		})
	}
}

func TestMultipartUploadsKeepMalformedBodiesWithinLimitInvalid(t *testing.T) {
	for name, test := range map[string]struct {
		read   func(http.ResponseWriter, *http.Request) error
		handle func(http.ResponseWriter, *http.Request)
	}{
		"evidence":  {func(w http.ResponseWriter, r *http.Request) error { _, err := readEvidenceUpload(w, r); return err }, func(w http.ResponseWriter, r *http.Request) { (Server{}).captureEvidence(w, r, postgres.Session{}, "") }},
		"ingestion": {func(w http.ResponseWriter, r *http.Request) error { _, err := readIngestionUpload(w, r); return err }, func(w http.ResponseWriter, r *http.Request) { (Server{}).recordIngestion(w, r, postgres.Session{}, "") }},
		"report": {func(w http.ResponseWriter, r *http.Request) error { _, err := readReportUpload(w, r); return err }, func(w http.ResponseWriter, r *http.Request) {
			(Server{}).importReportRevision(w, r, postgres.Session{}, "")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not multipart"))
			request.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
			if err := test.read(httptest.NewRecorder(), request); err == nil || errors.Is(err, errMultipartBodyTooLarge) {
				t.Fatalf("read error = %v, want non-size malformed error", err)
			}

			response := httptest.NewRecorder()
			request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not multipart"))
			request.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
			test.handle(response, request)
			if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("invalid_request")) {
				t.Fatalf("response = %d %s, want 400 invalid_request", response.Code, response.Body.String())
			}
		})
	}
}

func oversizedMultipartRequest(t *testing.T, artifactLimit, contentLength int64, kind string) *http.Request {
	t.Helper()
	boundary := "frameops"
	header := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"upload.bin\"\r\n\r\n"
	if kind == "extra hostile part" {
		header = "--" + boundary + "\r\nContent-Disposition: form-data; name=\"unexpected\"\r\n\r\n"
	}
	body := io.MultiReader(strings.NewReader(header), io.LimitReader(filler{}, artifactLimit+multipartOverheadBytes+1))
	request := httptest.NewRequest(http.MethodPost, "/", body)
	request.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	request.ContentLength = contentLength
	return request
}
