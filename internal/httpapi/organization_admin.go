package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/sayseven7/frameops/internal/store/postgres"
)

func (server Server) organizationAdministration(response http.ResponseWriter, request *http.Request) {
	session, ok := server.session(response, request, request.Method != http.MethodGet)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		organization, err := postgres.ReadOrganization(request.Context(), server.pool, session)
		if err != nil {
			writeOrganizationError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, organization)
	case http.MethodPut:
		var input struct {
			Name string `json:"name"`
		}
		if !decodeJSON(request, &input) {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		organization, err := postgres.UpdateOrganization(request.Context(), server.pool, session, input.Name)
		writeOrganizationError(response, err)
		if err == nil {
			writeJSON(response, http.StatusOK, organization)
		}
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) organizationMembers(response http.ResponseWriter, request *http.Request) {
	session, ok := server.session(response, request, request.Method == http.MethodPost)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		members, err := postgres.ListOrganizationMembers(request.Context(), server.pool, session)
		if err != nil {
			writeOrganizationError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": members})
	case http.MethodPost:
		var input struct {
			DisplayName string `json:"displayName"`
			Email       string `json:"email"`
			Password    string `json:"password"`
			Role        string `json:"role"`
		}
		if !decodeJSON(request, &input) {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		member, err := postgres.CreateOrganizationMember(request.Context(), server.pool, session, postgres.OrganizationMember{DisplayName: input.DisplayName, Email: input.Email, Role: input.Role}, input.Password)
		writeOrganizationError(response, err)
		if err == nil {
			writeJSON(response, http.StatusCreated, member)
		}
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) organizationMember(response http.ResponseWriter, request *http.Request) {
	userID, ok := pathID(request.URL.Path, "/v1/organization/members/", "")
	if !ok || request.Method != http.MethodPatch {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, true)
	if !ok {
		return
	}
	var input struct {
		Role     string `json:"role"`
		IsActive *bool  `json:"isActive"`
	}
	if !decodeJSON(request, &input) {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	if input.Role != "" && input.IsActive != nil {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	member, err := postgres.UpdateOrganizationMember(request.Context(), server.pool, session, userID, input.Role, input.IsActive)
	writeOrganizationError(response, err)
	if err == nil {
		writeJSON(response, http.StatusOK, member)
	}
}

func (server Server) organizationAuditEvents(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, false)
	if !ok {
		return
	}
	limit := 50
	if value := request.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		limit = parsed
	}
	action, cursor := strings.TrimSpace(request.URL.Query().Get("action")), strings.TrimSpace(request.URL.Query().Get("cursor"))
	if len(action) > 200 || len(cursor) > 36 || (cursor != "" && !identifier(cursor)) {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	events, nextCursor, err := postgres.ListAuditEvents(request.Context(), server.pool, session, action, cursor, limit)
	writeOrganizationError(response, err)
	if err == nil {
		writeJSON(response, http.StatusOK, map[string]any{"items": events, "nextCursor": nextCursor})
	}
}

func writeOrganizationError(response http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, postgres.ErrForbidden):
		writeError(response, http.StatusForbidden, "forbidden")
	case errors.Is(err, postgres.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrInvalidState):
		writeError(response, http.StatusConflict, "invalid_state")
	case err != nil && strings.Contains(err.Error(), "invalid"):
		writeError(response, http.StatusBadRequest, "invalid_request")
	default:
		writeError(response, http.StatusInternalServerError, "internal_error")
	}
}
