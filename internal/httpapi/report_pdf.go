package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sayseven7/frameops/internal/store/postgres"
)

// reportPDF derives the deliverable PDF of one report revision. There is a
// single content pipeline: the bytes converted here are read back from the
// object store, checked against the digest recorded when the revision was
// approved, and handed to an isolated worker. Nothing is rendered from project
// data, so changing a finding after approval cannot change the delivered PDF.
func (server Server) reportPDF(response http.ResponseWriter, request *http.Request) {
	revisionID, ok := pathID(request.URL.Path, "/v1/report-revisions/", "/pdf")
	if !ok || request.Method != http.MethodPost {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, true)
	if !ok {
		return
	}
	revision, err := postgres.ApprovedReportRevision(request.Context(), server.pool, session, revisionID)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, postgres.ErrInvalidState):
		writeError(response, http.StatusConflict, "invalid_state")
		return
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}

	workspace, err := os.MkdirTemp("", "frameops-pdf-")
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	defer os.RemoveAll(workspace) //nolint:errcheck
	source := filepath.Join(workspace, "approved.docx")
	if err := server.readApprovedDOCX(request.Context(), revision, source); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	// A conversion that fails produces no row and no object: an unconverted
	// approval is never recorded as a delivered PDF.
	converted, err := server.renderer.Convert(request.Context(), source, filepath.Join(workspace, "approved.pdf"))
	if err != nil {
		writeError(response, http.StatusInternalServerError, "conversion_failed")
		return
	}

	pdf, err := postgres.ReserveReportPDF(request.Context(), server.pool, session, revision.ID, converted.Converter, converted.SHA256, converted.ByteSize)
	if errors.Is(err, postgres.ErrInvalidState) {
		writeError(response, http.StatusConflict, "invalid_state")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	file, err := os.Open(filepath.Join(workspace, "approved.pdf"))
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	defer file.Close() //nolint:errcheck
	if err := server.evidence.Put(request.Context(), pdf.StorageKey, file, pdf.ByteSize, pdf.SHA256, "application/pdf"); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	pdf, err = postgres.ConfirmReportPDF(request.Context(), server.pool, session, pdf.ID)
	if errors.Is(err, postgres.ErrInvalidState) {
		writeError(response, http.StatusConflict, "invalid_state")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(response, http.StatusCreated, pdf)
}

// readApprovedDOCX writes the stored revision to a local file and refuses to
// continue unless the bytes it read are exactly the approved ones. The digest is
// recomputed over what was actually written, so a truncated or altered download
// cannot become the source of a delivered PDF.
func (server Server) readApprovedDOCX(ctx context.Context, revision postgres.ReportRevision, destination string) error {
	stored, err := server.evidence.Get(ctx, revision.StorageKey)
	if err != nil {
		return err
	}
	defer stored.Close() //nolint:errcheck
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck
	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(stored, maxReportBytes+1))
	if err != nil {
		return err
	}
	if size != revision.ByteSize || hex.EncodeToString(digest.Sum(nil)) != revision.SHA256 {
		return errors.New("the stored report revision does not match its approved digest")
	}
	return file.Close()
}
