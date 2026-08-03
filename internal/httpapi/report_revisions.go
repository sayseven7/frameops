package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sayseven7/frameops/internal/store/postgres"
)

const (
	maxReportBytes = 32 << 20
)

var errReportTooLarge = errors.New("report exceeds the accepted size")

type reportUpload struct {
	file     *os.File
	filename string
	sha256   string
	byteSize int64
}

func (server Server) reportRevisions(response http.ResponseWriter, request *http.Request) {
	engagementID, ok := pathID(request.URL.Path, "/v1/engagements/", "/reports")
	if !ok {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, request.Method == http.MethodPost)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		revisions, err := postgres.ListReportRevisions(request.Context(), server.pool, session, engagementID)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": revisions})
	case http.MethodPost:
		server.importReportRevision(response, request, session, engagementID)
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) importReportRevision(response http.ResponseWriter, request *http.Request, session postgres.Session, engagementID string) {
	upload, err := readReportUpload(request)
	if upload.file != nil {
		defer func() { _ = upload.file.Close(); _ = os.Remove(upload.file.Name()) }()
	}
	if errors.Is(err, errReportTooLarge) {
		writeError(response, http.StatusRequestEntityTooLarge, "report_too_large")
		return
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	revision, err := postgres.ReserveReportRevision(request.Context(), server.pool, session, engagementID, upload.filename, upload.sha256, upload.byteSize)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	if err := server.evidence.Put(request.Context(), revision.StorageKey, upload.file, upload.byteSize, upload.sha256, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	revision, err = postgres.ConfirmReportRevision(request.Context(), server.pool, session, revision.ID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(response, http.StatusCreated, revision)
}

func (server Server) approveReportRevision(response http.ResponseWriter, request *http.Request) {
	revisionID, ok := pathID(request.URL.Path, "/v1/report-revisions/", "/approve")
	if !ok || request.Method != http.MethodPost {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, true)
	if !ok {
		return
	}
	revision, err := postgres.ApproveReportRevision(request.Context(), server.pool, session, revisionID)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrInvalidState):
		writeError(response, http.StatusConflict, "invalid_state")
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error")
	default:
		writeJSON(response, http.StatusOK, revision)
	}
}

func readReportUpload(request *http.Request) (reportUpload, error) {
	parts, err := request.MultipartReader()
	if err != nil {
		return reportUpload{}, err
	}
	part, err := parts.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		return reportUpload{}, errors.New("a report requires one file part")
	}
	defer part.Close() //nolint:errcheck
	filename := strings.TrimSpace(part.FileName())
	if !plainMetadata(filename) || strings.ContainsAny(filename, `/\\`) || filename == "." || filename == ".." {
		return reportUpload{}, errors.New("a report requires a plain file name")
	}
	file, err := os.CreateTemp("", "frameops-report-")
	if err != nil {
		return reportUpload{}, err
	}
	upload := reportUpload{file: file, filename: filename}
	failed := true
	defer func() {
		if failed {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}()
	digest := sha256.New()
	size, err := io.Copy(file, io.TeeReader(io.LimitReader(part, maxReportBytes+1), digest))
	if err != nil {
		return reportUpload{}, err
	}
	if size > maxReportBytes {
		return reportUpload{}, errReportTooLarge
	}
	if size == 0 {
		return reportUpload{}, errors.New("a report carries at least one byte")
	}
	if next, nextErr := parts.NextPart(); !errors.Is(nextErr, io.EOF) {
		if next != nil {
			_ = next.Close()
		}
		return reportUpload{}, errors.New("a report carries exactly one file part")
	}
	if err := validateDOCX(file, size); err != nil {
		return reportUpload{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return reportUpload{}, err
	}
	upload.sha256, upload.byteSize, failed = hex.EncodeToString(digest.Sum(nil)), size, false
	return upload, nil
}
