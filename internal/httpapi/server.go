package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/domain"
	"github.com/sayseven7/frameops/internal/store/objectstore"
	"github.com/sayseven7/frameops/internal/store/postgres"
)

const sessionCookieName = "__Host-frameops_session"

type Server struct {
	pool     *pgxpool.Pool
	evidence objectstore.Bucket
}

func New(pool *pgxpool.Pool, evidence objectstore.Bucket) http.Handler {
	return Server{pool: pool, evidence: evidence}
}

func (server Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/v1/session/login":
		server.login(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/v1/csrf":
		server.csrf(response, request)
	case request.Method == http.MethodPost && request.URL.Path == "/v1/session/logout":
		server.logout(response, request)
	case request.URL.Path == "/v1/clients":
		server.clients(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/clients/") && strings.HasSuffix(request.URL.Path, "/engagements"):
		server.engagements(response, request)
	case request.URL.Path == "/v1/methodology-templates":
		server.methodologyTemplates(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/methodology-templates/") && strings.HasSuffix(request.URL.Path, "/draft"):
		server.methodologyDraftContent(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/methodology-templates/") && strings.HasSuffix(request.URL.Path, "/publish"):
		server.methodologyPublication(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/engagements/") && strings.HasSuffix(request.URL.Path, "/checklist"):
		server.engagementChecklist(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/engagements/") && strings.HasSuffix(request.URL.Path, "/reports"):
		server.reportRevisions(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/report-revisions/") && strings.HasSuffix(request.URL.Path, "/approve"):
		server.approveReportRevision(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/engagements/") && strings.HasSuffix(request.URL.Path, "/findings"):
		server.findings(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/engagements/") && strings.HasSuffix(request.URL.Path, "/assets"):
		server.assets(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/findings/") && strings.HasSuffix(request.URL.Path, "/assets"):
		server.findingAssets(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/findings/") && strings.HasSuffix(request.URL.Path, "/triage"):
		server.triage(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/findings/") && strings.HasSuffix(request.URL.Path, "/retests"):
		server.retests(response, request)
	case strings.HasPrefix(request.URL.Path, "/v1/findings/") && strings.HasSuffix(request.URL.Path, "/evidence"):
		server.findingEvidence(response, request)
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) login(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(request, &input) || strings.TrimSpace(input.Email) == "" || input.Password == "" {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	token, err := postgres.Authenticate(request.Context(), server.pool, input.Email, input.Password)
	if errors.Is(err, postgres.ErrUnauthorized) {
		writeError(response, http.StatusUnauthorized, "invalid_credentials")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	http.SetCookie(response, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", MaxAge: int((12 * time.Hour).Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	response.WriteHeader(http.StatusNoContent)
}

func (server Server) csrf(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, false)
	if !ok {
		return
	}
	token, err := postgres.IssueCSRF(request.Context(), server.pool, session)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, map[string]string{"token": token})
}

func (server Server) logout(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, true)
	if !ok {
		return
	}
	if err := postgres.RevokeSession(request.Context(), server.pool, session); err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error")
		return
	}
	http.SetCookie(response, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	response.WriteHeader(http.StatusNoContent)
}

func (server Server) clients(response http.ResponseWriter, request *http.Request) {
	session, ok := server.session(response, request, request.Method == http.MethodPost)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := postgres.ListClients(request.Context(), server.pool, session)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var input struct {
			Name string `json:"name"`
		}
		if !decodeJSON(request, &input) || strings.TrimSpace(input.Name) == "" {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		client, err := postgres.CreateClient(request.Context(), server.pool, session, input.Name)
		switch {
		case errors.Is(err, postgres.ErrForbidden):
			writeError(response, http.StatusForbidden, "forbidden")
		case err != nil:
			writeError(response, http.StatusInternalServerError, "internal_error")
		default:
			writeJSON(response, http.StatusCreated, client)
		}
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) engagements(response http.ResponseWriter, request *http.Request) {
	clientID, ok := pathID(request.URL.Path, "/v1/clients/", "/engagements")
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
		items, err := postgres.ListEngagements(request.Context(), server.pool, session, clientID)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		// The engagement may select one published methodology version, which it
		// receives as its own immutable checklist copy.
		var input struct {
			Name                 string `json:"name"`
			MethodologyVersionID string `json:"methodologyVersionId"`
		}
		if !decodeJSON(request, &input) || strings.TrimSpace(input.Name) == "" || !selectedID(input.MethodologyVersionID) {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		engagement, err := postgres.CreateEngagement(request.Context(), server.pool, session, clientID, input.Name, input.MethodologyVersionID)
		switch {
		case errors.Is(err, postgres.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found")
		case errors.Is(err, postgres.ErrForbidden):
			writeError(response, http.StatusForbidden, "forbidden")
		case err != nil:
			writeError(response, http.StatusInternalServerError, "internal_error")
		default:
			writeJSON(response, http.StatusCreated, engagement)
		}
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) findings(response http.ResponseWriter, request *http.Request) {
	engagementID, ok := pathID(request.URL.Path, "/v1/engagements/", "/findings")
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
		items, err := postgres.ListFindings(request.Context(), server.pool, session, engagementID)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var input struct {
			Title        string `json:"title"`
			Description  string `json:"description"`
			Impact       string `json:"impact"`
			Remediation  string `json:"remediation"`
			Reproduction string `json:"reproduction"`
			CVSSVector   string `json:"cvssVector"`
		}
		if !decodeJSON(request, &input) || strings.TrimSpace(input.Title) == "" {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		cvss, err := domain.ParseCVSS31(input.CVSSVector)
		if err != nil {
			writeError(response, http.StatusBadRequest, "invalid_cvss_vector")
			return
		}
		finding, err := postgres.CreateFinding(request.Context(), server.pool, session, engagementID, postgres.Finding{Title: input.Title, Description: input.Description, Impact: input.Impact, Remediation: input.Remediation, Reproduction: input.Reproduction, CVSSVector: cvss.Vector, CVSSScore: cvss.Score})
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusCreated, finding)
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

// maxFindingAssets bounds one finding-to-asset replacement so a single request
// cannot be used to enumerate or rewrite an unbounded set of identifiers.
const maxFindingAssets = 64

func (server Server) assets(response http.ResponseWriter, request *http.Request) {
	engagementID, ok := pathID(request.URL.Path, "/v1/engagements/", "/assets")
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
		items, err := postgres.ListAssets(request.Context(), server.pool, session, engagementID)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var input struct {
			Name string `json:"name"`
		}
		if !decodeJSON(request, &input) || strings.TrimSpace(input.Name) == "" {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		asset, err := postgres.CreateAsset(request.Context(), server.pool, session, engagementID, input.Name)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusCreated, asset)
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (server Server) findingAssets(response http.ResponseWriter, request *http.Request) {
	findingID, ok := pathID(request.URL.Path, "/v1/findings/", "/assets")
	if !ok {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, request.Method == http.MethodPut)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		items, err := postgres.ListFindingAssets(request.Context(), server.pool, session, findingID)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPut:
		var input struct {
			AssetIDs []string `json:"assetIds"`
		}
		if !decodeJSON(request, &input) || len(input.AssetIDs) > maxFindingAssets {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		items, err := postgres.ReplaceFindingAssets(request.Context(), server.pool, session, findingID, input.AssetIDs)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

// triage moves one finding along the only supported validation edge: a finding
// still in 'new' with no remediation state becomes 'confirmed' and 'open'. Any
// other target pair is refused before the store is reached; the store owns the
// current-state decision so a replay cannot be mistaken for a missing finding.
func (server Server) triage(response http.ResponseWriter, request *http.Request) {
	findingID, ok := pathID(request.URL.Path, "/v1/findings/", "/triage")
	if !ok || request.Method != http.MethodPut {
		writeError(response, http.StatusNotFound, "not_found")
		return
	}
	session, ok := server.session(response, request, true)
	if !ok {
		return
	}
	var input struct {
		ValidationState  string  `json:"validationState"`
		RemediationState *string `json:"remediationState"`
	}
	if !decodeJSON(request, &input) || input.ValidationState != "confirmed" || input.RemediationState == nil || *input.RemediationState != "open" {
		writeError(response, http.StatusBadRequest, "invalid_request")
		return
	}
	finding, err := postgres.TriageFinding(request.Context(), server.pool, session, findingID)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrInvalidState):
		writeError(response, http.StatusConflict, "invalid_state")
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error")
	default:
		writeJSON(response, http.StatusOK, finding)
	}
}

// retests reads and appends the retest history of one confirmed finding. A round
// may only be recorded while the finding is still 'open', and it either leaves it
// open or closes it as 'fixed' or 'not_reproduced'; those two stay distinct
// because an unreproduced finding is not proof that a correction was made.
// Accepting a risk is a separate decision and is refused here. The caller names
// the round it believes is next, so a replayed request is a conflict rather than
// a duplicated round, and the store owns the current-state decision.
func (server Server) retests(response http.ResponseWriter, request *http.Request) {
	findingID, ok := pathID(request.URL.Path, "/v1/findings/", "/retests")
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
		items, err := postgres.ListRetests(request.Context(), server.pool, session, findingID)
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(response, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var input struct {
			Round          int    `json:"round"`
			ResultState    string `json:"resultState"`
			Procedure      string `json:"procedure"`
			ObservedResult string `json:"observedResult"`
			Justification  string `json:"justification"`
		}
		if !decodeJSON(request, &input) || input.Round < 1 || !supportedRetestResult(input.ResultState) ||
			strings.TrimSpace(input.Procedure) == "" || strings.TrimSpace(input.ObservedResult) == "" || strings.TrimSpace(input.Justification) == "" {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		retest, err := postgres.RecordRetest(request.Context(), server.pool, session, findingID, postgres.Retest{Round: input.Round, ResultState: input.ResultState, Procedure: input.Procedure, ObservedResult: input.ObservedResult, Justification: input.Justification})
		switch {
		case errors.Is(err, postgres.ErrNotFound):
			writeError(response, http.StatusNotFound, "not_found")
		case errors.Is(err, postgres.ErrInvalidState):
			writeError(response, http.StatusConflict, "invalid_state")
		case err != nil:
			writeError(response, http.StatusInternalServerError, "internal_error")
		default:
			writeJSON(response, http.StatusCreated, retest)
		}
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func supportedRetestResult(state string) bool {
	return state == "open" || state == "fixed" || state == "not_reproduced"
}

// pathID reads the single identifier a route carries between its prefix and its
// suffix. An identifier PostgreSQL could not read as a UUID is reported as an
// absent route rather than forwarded, so a malformed path is a 404 instead of a
// database error, and it never reveals whether the identifier could exist.
func pathID(path, prefix, suffix string) (string, bool) {
	id := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if strings.Contains(id, "/") || !identifier(id) {
		return "", false
	}
	return id, true
}

// selectedID accepts one optional identifier a request body may carry, so an
// identifier PostgreSQL could not read as a UUID is refused at the boundary
// rather than reaching a query as a database error.
func selectedID(id string) bool {
	return id == "" || identifier(id)
}

func identifier(id string) bool {
	var parsed pgtype.UUID
	return parsed.Scan(id) == nil
}

func (server Server) session(response http.ResponseWriter, request *http.Request, requireCSRF bool) (postgres.Session, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return postgres.Session{}, false
	}
	session, err := postgres.SessionForToken(request.Context(), server.pool, cookie.Value)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return postgres.Session{}, false
	}
	if requireCSRF && !postgres.ValidCSRF(session, request.Header.Get("X-CSRF-Token")) {
		writeError(response, http.StatusForbidden, "forbidden")
		return postgres.Session{}, false
	}
	return session, true
}

func decodeJSON(request *http.Request, destination any) bool {
	if request.ContentLength > 1<<20 {
		return false
	}
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func writeError(response http.ResponseWriter, status int, code string) {
	writeJSON(response, status, map[string]string{"error": code})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
