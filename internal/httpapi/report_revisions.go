package httpapi

import (
	"errors"
	"net/http"

	"github.com/sayseven7/frameops/internal/store/postgres"
)

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
