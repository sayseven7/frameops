package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/sayseven7/frameops/internal/store/postgres"
)

// The supported resource-limit matrix is still an open product decision, so one
// submitted version is bounded conservatively: by the number of items it may
// carry and by the length of each field. The shared request-body limit bounds
// the whole document independently.
const (
	maxMethodologyItems      = 256
	maxMethodologyLabelBytes = 200
	maxMethodologyTextBytes  = 2000
)

// methodologyDraft is one submitted version. Item positions are absent by
// design: the database numbers items from the order they appear here.
type methodologyDraft struct {
	Name          string                 `json:"name"`
	SourceName    string                 `json:"sourceName"`
	SourceVersion string                 `json:"sourceVersion"`
	Attribution   string                 `json:"attribution"`
	Items         []methodologyDraftItem `json:"items"`
}

type methodologyDraftItem struct {
	Title            string `json:"title"`
	Objective        string `json:"objective"`
	Preconditions    string `json:"preconditions"`
	Procedure        string `json:"procedure"`
	ExpectedEvidence string `json:"expectedEvidence"`
	Reference        string `json:"reference"`
	Notes            string `json:"notes"`
}

// methodologyTemplates reads the organization's template library and drafts one
// new template. Every member may draft; publication is a separate, restricted
// step.
func (server Server) methodologyTemplates(response http.ResponseWriter, request *http.Request) {
	session, ok := server.session(response, request, request.Method == http.MethodPost)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := postgres.ListMethodologyVersions(request.Context(), server.pool, session)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		version, ok := decodeMethodologyDraft(response, request)
		if !ok {
			return
		}
		drafted, err := postgres.DraftMethodologyTemplate(request.Context(), server.pool, session, version)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusCreated, drafted)
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

// methodologyDraftContent replaces the whole content of a template's draft.
// Only the author of the draft may edit it; a published version is refused
// because the organization may already be testing against a copy of it.
func (server Server) methodologyDraftContent(response http.ResponseWriter, request *http.Request) {
	templateID, ok := pathID(request.URL.Path, "/v1/methodology-templates/", "/draft")
	if !ok || request.Method != http.MethodPut {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, true)
	if !ok {
		return
	}
	version, ok := decodeMethodologyDraft(response, request)
	if !ok {
		return
	}
	updated, err := postgres.UpdateMethodologyDraft(request.Context(), server.pool, session, templateID, version)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrInvalidState):
		writeError(response, http.StatusConflict, "invalid_state")
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error")
	default:
		writeJSON(response, http.StatusOK, updated)
	}
}

// methodologyPublication publishes a template's draft into the organization's
// shared library, where it becomes immutable.
func (server Server) methodologyPublication(response http.ResponseWriter, request *http.Request) {
	templateID, ok := pathID(request.URL.Path, "/v1/methodology-templates/", "/publish")
	if !ok || request.Method != http.MethodPost {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, true)
	if !ok {
		return
	}
	published, err := postgres.PublishMethodologyVersion(request.Context(), server.pool, session, templateID)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrForbidden):
		writeError(response, http.StatusForbidden, "forbidden")
	case errors.Is(err, postgres.ErrInvalidState):
		writeError(response, http.StatusConflict, "invalid_state")
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error")
	default:
		writeJSON(response, http.StatusOK, published)
	}
}

// engagementChecklist reads the immutable copy the engagement received when it
// was created.
func (server Server) engagementChecklist(response http.ResponseWriter, request *http.Request) {
	engagementID, ok := pathID(request.URL.Path, "/v1/engagements/", "/checklist")
	if !ok || request.Method != http.MethodGet {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, false)
	if !ok {
		return
	}
	checklist, err := postgres.ReadEngagementChecklist(request.Context(), server.pool, session, engagementID)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found")
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error")
	default:
		writeJSON(response, http.StatusOK, checklist)
	}
}

// decodeMethodologyDraft reads one submitted version and refuses it before the
// store is reached unless every required field is present and every field is
// within its accepted length. An item must state how to verify a control, so
// its objective and procedure are required alongside its title.
func decodeMethodologyDraft(response http.ResponseWriter, request *http.Request) (postgres.MethodologyVersion, bool) {
	var input methodologyDraft
	if !decodeJSON(request, &input) || len(input.Items) > maxMethodologyItems {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return postgres.MethodologyVersion{}, false
	}
	version := postgres.MethodologyVersion{
		Name:          strings.TrimSpace(input.Name),
		SourceName:    strings.TrimSpace(input.SourceName),
		SourceVersion: strings.TrimSpace(input.SourceVersion),
		Attribution:   strings.TrimSpace(input.Attribution),
	}
	if !methodologyLabel(version.Name) || !methodologyLabel(version.SourceName) || !methodologyLabel(version.SourceVersion) || !methodologyText(version.Attribution) || version.Attribution == "" {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return postgres.MethodologyVersion{}, false
	}
	for _, item := range input.Items {
		converted := postgres.MethodologyItem{
			Title:            strings.TrimSpace(item.Title),
			Objective:        strings.TrimSpace(item.Objective),
			Preconditions:    strings.TrimSpace(item.Preconditions),
			Procedure:        strings.TrimSpace(item.Procedure),
			ExpectedEvidence: strings.TrimSpace(item.ExpectedEvidence),
			Reference:        strings.TrimSpace(item.Reference),
			Notes:            strings.TrimSpace(item.Notes),
		}
		if !methodologyLabel(converted.Title) || !methodologyText(converted.Objective) || converted.Objective == "" ||
			!methodologyText(converted.Procedure) || converted.Procedure == "" || !methodologyText(converted.Preconditions) ||
			!methodologyText(converted.ExpectedEvidence) || !methodologyText(converted.Reference) || !methodologyText(converted.Notes) {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return postgres.MethodologyVersion{}, false
		}
		version.Items = append(version.Items, converted)
	}
	return version, true
}

// label accepts one required short name.
func methodologyLabel(value string) bool {
	return value != "" && len(value) <= maxMethodologyLabelBytes
}

// text accepts one bounded content field, which may be empty.
func methodologyText(value string) bool {
	return len(value) <= maxMethodologyTextBytes
}
